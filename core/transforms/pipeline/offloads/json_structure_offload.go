package offloads

import (
	"fmt"
	"strings"

	"github.com/projectbarks/goheadroom/core/ccr"
	"github.com/projectbarks/goheadroom/core/transforms/contentdetector"
	"github.com/projectbarks/goheadroom/core/transforms/jsoncompressor"
	"github.com/projectbarks/goheadroom/core/transforms/pipeline"
)

const jsonStructureOffloadName = "json_structure_offload"
const jsonStructureOffloadConfidence float32 = 0.80

// JsonStructureOffload wraps jsoncompressor.Compress as an OffloadTransform.
// Targets JSON objects and arrays, preserving keys and structure while
// eliding long string values.
type JsonStructureOffload struct {
	config pipeline.JsonStructureOffloadConfig
}

// NewJsonStructureOffload creates a JsonStructureOffload with the given config.
func NewJsonStructureOffload(config pipeline.JsonStructureOffloadConfig) *JsonStructureOffload {
	return &JsonStructureOffload{config: config}
}

func (j *JsonStructureOffload) Name() string { return jsonStructureOffloadName }

func (j *JsonStructureOffload) AppliesTo() []contentdetector.ContentType {
	return []contentdetector.ContentType{contentdetector.PlainText, contentdetector.JsonArray}
}

func (j *JsonStructureOffload) EstimateBloat(content string) float32 {
	if content == "" {
		return 0.0
	}
	trimmed := strings.TrimLeft(content, " \t\n\r")
	if len(trimmed) == 0 {
		return 0.0
	}
	if trimmed[0] != '{' && trimmed[0] != '[' {
		return 0.0
	}
	minLen := j.config.MinInputLen
	if minLen <= 0 {
		minLen = 100
	}
	if len(content) < minLen {
		return 0.0
	}
	result := jsoncompressor.Compress(content, jsoncompressor.DefaultConfig())
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

func (j *JsonStructureOffload) Apply(content string, ctx *pipeline.CompressionContext, store pipeline.CcrStore) (*pipeline.OffloadOutput, error) {
	trimmed := strings.TrimLeft(content, " \t\n\r")
	if len(trimmed) == 0 || (trimmed[0] != '{' && trimmed[0] != '[') {
		return nil, pipeline.InvalidInput(jsonStructureOffloadName, "not a JSON object or array")
	}

	result := jsoncompressor.Compress(content, jsoncompressor.DefaultConfig())
	if len(result.Compressed) >= len(content) {
		return nil, pipeline.Skipped(jsonStructureOffloadName, "no savings after compression")
	}

	key := ccr.ComputeKey([]byte(content))
	store.Put(key, []byte(content))
	output := result.Compressed + fmt.Sprintf("\n[json_structure_offload CCR: hash=%s]", key)

	out := pipeline.OffloadOutputFromLengths(len(content), output, key)
	return &out, nil
}

func (j *JsonStructureOffload) Confidence() float32 { return jsonStructureOffloadConfidence }

var _ pipeline.OffloadTransform = (*JsonStructureOffload)(nil)
