// Package reformats implements lossless reformat transforms for the
// compression pipeline.
package reformats

import (
	"encoding/json"
	"strings"

	"github.com/uber/goheadroom/transforms/contentdetector"
	"github.com/uber/goheadroom/transforms/pipeline"
)

const jsonMinifierName = "json_minifier"

// JsonMinifier strips insignificant whitespace from JSON via
// encoding/json round-trip. Lossless by construction.
type JsonMinifier struct{}

// NewJsonMinifier creates a new JsonMinifier.
func NewJsonMinifier() *JsonMinifier {
	return &JsonMinifier{}
}

func (j *JsonMinifier) Name() string { return jsonMinifierName }

func (j *JsonMinifier) AppliesTo() []contentdetector.ContentType {
	return []contentdetector.ContentType{contentdetector.JsonArray}
}

func (j *JsonMinifier) Apply(content string) (*pipeline.ReformatOutput, error) {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return nil, pipeline.Skipped(jsonMinifierName, "empty input")
	}

	var parsed interface{}
	if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil {
		return nil, pipeline.InvalidInput(jsonMinifierName, err.Error())
	}

	minified, err := json.Marshal(parsed)
	if err != nil {
		return nil, pipeline.Internal(jsonMinifierName, err.Error())
	}

	// Defensive: if minification grew the byte count, hand the
	// original back so we never inflate the wire output.
	if len(minified) >= len(content) {
		out := pipeline.ReformatOutputFromLengths(len(content), content)
		return &out, nil
	}

	out := pipeline.ReformatOutputFromLengths(len(content), string(minified))
	return &out, nil
}

// Verify interface compliance at compile time.
var _ pipeline.ReformatTransform = (*JsonMinifier)(nil)
