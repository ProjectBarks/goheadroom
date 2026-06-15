package offloads

import (
	"fmt"
	"strings"

	"github.com/uber/goheadroom/ccr"
	"github.com/uber/goheadroom/transforms/codecompressor"
	"github.com/uber/goheadroom/transforms/contentdetector"
	"github.com/uber/goheadroom/transforms/pipeline"
)

const codeOffloadName = "code_offload"
const codeOffloadConfidence float32 = 0.85

// CodeOffload wraps codecompressor.Compress as an OffloadTransform.
// Targets source code, preserving imports/signatures while eliding bodies.
type CodeOffload struct {
	config pipeline.CodeOffloadConfig
}

// NewCodeOffload creates a CodeOffload with the given config.
func NewCodeOffload(config pipeline.CodeOffloadConfig) *CodeOffload {
	return &CodeOffload{config: config}
}

func (c *CodeOffload) Name() string { return codeOffloadName }

func (c *CodeOffload) AppliesTo() []contentdetector.ContentType {
	return []contentdetector.ContentType{contentdetector.SourceCode}
}

func (c *CodeOffload) EstimateBloat(content string) float32 {
	if content == "" {
		return 0.0
	}
	if !codecompressor.CanHandle(content) {
		return 0.0
	}
	minLen := c.config.MinInputLen
	if minLen <= 0 {
		minLen = 200
	}
	if len(strings.TrimSpace(content)) < minLen {
		return 0.0
	}
	result := codecompressor.Compress(content)
	if result.BodiesRemoved == 0 {
		return 0.0
	}
	if len(result.Compressed) >= len(content) {
		return 0.0
	}
	ratio := 1.0 - float32(len(result.Compressed))/float32(len(content))
	if ratio < 0 {
		return 0.0
	}
	if ratio > 1.0 {
		return 1.0
	}
	return ratio
}

func (c *CodeOffload) Apply(content string, ctx *pipeline.CompressionContext, store pipeline.CcrStore) (*pipeline.OffloadOutput, error) {
	if !codecompressor.CanHandle(content) {
		return nil, pipeline.InvalidInput(codeOffloadName, "content does not look like source code")
	}

	result := codecompressor.Compress(content)
	if result.BodiesRemoved == 0 {
		return nil, pipeline.Skipped(codeOffloadName, "no function bodies found to elide")
	}
	if len(result.Compressed) >= len(content) {
		return nil, pipeline.Skipped(codeOffloadName, "no savings after compression")
	}

	key := ccr.ComputeKey([]byte(content))
	store.Put(key, []byte(content))
	output := result.Compressed + fmt.Sprintf("\n[code_offload CCR: hash=%s]", key)

	out := pipeline.OffloadOutputFromLengths(len(content), output, key)
	return &out, nil
}

func (c *CodeOffload) Confidence() float32 { return codeOffloadConfidence }

var _ pipeline.OffloadTransform = (*CodeOffload)(nil)
