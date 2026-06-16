package searchcompressor

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/projectbarks/goheadroom/core/ccr"
)

// ── Config test ─────────────────────────────────────────────────────

func TestDefaultSearchCompressorConfig(t *testing.T) {
	cfg := DefaultConfig()
	assert.Equal(t, 5, cfg.MaxMatchesPerFile)
	assert.True(t, cfg.AlwaysKeepFirst)
	assert.True(t, cfg.AlwaysKeepLast)
	assert.Equal(t, 30, cfg.MaxTotalMatches)
	assert.Equal(t, 15, cfg.MaxFiles)
	assert.Empty(t, cfg.ContextKeywords)
	assert.True(t, cfg.BoostErrors)
	assert.True(t, cfg.EnableCCR)
	assert.Equal(t, 10, cfg.MinMatchesForCCR)
	assert.InDelta(t, 0.8, cfg.MinCompressionRatioForCCR, 1e-9)
}

// ── Parse tests ─────────────────────────────────────────────────────

func TestParsesStandardGrepLine(t *testing.T) {
	file, n, content, ok := parseMatchLine("src/main.py:42:def main():")
	require.True(t, ok)
	assert.Equal(t, "src/main.py", file)
	assert.Equal(t, uint64(42), n)
	assert.Equal(t, "def main():", content)
}

func TestParsesRipgrepContextLine(t *testing.T) {
	file, n, content, ok := parseMatchLine("src/main.py-43-context after match")
	require.True(t, ok)
	assert.Equal(t, "src/main.py", file)
	assert.Equal(t, uint64(43), n)
	assert.Equal(t, "context after match", content)
}

func TestHandlesWindowsPathWithBackslash(t *testing.T) {
	file, n, content, ok := parseMatchLine(`C:\Users\foo\bar.py:42:def main():`)
	require.True(t, ok)
	assert.Equal(t, `C:\Users\foo\bar.py`, file)
	assert.Equal(t, uint64(42), n)
	assert.Equal(t, "def main():", content)
}

func TestHandlesWindowsPathWithForwardSlash(t *testing.T) {
	file, n, content, ok := parseMatchLine("C:/Users/foo/bar.py:42:def main():")
	require.True(t, ok)
	assert.Equal(t, "C:/Users/foo/bar.py", file)
	assert.Equal(t, uint64(42), n)
	assert.Equal(t, "def main():", content)
}

func TestHandlesDashesInFilenameWithRipgrepContext(t *testing.T) {
	file, n, content, ok := parseMatchLine("pre-commit-config.yaml-42-fail_fast: true")
	require.True(t, ok)
	assert.Equal(t, "pre-commit-config.yaml", file)
	assert.Equal(t, uint64(42), n)
	assert.Equal(t, "fail_fast: true", content)
}

func TestPreservesColonsInMatchContent(t *testing.T) {
	file, n, content, ok := parseMatchLine(`config.py:10:DATABASE_URL = "postgres://user:pass@host:5432/db"`)
	require.True(t, ok)
	assert.Equal(t, "config.py", file)
	assert.Equal(t, uint64(10), n)
	assert.Equal(t, `DATABASE_URL = "postgres://user:pass@host:5432/db"`, content)
}

func TestRejectsLinesWithoutLineNumberMarker(t *testing.T) {
	_, _, _, ok := parseMatchLine("just a normal line of prose")
	assert.False(t, ok)
	_, _, _, ok = parseMatchLine("file.py:not-a-number:something")
	assert.False(t, ok)
	_, _, _, ok = parseMatchLine(":42:something")
	assert.False(t, ok)
}

func TestRejectsNegativeLineNumbers(t *testing.T) {
	_, _, _, ok := parseMatchLine("src/file.py:-1:invalid")
	assert.False(t, ok)
	_, _, _, ok = parseMatchLine("src/file.py--1-invalid")
	assert.False(t, ok)
}

func TestParserGroupsByFileAndCounts(t *testing.T) {
	compressor := New(DefaultConfig())
	content := "src/main.py:42:def main():\nsrc/main.py:43:    pass\nsrc/utils.py:15:def util():\njust prose, no marker\nsrc/main.py-44-context line"
	var stats SearchCompressorStats
	parsed := compressor.parseSearchResults(content, &stats)
	assert.Equal(t, 2, len(parsed))
	assert.Equal(t, 3, len(parsed["src/main.py"].Matches))
	assert.Equal(t, 1, len(parsed["src/utils.py"].Matches))
	assert.Equal(t, 1, stats.LinesUnparsed)
	assert.Equal(t, 5, stats.LinesScanned)
}

// ── Scoring test ────────────────────────────────────────────────────

func TestScoringBoostsErrorLinesInSearchContext(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ContextKeywords = []string{"auth"}
	compressor := New(cfg)

	files := map[string]*FileMatches{
		"src/auth.py": {
			File: "src/auth.py",
			Matches: []SearchMatch{
				{File: "src/auth.py", LineNumber: 10, Content: "ERROR auth failed"},
				{File: "src/auth.py", LineNumber: 11, Content: "plain auth line"},
			},
		},
	}

	compressor.scoreMatches(files, "find auth error")
	scored := files["src/auth.py"].Matches
	// ERROR + auth-keyword + context-word boosts -> clamped to 1.0
	assert.InDelta(t, 1.0, scored[0].Score, 0.001)
	// Plain line gets only context-word + keyword boosts (no error)
	assert.Greater(t, scored[1].Score, float32(0.0))
	assert.Less(t, scored[1].Score, float32(1.0))
}

// ── Selection test ──────────────────────────────────────────────────

func TestSelectRespectsPerFileCapAndGlobalCap(t *testing.T) {
	compressor := New(SearchCompressorConfig{
		MaxMatchesPerFile:         2,
		MaxTotalMatches:           6,
		MaxFiles:                  2,
		AlwaysKeepFirst:           true,
		AlwaysKeepLast:            true,
		BoostErrors:               true,
		EnableCCR:                 true,
		MinMatchesForCCR:          10,
		MinCompressionRatioForCCR: 0.8,
	})
	files := make(map[string]*FileMatches)
	for _, entry := range []struct {
		file string
		n    int
	}{{"a.py", 5}, {"b.py", 4}, {"c.py", 3}} {
		fm := &FileMatches{File: entry.file}
		for i := 0; i < entry.n; i++ {
			fm.Matches = append(fm.Matches, SearchMatch{
				File: entry.file, LineNumber: uint64(i + 1),
				Content: fmt.Sprintf("line %d", i+1),
			})
		}
		files[entry.file] = fm
	}

	var stats SearchCompressorStats
	selected := compressor.selectMatches(files, 1.0, &stats)

	assert.Equal(t, 2, len(selected))
	assert.GreaterOrEqual(t, stats.FilesDropped, 1)
	for _, fm := range selected {
		assert.LessOrEqual(t, len(fm.Matches), 2)
		for i := 1; i < len(fm.Matches); i++ {
			assert.Less(t, fm.Matches[i-1].LineNumber, fm.Matches[i].LineNumber)
		}
	}
}

// ── Empty input test ────────────────────────────────────────────────

func TestEmptyInputReturnsUnchanged(t *testing.T) {
	compressor := New(DefaultConfig())
	result, _ := compressor.Compress("plain text only", "", 1.0)
	assert.Equal(t, 0, result.OriginalMatchCount)
	assert.Equal(t, "plain text only", result.Compressed)
	assert.InDelta(t, 1.0, result.CompressionRatio, 1e-9)
}

// ── CCR tests ───────────────────────────────────────────────────────

func TestCCRMarkerEmittedWhenThresholdsClear(t *testing.T) {
	compressor := New(SearchCompressorConfig{
		MaxMatchesPerFile:         2,
		MaxTotalMatches:           4,
		MinMatchesForCCR:          5,
		MinCompressionRatioForCCR: 0.95,
		AlwaysKeepFirst:           true,
		AlwaysKeepLast:            true,
		MaxFiles:                  15,
		BoostErrors:               true,
		EnableCCR:                 true,
	})
	var content strings.Builder
	for i := 1; i <= 12; i++ {
		fmt.Fprintf(&content, "src/main.py:%d:line content %d\n", i, i)
	}
	store := ccr.NewInMemoryStore()
	result, stats := compressor.CompressWithStore(content.String(), "", 1.0, store)
	assert.NotNil(t, result.CacheKey)
	assert.True(t, stats.CCREmitted)
	assert.Contains(t, result.Compressed, "[12 matches compressed to")
	key := *result.CacheKey
	got, ok := store.Get(key)
	assert.True(t, ok)
	assert.Equal(t, content.String(), string(got))
}

func TestCCRSkippedWhenBelowMinMatches(t *testing.T) {
	compressor := New(SearchCompressorConfig{
		MinMatchesForCCR:          100,
		MaxMatchesPerFile:         5,
		MaxTotalMatches:           30,
		MaxFiles:                  15,
		AlwaysKeepFirst:           true,
		AlwaysKeepLast:            true,
		BoostErrors:               true,
		EnableCCR:                 true,
		MinCompressionRatioForCCR: 0.8,
	})
	content := "src/main.py:1:hi\nsrc/main.py:2:bye\n"
	store := ccr.NewInMemoryStore()
	_, stats := compressor.CompressWithStore(content, "", 1.0, store)
	assert.False(t, stats.CCREmitted)
	require.NotNil(t, stats.CCRSkipReason)
	assert.Equal(t, "below min_matches_for_ccr", *stats.CCRSkipReason)
	assert.Equal(t, 0, store.Len())
}

func TestCCRSkippedWhenDisabled(t *testing.T) {
	cfg := DefaultConfig()
	cfg.EnableCCR = false
	compressor := New(cfg)
	var content strings.Builder
	for i := 1; i <= 20; i++ {
		fmt.Fprintf(&content, "src/main.py:%d:line\n", i)
	}
	store := ccr.NewInMemoryStore()
	_, stats := compressor.CompressWithStore(content.String(), "", 1.0, store)
	assert.False(t, stats.CCREmitted)
	require.NotNil(t, stats.CCRSkipReason)
	assert.Equal(t, "ccr disabled in config", *stats.CCRSkipReason)
}
