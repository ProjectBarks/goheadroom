package comparators

import (
	"encoding/json"
	"math"

	"github.com/projectbarks/goheadroom/core/parity"
	"github.com/projectbarks/goheadroom/core/transforms/jsoncompressor"
)

type JSONCompressor struct{}

func (JSONCompressor) Name() string { return "json_compressor" }

func (JSONCompressor) Run(input, config json.RawMessage) (interface{}, error) {
	var text string
	if err := json.Unmarshal(input, &text); err != nil {
		return nil, err
	}

	cfg := jsoncompressor.DefaultConfig()
	result := jsoncompressor.Compress(text, cfg)

	ratio := 0.0
	if len(text) > 0 {
		ratio = math.Round(float64(len(result.Compressed))/float64(len(text))*10000) / 10000
	}

	return map[string]interface{}{
		"compressed":         result.Compressed,
		"preservation_ratio": ratio,
	}, nil
}

var _ parity.Comparator = JSONCompressor{}
