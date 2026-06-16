package comparators

import (
	"encoding/json"

	"github.com/projectbarks/goheadroom/core/parity"
	"github.com/projectbarks/goheadroom/core/transforms/codecompressor"
)

type CodeCompressor struct{}

func (CodeCompressor) Name() string { return "code_compressor" }

func (CodeCompressor) Run(input, config json.RawMessage) (interface{}, error) {
	var text string
	if err := json.Unmarshal(input, &text); err != nil {
		return nil, err
	}

	result := codecompressor.Compress(text)

	out := map[string]interface{}{
		"compressed": result.Compressed,
		"language":   result.Language.String(),
	}
	return out, nil
}

var _ parity.Comparator = CodeCompressor{}
