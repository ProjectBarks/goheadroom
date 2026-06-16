package comparators

import (
	"encoding/json"

	"github.com/projectbarks/goheadroom/core/parity"
	"github.com/projectbarks/goheadroom/core/transforms/diffcompressor"
)

type diffCompressorConfig struct {
	AlwaysKeepAdditions       *bool `json:"always_keep_additions"`
	AlwaysKeepDeletions       *bool `json:"always_keep_deletions"`
	MaxContextLines           *int  `json:"max_context_lines"`
	MaxFiles                  *int  `json:"max_files"`
	MaxHunksPerFile           *int  `json:"max_hunks_per_file"`
	EnableCCR                 *bool `json:"enable_ccr"`
	MinLinesForCCR            *int  `json:"min_lines_for_ccr"`
}

type DiffCompressor struct{}

func (DiffCompressor) Name() string { return "diff_compressor" }

func (DiffCompressor) Run(input, config json.RawMessage) (interface{}, error) {
	var text string
	if err := json.Unmarshal(input, &text); err != nil {
		return nil, err
	}

	cfg := diffcompressor.DefaultConfig()

	if config != nil {
		var parsed diffCompressorConfig
		if err := json.Unmarshal(config, &parsed); err == nil {
			if parsed.AlwaysKeepAdditions != nil {
				cfg.AlwaysKeepAdditions = *parsed.AlwaysKeepAdditions
			}
			if parsed.AlwaysKeepDeletions != nil {
				cfg.AlwaysKeepDeletions = *parsed.AlwaysKeepDeletions
			}
			if parsed.MaxContextLines != nil {
				cfg.MaxContextLines = *parsed.MaxContextLines
			}
			if parsed.MaxFiles != nil {
				cfg.MaxFiles = *parsed.MaxFiles
			}
			if parsed.MaxHunksPerFile != nil {
				cfg.MaxHunksPerFile = *parsed.MaxHunksPerFile
			}
			if parsed.EnableCCR != nil {
				cfg.EnableCCR = *parsed.EnableCCR
			}
			if parsed.MinLinesForCCR != nil {
				cfg.MinLinesForCCR = *parsed.MinLinesForCCR
			}
		}
	}

	dc := diffcompressor.New(cfg)
	result := dc.Compress(text, "")

	out := map[string]interface{}{
		"compressed":           result.Compressed,
		"compressed_line_count": result.CompressedLineCount,
		"original_line_count":  result.OriginalLineCount,
		"files_affected":       result.FilesAffected,
		"additions":            result.Additions,
		"deletions":            result.Deletions,
		"hunks_kept":           result.HunksKept,
		"hunks_removed":        result.HunksRemoved,
		"cache_key":            result.CacheKey,
	}
	return out, nil
}

var _ parity.Comparator = DiffCompressor{}
