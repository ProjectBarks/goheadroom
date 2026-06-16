package comparators

import (
	"encoding/json"

	"github.com/projectbarks/goheadroom/core/parity"
	"github.com/projectbarks/goheadroom/core/transforms/logcompressor"
)

type LogCompressor struct{}

func (LogCompressor) Name() string { return "log_compressor" }

func (LogCompressor) Run(input, config json.RawMessage) (interface{}, error) {
	var text string
	if err := json.Unmarshal(input, &text); err != nil {
		return nil, err
	}

	cfg := logcompressor.DefaultConfig()
	lc := logcompressor.New(cfg)
	result, _ := lc.Compress(text, 1.0)

	out := map[string]interface{}{
		"compressed":           result.Compressed,
		"compressed_line_count": result.CompressedLineCount,
		"original_line_count":  result.OriginalLineCount,
		"format_detected":      result.FormatDetected.String(),
		"cache_key":            result.CacheKey,
	}
	return out, nil
}

var _ parity.Comparator = LogCompressor{}
