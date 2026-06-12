package offloads

import (
	"fmt"
	"strings"

	"github.com/uber/goheadroom/ccr"
	"github.com/uber/goheadroom/transforms/contentdetector"
	"github.com/uber/goheadroom/transforms/pipeline"
	"github.com/uber/goheadroom/transforms/smartcrusher"
)

const jsonOffloadName = "json_offload"
const jsonOffloadConfidence float32 = 0.85

// JsonOffload wraps SmartCrusher as an OffloadTransform.
// Bloat heuristic: counts row separators in JSON arrays.
type JsonOffload struct {
	crusher *smartcrusher.SmartCrusher
	config  pipeline.JsonOffloadConfig
}

// NewJsonOffload creates a JsonOffload with the given config.
func NewJsonOffload(config pipeline.JsonOffloadConfig) *JsonOffload {
	sc := smartcrusher.NewSmartCrusherBuilder(smartcrusher.DefaultSmartCrusherConfig()).Build()
	return &JsonOffload{crusher: sc, config: config}
}

func (j *JsonOffload) Name() string { return jsonOffloadName }

func (j *JsonOffload) AppliesTo() []contentdetector.ContentType {
	return []contentdetector.ContentType{contentdetector.JsonArray}
}

func (j *JsonOffload) EstimateBloat(content string) float32 {
	if content == "" {
		return 0.0
	}
	trimmed := strings.TrimLeft(content, " \t\n\r")
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return 0.0
	}
	separators := countRowSeparators(content)
	minSep := j.config.MinArrayRows - 1
	if minSep < 0 {
		minSep = 0
	}
	if separators < minSep {
		return 0.0
	}
	saturation := j.config.SaturationRows - 1
	if saturation < 1 {
		saturation = 1
	}
	score := float32(separators) / float32(saturation)
	if score > 1.0 {
		score = 1.0
	}
	return score
}

func (j *JsonOffload) Apply(content string, ctx *pipeline.CompressionContext, store pipeline.CcrStore) (*pipeline.OffloadOutput, error) {
	result := j.crusher.Crush(content, ctx.Query, 0.0)
	if !result.WasModified {
		return nil, pipeline.Skipped(jsonOffloadName, "smart crusher returned passthrough")
	}
	if len(result.Compressed) >= len(content) {
		return nil, pipeline.Skipped(jsonOffloadName, "no savings after crush")
	}

	// Wrapper-level CCR: hash the WHOLE original input.
	key := ccr.ComputeKey([]byte(content))
	store.Put(key, []byte(content))
	output := result.Compressed + fmt.Sprintf("\n[json_offload CCR: hash=%s]", key)

	out := pipeline.OffloadOutputFromLengths(len(content), output, key)
	return &out, nil
}

func (j *JsonOffload) Confidence() float32 { return jsonOffloadConfidence }

// countRowSeparators counts plausible row-boundary patterns.
func countRowSeparators(content string) int {
	return strings.Count(content, "},{") +
		strings.Count(content, "}, {") +
		strings.Count(content, "},\n")
}

var _ pipeline.OffloadTransform = (*JsonOffload)(nil)
