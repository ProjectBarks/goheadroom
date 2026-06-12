// Package offloads implements OffloadTransform wrappers around the
// specialized compressors for the compression pipeline.
package offloads

import (
	"strings"

	"github.com/uber/goheadroom/ccr"
	"github.com/uber/goheadroom/transforms/contentdetector"
	"github.com/uber/goheadroom/transforms/diffcompressor"
	"github.com/uber/goheadroom/transforms/pipeline"
)

const diffOffloadName = "diff_offload"
const diffOffloadConfidence float32 = 0.85

// DiffOffload wraps DiffCompressor as an OffloadTransform.
// Bloat heuristic: context-to-change ratio in unified diffs.
type DiffOffload struct {
	compressor *diffcompressor.DiffCompressor
	bloat      pipeline.DiffBloatConfig
}

// NewDiffOffload creates a DiffOffload with the given bloat config.
func NewDiffOffload(bloat pipeline.DiffBloatConfig) *DiffOffload {
	return &DiffOffload{
		compressor: diffcompressor.New(diffcompressor.DefaultConfig()),
		bloat:      bloat,
	}
}

func (d *DiffOffload) Name() string { return diffOffloadName }

func (d *DiffOffload) AppliesTo() []contentdetector.ContentType {
	return []contentdetector.ContentType{contentdetector.GitDiff}
}

func (d *DiffOffload) EstimateBloat(content string) float32 {
	if content == "" {
		return 0.0
	}
	var totalLines, change, context int
	inHunk := false

	for _, line := range strings.Split(content, "\n") {
		totalLines++
		if strings.HasPrefix(line, "@@") {
			inHunk = true
			continue
		}
		if strings.HasPrefix(line, "diff --git") {
			inHunk = false
			continue
		}
		if strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---") {
			continue
		}
		if !inHunk {
			continue
		}
		if len(line) == 0 {
			continue
		}
		switch line[0] {
		case '+', '-':
			change++
		case ' ':
			context++
		}
	}

	if totalLines < d.bloat.MinLines {
		return 0.0
	}
	denom := float64(context + change)
	if denom == 0.0 {
		return 0.0
	}
	ratio := float64(context) / denom
	normal := d.bloat.NormalContextRatio
	if ratio <= normal {
		return 0.0
	}
	span := 1.0 - normal
	if span <= 0.0 {
		return 1.0
	}
	score := (ratio - normal) / span
	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}
	return float32(score)
}

func (d *DiffOffload) Apply(content string, ctx *pipeline.CompressionContext, store pipeline.CcrStore) (*pipeline.OffloadOutput, error) {
	result, _ := d.compressor.CompressWithStore(content, ctx.Query, store.(ccr.CcrStore))

	if result.CacheKey == nil {
		return nil, pipeline.Skipped(diffOffloadName, "diff compressor did not emit a cache_key")
	}

	out := pipeline.OffloadOutputFromLengths(len(content), result.Compressed, *result.CacheKey)
	return &out, nil
}

func (d *DiffOffload) Confidence() float32 { return diffOffloadConfidence }

var _ pipeline.OffloadTransform = (*DiffOffload)(nil)
