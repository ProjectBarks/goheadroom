package comparators

import (
	"encoding/json"

	"github.com/projectbarks/goheadroom/core/parity"
	"github.com/projectbarks/goheadroom/core/transforms/searchcompressor"
)

type SearchCompressor struct{}

func (SearchCompressor) Name() string { return "search_compressor" }

func (SearchCompressor) Run(input, config json.RawMessage) (interface{}, error) {
	var text string
	if err := json.Unmarshal(input, &text); err != nil {
		return nil, err
	}

	cfg := searchcompressor.DefaultConfig()
	sc := searchcompressor.New(cfg)
	result, _ := sc.Compress(text, "", 1.0)

	out := map[string]interface{}{
		"compressed": result.Compressed,
	}
	return out, nil
}

var _ parity.Comparator = SearchCompressor{}
