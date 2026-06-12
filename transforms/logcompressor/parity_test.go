package logcompressor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type parityFixture struct {
	Config      json.RawMessage `json:"config"`
	Input       string          `json:"input"`
	Output      parityOutput    `json:"output"`
	InputSHA256 string          `json:"input_sha256"`
	Transform   string          `json:"transform"`
}

type parityOutput struct {
	Compressed          string  `json:"compressed"`
	CompressedLineCount int     `json:"compressed_line_count"`
	OriginalLineCount   int     `json:"original_line_count"`
	FormatDetected      string  `json:"format_detected"`
	CacheKey            *string `json:"cache_key"`
}

type parityConfig struct {
	MaxTotalLines      *int  `json:"max_total_lines,omitempty"`
	MaxErrors          *int  `json:"max_errors,omitempty"`
	MaxWarnings        *int  `json:"max_warnings,omitempty"`
	MaxStackTraces     *int  `json:"max_stack_traces,omitempty"`
	StackTraceMaxLines *int  `json:"stack_trace_max_lines,omitempty"`
	ErrorContextLines  *int  `json:"error_context_lines,omitempty"`
	KeepFirstError     *bool `json:"keep_first_error,omitempty"`
	KeepLastError      *bool `json:"keep_last_error,omitempty"`
	KeepSummaryLines   *bool `json:"keep_summary_lines,omitempty"`
	DedupeWarnings     *bool `json:"dedupe_warnings,omitempty"`
	EnableCCR          *bool `json:"enable_ccr,omitempty"`
	MinLinesForCCR     *int  `json:"min_lines_for_ccr,omitempty"`
}

func configFromParity(raw json.RawMessage) LogCompressorConfig {
	cfg := DefaultConfig()
	var pc parityConfig
	if err := json.Unmarshal(raw, &pc); err != nil {
		return cfg
	}
	if pc.MaxTotalLines != nil {
		cfg.MaxTotalLines = *pc.MaxTotalLines
	}
	if pc.MaxErrors != nil {
		cfg.MaxErrors = *pc.MaxErrors
	}
	if pc.MaxWarnings != nil {
		cfg.MaxWarnings = *pc.MaxWarnings
	}
	if pc.MaxStackTraces != nil {
		cfg.MaxStackTraces = *pc.MaxStackTraces
	}
	if pc.StackTraceMaxLines != nil {
		cfg.StackTraceMaxLines = *pc.StackTraceMaxLines
	}
	if pc.ErrorContextLines != nil {
		cfg.ErrorContextLines = *pc.ErrorContextLines
	}
	if pc.KeepFirstError != nil {
		cfg.KeepFirstError = *pc.KeepFirstError
	}
	if pc.KeepLastError != nil {
		cfg.KeepLastError = *pc.KeepLastError
	}
	if pc.KeepSummaryLines != nil {
		cfg.KeepSummaryLines = *pc.KeepSummaryLines
	}
	if pc.DedupeWarnings != nil {
		cfg.DedupeWarnings = *pc.DedupeWarnings
	}
	if pc.EnableCCR != nil {
		cfg.EnableCCR = *pc.EnableCCR
	}
	if pc.MinLinesForCCR != nil {
		cfg.MinLinesForCCR = *pc.MinLinesForCCR
	}
	return cfg
}

func TestLogCompressorParity(t *testing.T) {
	fixtureDir := filepath.Join("..", "..", "testdata", "parity", "log_compressor")
	entries, err := os.ReadDir(fixtureDir)
	require.NoError(t, err, "failed to read fixture dir: %s", fixtureDir)
	require.NotEmpty(t, entries, "no fixtures found")

	count := 0
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		count++
		t.Run(entry.Name(), func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(fixtureDir, entry.Name()))
			require.NoError(t, err)

			var fixture parityFixture
			require.NoError(t, json.Unmarshal(data, &fixture))
			assert.Equal(t, "log_compressor", fixture.Transform)

			cfg := configFromParity(fixture.Config)
			c := New(cfg)
			result, _ := c.Compress(fixture.Input, 1.0)

			assert.Equal(t, fixture.Output.Compressed, result.Compressed,
				"compressed output mismatch for %s", entry.Name())
			assert.Equal(t, fixture.Output.CompressedLineCount, result.CompressedLineCount,
				"compressed_line_count mismatch for %s", entry.Name())
			assert.Equal(t, fixture.Output.OriginalLineCount, result.OriginalLineCount,
				"original_line_count mismatch for %s", entry.Name())
			if fixture.Output.FormatDetected != "" {
				assert.Equal(t, fixture.Output.FormatDetected, result.FormatDetected.String(),
					"format_detected mismatch for %s", entry.Name())
			}
		})
	}
	assert.Equal(t, 20, count, "expected 20 fixture files")
}
