package comparators

import (
	"encoding/json"

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

	out := map[string]interface{}{
		"compressed": result.Compressed,
	}
	return out, nil
}

var _ parity.Comparator = JSONCompressor{}
