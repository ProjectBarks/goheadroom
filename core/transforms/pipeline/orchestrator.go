package pipeline

import (
	"sync"

	"github.com/projectbarks/goheadroom/core/transforms/contentdetector"
)

// PipelineResult is returned by CompressionPipeline.Run.
type PipelineResult struct {
	// Final output. Equal to the input if every stage skipped.
	Output string
	// Total bytes removed across all accepted stages.
	BytesSaved int
	// Names of transforms that were actually accepted, in execution order.
	StepsApplied []string
	// CCR cache keys produced by accepted offloads. Empty when only
	// reformats ran.
	CacheKeys []string
}

// CompressionPipeline orchestrates reformat-then-offload compression
// with parallel bloat estimation.
type CompressionPipeline struct {
	reformatsByType map[contentdetector.ContentType][]ReformatTransform
	offloadsByType  map[contentdetector.ContentType][]OffloadTransform
	config          *PipelineConfig
}

// Run executes the pipeline against content of the given type.
// store receives offload payloads under their cache keys.
func (p *CompressionPipeline) Run(content string, contentType contentdetector.ContentType, ctx *CompressionContext, store CcrStore) PipelineResult {
	originalLen := len(content)
	if originalLen == 0 {
		return PipelineResult{Output: ""}
	}

	reformats := p.reformatsByType[contentType]
	offloads := p.offloadsByType[contentType]

	// Phase 1+2: run reformat phase and bloat estimation in parallel.
	var reformatAcc reformatAccumulator
	var bloatScores []float32
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		reformatAcc = p.runReformats(content, reformats)
	}()
	go func() {
		defer wg.Done()
		bloatScores = p.estimateBloats(content, offloads)
	}()
	wg.Wait()

	steps := reformatAcc.steps
	totalSaved := reformatAcc.bytesSaved
	current := reformatAcc.output

	// Compute post-reformat ratio for fallback gating.
	reformatRatio := float64(len(current)) / float64(max(originalLen, 1))

	// Phase 3: decide and run offloads.
	var cacheKeys []string
	for i, offload := range offloads {
		score := bloatScores[i]
		aboveThreshold := score >= p.config.Pipeline.BloatThreshold
		reformatUnderwhelmed := reformatRatio > p.config.Pipeline.OffloadFallbackRatio && score > 0

		if !(aboveThreshold || reformatUnderwhelmed) {
			continue
		}

		out, err := offload.Apply(current, ctx, store)
		if err != nil {
			// Transform errors are non-fatal. Internal errors get
			// logged at WARN in production; all others at TRACE.
			continue
		}
		if out.BytesSaved == 0 {
			continue
		}

		totalSaved += out.BytesSaved
		current = out.Output
		steps = append(steps, offload.Name())
		cacheKeys = append(cacheKeys, out.CacheKey)
	}

	return PipelineResult{
		Output:       current,
		BytesSaved:   totalSaved,
		StepsApplied: steps,
		CacheKeys:    cacheKeys,
	}
}

// Config returns the pipeline configuration.
func (p *CompressionPipeline) Config() *PipelineConfig {
	return p.config
}

// runReformats runs applicable reformats in registration order.
// Stops early when output_len/original_len <= reformat_target_ratio.
func (p *CompressionPipeline) runReformats(content string, reformats []ReformatTransform) reformatAccumulator {
	originalLen := len(content)
	current := content
	var totalSaved int
	var steps []string

	for _, transform := range reformats {
		ratio := float64(len(current)) / float64(max(originalLen, 1))
		if ratio <= p.config.Pipeline.ReformatTargetRatio {
			break
		}
		out, err := transform.Apply(current)
		if err != nil {
			continue
		}
		if out.BytesSaved == 0 {
			continue
		}
		totalSaved += out.BytesSaved
		current = out.Output
		steps = append(steps, transform.Name())
	}

	return reformatAccumulator{
		output:     current,
		bytesSaved: totalSaved,
		steps:      steps,
	}
}

// estimateBloats runs bloat estimation in parallel for all offloads.
func (p *CompressionPipeline) estimateBloats(content string, offloads []OffloadTransform) []float32 {
	scores := make([]float32, len(offloads))
	var wg sync.WaitGroup
	for i, o := range offloads {
		wg.Add(1)
		go func(i int, o OffloadTransform) {
			defer wg.Done()
			scores[i] = o.EstimateBloat(content)
		}(i, o)
	}
	wg.Wait()
	return scores
}

type reformatAccumulator struct {
	output     string
	bytesSaved int
	steps      []string
}

// --- Builder ---

// CompressionPipelineBuilder is a fluent builder for CompressionPipeline.
type CompressionPipelineBuilder struct {
	reformatsByType map[contentdetector.ContentType][]ReformatTransform
	offloadsByType  map[contentdetector.ContentType][]OffloadTransform
	config          *PipelineConfig
}

// NewPipelineBuilder creates a new builder.
func NewPipelineBuilder() *CompressionPipelineBuilder {
	return &CompressionPipelineBuilder{
		reformatsByType: make(map[contentdetector.ContentType][]ReformatTransform),
		offloadsByType:  make(map[contentdetector.ContentType][]OffloadTransform),
	}
}

// WithReformat registers a reformat transform for its applicable content types.
func (b *CompressionPipelineBuilder) WithReformat(r ReformatTransform) *CompressionPipelineBuilder {
	for _, ct := range r.AppliesTo() {
		b.reformatsByType[ct] = append(b.reformatsByType[ct], r)
	}
	return b
}

// WithOffload registers an offload transform for its applicable content types.
func (b *CompressionPipelineBuilder) WithOffload(o OffloadTransform) *CompressionPipelineBuilder {
	for _, ct := range o.AppliesTo() {
		b.offloadsByType[ct] = append(b.offloadsByType[ct], o)
	}
	return b
}

// WithConfig sets a custom pipeline config.
func (b *CompressionPipelineBuilder) WithConfig(cfg *PipelineConfig) *CompressionPipelineBuilder {
	b.config = cfg
	return b
}

// Build creates the CompressionPipeline.
func (b *CompressionPipelineBuilder) Build() *CompressionPipeline {
	cfg := b.config
	if cfg == nil {
		var err error
		cfg, err = DefaultPipelineConfig()
		if err != nil {
			// Embedded config is tested at build time; panic is appropriate.
			panic("embedded pipeline config must parse: " + err.Error())
		}
	}
	return &CompressionPipeline{
		reformatsByType: b.reformatsByType,
		offloadsByType:  b.offloadsByType,
		config:          cfg,
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
