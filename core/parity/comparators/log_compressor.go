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

	cfg := parseLogConfig(config)
	lc := logcompressor.New(cfg)
	result, _ := lc.Compress(text, 1.0)

	out := map[string]interface{}{
		"compressed":            result.Compressed,
		"compressed_line_count": result.CompressedLineCount,
		"compression_ratio":     result.CompressionRatio,
		"format_detected":       result.FormatDetected.String(),
		"original":              result.Original,
		"original_line_count":   result.OriginalLineCount,
		"cache_key":             result.CacheKey,
		"stats":                 result.Stats,
	}
	return out, nil
}

func parseLogConfig(raw json.RawMessage) logcompressor.LogCompressorConfig {
	cfg := logcompressor.DefaultConfig()
	var pc struct {
		MaxTotalLines             *int     `json:"max_total_lines,omitempty"`
		MaxErrors                 *int     `json:"max_errors,omitempty"`
		MaxWarnings               *int     `json:"max_warnings,omitempty"`
		MaxStackTraces            *int     `json:"max_stack_traces,omitempty"`
		StackTraceMaxLines        *int     `json:"stack_trace_max_lines,omitempty"`
		ErrorContextLines         *int     `json:"error_context_lines,omitempty"`
		KeepFirstError            *bool    `json:"keep_first_error,omitempty"`
		KeepLastError             *bool    `json:"keep_last_error,omitempty"`
		KeepSummaryLines          *bool    `json:"keep_summary_lines,omitempty"`
		DedupeWarnings            *bool    `json:"dedupe_warnings,omitempty"`
		EnableCCR                 *bool    `json:"enable_ccr,omitempty"`
		MinLinesForCCR            *int     `json:"min_lines_for_ccr,omitempty"`
		MinCompressionRatioForCCR *float64 `json:"min_compression_ratio_for_ccr,omitempty"`
	}
	if json.Unmarshal(raw, &pc) != nil {
		return cfg
	}
	if pc.MaxTotalLines != nil { cfg.MaxTotalLines = *pc.MaxTotalLines }
	if pc.MaxErrors != nil { cfg.MaxErrors = *pc.MaxErrors }
	if pc.MaxWarnings != nil { cfg.MaxWarnings = *pc.MaxWarnings }
	if pc.MaxStackTraces != nil { cfg.MaxStackTraces = *pc.MaxStackTraces }
	if pc.StackTraceMaxLines != nil { cfg.StackTraceMaxLines = *pc.StackTraceMaxLines }
	if pc.ErrorContextLines != nil { cfg.ErrorContextLines = *pc.ErrorContextLines }
	if pc.KeepFirstError != nil { cfg.KeepFirstError = *pc.KeepFirstError }
	if pc.KeepLastError != nil { cfg.KeepLastError = *pc.KeepLastError }
	if pc.KeepSummaryLines != nil { cfg.KeepSummaryLines = *pc.KeepSummaryLines }
	if pc.DedupeWarnings != nil { cfg.DedupeWarnings = *pc.DedupeWarnings }
	if pc.EnableCCR != nil { cfg.EnableCCR = *pc.EnableCCR }
	if pc.MinLinesForCCR != nil { cfg.MinLinesForCCR = *pc.MinLinesForCCR }
	if pc.MinCompressionRatioForCCR != nil { cfg.MinCompressionRatioForCCR = *pc.MinCompressionRatioForCCR }
	return cfg
}

var _ parity.Comparator = LogCompressor{}
