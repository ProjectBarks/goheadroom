package logcompressor

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uber/goheadroom/ccr"
)

// ── Enum tests ──────────────────────────────────────────────────────

func TestLogFormatEnum(t *testing.T) {
	assert.Equal(t, "pytest", LogFormatPytest.String())
	assert.Equal(t, "npm", LogFormatNpm.String())
	assert.Equal(t, "cargo", LogFormatCargo.String())
	assert.Equal(t, "jest", LogFormatJest.String())
	assert.Equal(t, "make", LogFormatMake.String())
	assert.Equal(t, "generic", LogFormatGeneric.String())
}

func TestLogLevelEnum(t *testing.T) {
	// Error > Fail > Warn > Unknown > Info > Debug > Trace
	assert.Greater(t, int(LogLevelError), int(LogLevelWarn))
	assert.Greater(t, int(LogLevelWarn), int(LogLevelInfo))
	assert.Greater(t, int(LogLevelInfo), int(LogLevelDebug))
	assert.Greater(t, int(LogLevelDebug), int(LogLevelTrace))
}

func TestDefaultLogCompressorConfig(t *testing.T) {
	cfg := DefaultConfig()
	assert.Equal(t, 10, cfg.MaxErrors)
	assert.Equal(t, 3, cfg.ErrorContextLines)
	assert.True(t, cfg.KeepFirstError)
	assert.True(t, cfg.KeepLastError)
	assert.Equal(t, 3, cfg.MaxStackTraces)
	assert.Equal(t, 20, cfg.StackTraceMaxLines)
	assert.Equal(t, 5, cfg.MaxWarnings)
	assert.True(t, cfg.DedupeWarnings)
	assert.True(t, cfg.KeepSummaryLines)
	assert.Equal(t, 100, cfg.MaxTotalLines)
	assert.True(t, cfg.EnableCCR)
	assert.Equal(t, 50, cfg.MinLinesForCCR)
	assert.InDelta(t, 0.5, cfg.MinCompressionRatioForCCR, 1e-9)
}

// ── Format detection tests ──────────────────────────────────────────

func TestDetectsPytestFormat(t *testing.T) {
	lines := []string{
		"============================= test session starts =============================",
		"collected 15 items",
		"tests/test_foo.py::test_basic PASSED [  6%]",
		"FAILED tests/test_foo.py::test_edge",
	}
	assert.Equal(t, LogFormatPytest, DetectFormat(lines))
}

func TestDetectsNpmFormat(t *testing.T) {
	lines := []string{"npm WARN deprecated x", "npm ERR! something"}
	assert.Equal(t, LogFormatNpm, DetectFormat(lines))
}

func TestDetectsCargoFormat(t *testing.T) {
	lines := []string{"   Compiling app v0.1.0", "warning: unused variable"}
	assert.Equal(t, LogFormatCargo, DetectFormat(lines))
}

func TestDetectsJestFormat(t *testing.T) {
	lines := []string{"PASS src/app.test.js", "Test Suites: 1 failed"}
	assert.Equal(t, LogFormatJest, DetectFormat(lines))
}

func TestDetectsMakeFormat(t *testing.T) {
	lines := []string{"make[1]: Entering directory", "gcc -c main.c"}
	assert.Equal(t, LogFormatMake, DetectFormat(lines))
}

func TestDetectsGenericForUnrecognisedInput(t *testing.T) {
	lines := []string{"INFO Starting application", "DEBUG Initializing"}
	assert.Equal(t, LogFormatGeneric, DetectFormat(lines))
}

// ── Level classifier tests ──────────────────────────────────────────

func TestLevelClassifierWordBoundaryMatches(t *testing.T) {
	lc := New(DefaultConfig())
	lines := lc.parseLines([]string{"ERROR: critical", "warning: x", "INFO: x", "no level here"})
	assert.Equal(t, LogLevelError, lines[0].Level)
	assert.Equal(t, LogLevelWarn, lines[1].Level)
	assert.Equal(t, LogLevelInfo, lines[2].Level)
	assert.Equal(t, LogLevelUnknown, lines[3].Level)
}

func TestLevelClassifierDoesNotOverfireOnSubstrings(t *testing.T) {
	lc := New(DefaultConfig())
	lines := lc.parseLines([]string{"informant arrested", "errorless code", "warned-off"})
	assert.Equal(t, LogLevelUnknown, lines[0].Level)
	assert.Equal(t, LogLevelUnknown, lines[1].Level)
	assert.Equal(t, LogLevelUnknown, lines[2].Level)
}

// ── Stack trace tests ───────────────────────────────────────────────

func TestChainedExceptionTracesSurviveBlankLines(t *testing.T) {
	lc := New(DefaultConfig())
	lines := lc.parseLines([]string{
		"Traceback (most recent call last):",
		`  File "a.py", line 1, in <module>`,
		"ValueError: x",
		"",
		"During handling of the above exception, another exception occurred:",
		"",
		"Traceback (most recent call last):",
		`  File "b.py", line 2, in <module>`,
		"RuntimeError: y",
	})
	for i := 0; i < 9; i++ {
		assert.True(t, lines[i].IsStackTrace,
			"line %d: '%s' expected is_stack_trace=true", i, lines[i].Content)
	}
}

// ── Dedupe tests ────────────────────────────────────────────────────

func TestDedupePreservesDistinctMessages(t *testing.T) {
	warnings := []LogLine{
		{LineNumber: 0, Content: "segfault at 0xdeadbeef in thread main"},
		{LineNumber: 1, Content: "heap overflow at 0xcafef00d in thread worker"},
	}
	deduped := dedupeSimilar(warnings)
	assert.Equal(t, 2, len(deduped))
}

func TestDedupeCollapsesGenuinelyRepeatedWarnings(t *testing.T) {
	warnings := []LogLine{
		{LineNumber: 0, Content: "warning: file /tmp/a/123 issue"},
		{LineNumber: 1, Content: "warning: file /tmp/b/999 issue"},
	}
	deduped := dedupeSimilar(warnings)
	assert.Equal(t, 1, len(deduped))
}

// ── Selection tests ─────────────────────────────────────────────────

func TestSelectLinesCapsGlobalTotal(t *testing.T) {
	c := New(LogCompressorConfig{
		MaxTotalLines:      12,
		StackTraceMaxLines: 2,
		MinLinesForCCR:     1,
		KeepFirstError:     true,
		KeepLastError:      true,
		MaxErrors:          10,
		MaxWarnings:        5,
		MaxStackTraces:     3,
		ErrorContextLines:  3,
		DedupeWarnings:     true,
		KeepSummaryLines:   true,
		EnableCCR:          true,
		MinCompressionRatioForCCR: 0.5,
	})
	var content strings.Builder
	for i := 0; i < 60; i++ {
		fmt.Fprintf(&content, "INFO line %d\n", i)
	}
	content.WriteString("ERROR something exploded\n")
	content.WriteString("ERROR another failure\n")
	result, stats := c.Compress(content.String(), 1.0)
	assert.LessOrEqual(t, result.CompressedLineCount, 12)
	require.NotNil(t, stats.Format)
	assert.Equal(t, LogFormatGeneric, *stats.Format)
}

// ── Empty / short input tests ───────────────────────────────────────

func TestEmptyInputReturnsUnchanged(t *testing.T) {
	c := New(DefaultConfig())
	result, _ := c.Compress("a\nb\nc", 1.0)
	assert.Equal(t, "a\nb\nc", result.Compressed)
	assert.InDelta(t, 1.0, result.CompressionRatio, 1e-9)
}

// ── CCR tests ───────────────────────────────────────────────────────

func TestCCRMarkerEmittedWhenThresholdsClear(t *testing.T) {
	c := New(LogCompressorConfig{
		MaxTotalLines:           5,
		MinLinesForCCR:          5,
		MinCompressionRatioForCCR: 0.95,
		KeepFirstError:          true,
		KeepLastError:           true,
		MaxErrors:               10,
		MaxWarnings:             5,
		MaxStackTraces:          3,
		StackTraceMaxLines:      20,
		ErrorContextLines:       3,
		DedupeWarnings:          true,
		KeepSummaryLines:        true,
		EnableCCR:               true,
	})
	var content strings.Builder
	for i := 0; i < 50; i++ {
		fmt.Fprintf(&content, "INFO line %d\n", i)
	}
	content.WriteString("ERROR boom\n")
	store := ccr.NewInMemoryStore()
	result, stats := c.CompressWithStore(content.String(), 1.0, store)
	assert.NotNil(t, result.CacheKey, "cache_key should be populated")
	assert.True(t, stats.CCREmitted)
	key := *result.CacheKey
	got, ok := store.Get(key)
	assert.True(t, ok)
	assert.Equal(t, content.String(), string(got))
}

// ── Format output test ──────────────────────────────────────────────

func TestFormatOutputEmitsSummaryWithOmittedCount(t *testing.T) {
	lc := New(DefaultConfig())
	allLines := []LogLine{
		{LineNumber: 0, Content: "ERROR a", Level: LogLevelError},
		{LineNumber: 1, Content: "WARN b", Level: LogLevelWarn},
		{LineNumber: 2, Content: "INFO c", Level: LogLevelInfo},
		{LineNumber: 3, Content: "INFO d", Level: LogLevelInfo},
	}
	selected := []LogLine{allLines[0]}
	output, stats := lc.formatOutput(selected, allLines)
	assert.Contains(t, output, "[3 lines omitted: 1 ERROR, 1 WARN, 2 INFO]")
	assert.Equal(t, int64(1), stats["errors"])
	assert.Equal(t, int64(2), stats["info"])
}

// ── Score cap test ──────────────────────────────────────────────────

func TestScoreLineCapsAtOnePointZero(t *testing.T) {
	line := &LogLine{
		Level:        LogLevelError,
		IsStackTrace: true,
		IsSummary:    true,
	}
	assert.InDelta(t, 1.0, scoreLogLine(line), 1e-6)
}

// ── Select with first/last test ─────────────────────────────────────

func TestSelectWithFirstLastKeepsBothEndpoints(t *testing.T) {
	lc := New(DefaultConfig())
	lines := make([]LogLine, 5)
	for i := 0; i < 5; i++ {
		lines[i] = LogLine{
			LineNumber: i,
			Content:    fmt.Sprintf("line %d", i),
			Score:      0.1,
		}
	}
	lines[2].Score = 0.9
	kept := lc.selectWithFirstLast(lines, 3)
	lineNums := make([]int, len(kept))
	for i, l := range kept {
		lineNums[i] = l.LineNumber
	}
	assert.Contains(t, lineNums, 0)
	assert.Contains(t, lineNums, 4)
	assert.Contains(t, lineNums, 2)
}
