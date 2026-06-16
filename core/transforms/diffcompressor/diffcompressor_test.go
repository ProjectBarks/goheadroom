package diffcompressor

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/projectbarks/goheadroom/core/ccr"
)

// ── Constant tests ──────────────────────────────────────────────────

func TestScoreConstants(t *testing.T) {
	assert.InDelta(t, 0.03, ScoreChangeDensityWeight, 1e-9)
	assert.InDelta(t, 0.3, ScoreChangeDensityCap, 1e-9)
	assert.InDelta(t, 0.2, ScoreContextWordWeight, 1e-9)
	assert.Equal(t, 2, ScoreContextMinWordLen)
	assert.InDelta(t, 0.3, ScorePriorityPatternBoost, 1e-9)
	assert.InDelta(t, 1.0, ScoreTotalCap, 1e-9)
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	assert.Equal(t, 2, cfg.MaxContextLines)
	assert.Equal(t, 10, cfg.MaxHunksPerFile)
	assert.Equal(t, 20, cfg.MaxFiles)
	assert.True(t, cfg.AlwaysKeepAdditions)
	assert.True(t, cfg.AlwaysKeepDeletions)
	assert.True(t, cfg.EnableCCR)
	assert.Equal(t, 50, cfg.MinLinesForCCR)
	assert.InDelta(t, 0.8, cfg.MinCompressionRatioForCCR, 1e-9)
}

// ── Ported from Rust tests ──────────────────────────────────────────

func TestShortInputPassesThrough(t *testing.T) {
	c := New(DefaultConfig())
	input := "diff --git a/x b/x\n@@ -1 +1 @@\n-a\n+b"
	r := c.Compress(input, "")
	assert.Equal(t, input, r.Compressed)
	assert.Equal(t, 4, r.OriginalLineCount)
	assert.Equal(t, 4, r.CompressedLineCount)
	assert.Equal(t, 0, r.FilesAffected)
	assert.Nil(t, r.CacheKey)
}

func TestNonDiffInputPassesThrough(t *testing.T) {
	c := New(DefaultConfig())
	input := strings.Repeat("this is not a diff\n", 60)
	r := c.Compress(input, "")
	assert.Equal(t, input, r.Compressed)
	assert.Equal(t, 0, r.FilesAffected)
}

func TestMD5Hex24MatchesPython(t *testing.T) {
	assert.Equal(t, "5d41402abc4b2a76b9719d91", md5Hex24("hello"))
	assert.Equal(t, "d41d8cd98f00b204e9800998", md5Hex24(""))
}

func TestCountSplitLinesMatchesPythonSplitN(t *testing.T) {
	assert.Equal(t, 1, countSplitLines(""))
	assert.Equal(t, 1, countSplitLines("a"))
	assert.Equal(t, 2, countSplitLines("a\n"))
	assert.Equal(t, 2, countSplitLines("a\nb"))
	assert.Equal(t, 2, countSplitLines("\n"))
}

func TestStatsAreEmittedWithCompressWithStats(t *testing.T) {
	c := New(DefaultConfig())
	input := strings.Repeat("noise\n", 60)
	_, stats := c.CompressWithStats(input, "")
	assert.Equal(t, 61, stats.InputLines) // 60 newlines -> 61 elements
	assert.Equal(t, 61, stats.OutputLines)
	assert.InDelta(t, 1.0, stats.CompressionRatio, 1e-9)
	assert.Empty(t, stats.ParseWarnings)
	assert.NotNil(t, stats.CCRSkippedReason)
}

// buildSyntheticDiff builds an n-file diff matching the parity fixture shape.
func buildSyntheticDiff(nFiles int) string {
	var s strings.Builder
	for i := 0; i < nFiles; i++ {
		fmt.Fprintf(&s, "diff --git a/file_%d.py b/file_%d.py\n--- a/file_%d.py\n+++ b/file_%d.py\n@@ -1,10 +1,12 @@\n", i, i, i, i)
		for k := 0; k < 5; k++ {
			fmt.Fprintf(&s, " context_%d_%d\n", k, i)
		}
		for k := 0; k < 3; k++ {
			fmt.Fprintf(&s, "-removed_%d_%d\n", k, i)
		}
		for k := 0; k < 5; k++ {
			fmt.Fprintf(&s, "+added_%d_%d\n", k, i)
		}
		for k := 0; k < 5; k++ {
			fmt.Fprintf(&s, " tail_%d_%d\n", k, i)
		}
	}
	s.WriteString("# variant 1")
	return s.String()
}

func TestSyntheticEightFileDiffMatchesKnownShape(t *testing.T) {
	c := New(DefaultConfig())
	input := buildSyntheticDiff(8)
	r := c.Compress(input, "")
	assert.Equal(t, 177, r.OriginalLineCount)
	assert.Equal(t, 8, r.FilesAffected)
	assert.Equal(t, 40, r.Additions)
	assert.Equal(t, 24, r.Deletions)
	assert.Equal(t, 8, r.HunksKept)
	assert.Equal(t, 0, r.HunksRemoved)
	assert.Equal(t, 129, r.CompressedLineCount)
	assert.NotNil(t, r.CacheKey)
}

// buildNHunkDiff builds a single-file diff with n hunks.
func buildNHunkDiff(n int) string {
	var s strings.Builder
	s.WriteString("diff --git a/big.py b/big.py\n--- a/big.py\n+++ b/big.py\n")
	for i := 0; i < n; i++ {
		start := i*100 + 1
		fmt.Fprintf(&s, "@@ -%d,6 +%d,6 @@\n", start, start)
		fmt.Fprintf(&s, " ctx_a_%d\n", i)
		fmt.Fprintf(&s, " ctx_b_%d\n", i)
		fmt.Fprintf(&s, "-old_%d\n", i)
		fmt.Fprintf(&s, "+new_%d\n", i)
		fmt.Fprintf(&s, " ctx_c_%d\n", i)
		fmt.Fprintf(&s, " ctx_d_%d\n", i)
	}
	return s.String()
}

func TestMaxHunksPerFileCapDropsExcessAndRecordsStats(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxHunksPerFile = 10
	input := buildNHunkDiff(15)
	result, stats := New(cfg).CompressWithStats(input, "")

	assert.Equal(t, 10, result.HunksKept, "kept 10 hunks")
	assert.Equal(t, 5, result.HunksRemoved, "dropped 5")
	assert.Equal(t, 15, stats.HunksTotal)
	assert.Equal(t, 5, stats.HunksDropped)
	perFileTotal := 0
	for _, v := range stats.HunksDroppedPerFile {
		perFileTotal += v
	}
	assert.Equal(t, 5, perFileTotal)
	assert.GreaterOrEqual(t, stats.LargestHunkDroppedLines, 6)
}

func TestMaxFilesCapDropsFilesAndRecordsNamesInStats(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxFiles = 20
	input := buildSyntheticDiff(25)
	_, stats := New(cfg).CompressWithStats(input, "")

	assert.Equal(t, 25, stats.FilesTotal)
	assert.Equal(t, 20, stats.FilesKept)
	require.Len(t, stats.FilesDropped, 5, "expected 5 dropped file labels")
	for _, label := range stats.FilesDropped {
		assert.Contains(t, label, "-> ", "label %q should contain ` -> `", label)
	}
}

func TestFileModeNormalizationIsRecordedForExecutableBit(t *testing.T) {
	input := "diff --git a/script.sh b/script.sh\n" +
		"new file mode 100755\n" +
		"--- /dev/null\n" +
		"+++ b/script.sh\n" +
		"@@ -0,0 +1,3 @@\n" +
		"+#!/bin/sh\n" +
		"+echo hi\n" +
		"+exit 0\n"
	// Pad to clear min_lines_for_ccr.
	for i := 0; i < 50; i++ {
		input += "# pad\n"
	}
	_, stats := New(DefaultConfig()).CompressWithStats(input, "")
	require.Len(t, stats.FileModeNormalizations, 1)
	label := stats.FileModeNormalizations[0][0]
	original := stats.FileModeNormalizations[0][1]
	assert.Contains(t, label, "script.sh")
	assert.Equal(t, "new file mode 100755", original)
}

func TestBinaryFilesSimplificationIsRecorded(t *testing.T) {
	input := "diff --git a/img.png b/img.png\n" +
		"Binary files a/img.png and b/img.png differ\n"
	for i := 0; i < 60; i++ {
		input += "# pad\n"
	}
	_, stats := New(DefaultConfig()).CompressWithStats(input, "")
	require.Len(t, stats.BinaryFilesSimplified, 1)
	assert.Equal(t, "Binary files a/img.png and b/img.png differ", stats.BinaryFilesSimplified[0])
}

func TestMinCompressionRatioForCCRIsConfigurable(t *testing.T) {
	// Default 0.8: 8-file synthetic compresses 177->129 (ratio 0.729) -> CCR emitted.
	r := New(DefaultConfig()).Compress(buildSyntheticDiff(8), "")
	assert.NotNil(t, r.CacheKey, "default 0.8 should emit CCR")

	// 0.5: ratio 0.729 does NOT beat 0.5 -> no CCR.
	cfg := DefaultConfig()
	cfg.MinCompressionRatioForCCR = 0.5
	r2, stats := New(cfg).CompressWithStats(buildSyntheticDiff(8), "")
	assert.Nil(t, r2.CacheKey, "0.5 threshold should suppress CCR")
	assert.False(t, stats.CacheKeyEmitted)
	assert.NotNil(t, stats.CCRSkippedReason)
}

func TestCompressWithStorePersistsOriginalUnderCacheKey(t *testing.T) {
	store := ccr.NewInMemoryStore()
	input := buildSyntheticDiff(8)
	r, stats := New(DefaultConfig()).CompressWithStore(input, "", store)
	require.NotNil(t, r.CacheKey)
	key := *r.CacheKey
	assert.True(t, stats.CacheKeyEmitted)
	assert.Contains(t, r.Compressed, fmt.Sprintf("hash=%s", key))
	got, ok := store.Get(key)
	assert.True(t, ok)
	assert.Equal(t, input, string(got))
}

func TestCompressWithStoreNilMatchesCompressWithStatsBehavior(t *testing.T) {
	input := buildSyntheticDiff(8)
	legacyResult, _ := New(DefaultConfig()).CompressWithStats(input, "")
	newResult, _ := New(DefaultConfig()).CompressWithStore(input, "", nil)
	assert.Equal(t, legacyResult.Compressed, newResult.Compressed)
	assert.Equal(t, legacyResult.CacheKey, newResult.CacheKey)
}

func TestCompressWithStoreNoOpWhenCCRSkipped(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MinCompressionRatioForCCR = 0.1
	store := ccr.NewInMemoryStore()
	r, _ := New(cfg).CompressWithStore(buildSyntheticDiff(8), "", store)
	assert.Nil(t, r.CacheKey)
	assert.Equal(t, 0, store.Len())
}

// ── Bug-fix tests ───────────────────────────────────────────────────

func TestBugfixRenameMarkersArePreservedInOutput(t *testing.T) {
	input := "diff --git a/old.py b/new.py\n" +
		"similarity index 92%\n" +
		"rename from old.py\n" +
		"rename to new.py\n" +
		"--- a/old.py\n" +
		"+++ b/new.py\n" +
		"@@ -1,3 +1,3 @@\n" +
		" ctx_a\n" +
		"-old_line\n" +
		"+new_line\n" +
		" ctx_b\n"
	cfg := DefaultConfig()
	cfg.MinLinesForCCR = 5
	r := New(cfg).Compress(input, "")
	assert.Contains(t, r.Compressed, "similarity index 92%")
	assert.Contains(t, r.Compressed, "rename from old.py")
	assert.Contains(t, r.Compressed, "rename to new.py")
}

func TestBugfixCombinedDiff3WayContentIsParsedAndEmitted(t *testing.T) {
	input := "diff --git a/merge.py b/merge.py\n" +
		"--- a/merge.py\n" +
		"+++ b/merge.py\n" +
		"@@@ -1,3 -1,3 +1,4 @@@\n" +
		"  unchanged_a\n" +
		" -old_branch_1\n" +
		"- old_branch_2\n" +
		"++new_in_merge\n" +
		" +new_added\n" +
		"  unchanged_b\n"
	cfg := DefaultConfig()
	cfg.MinLinesForCCR = 5
	r, stats := New(cfg).CompressWithStats(input, "")
	assert.Contains(t, r.Compressed, "@@@ -1,3 -1,3 +1,4 @@@")
	assert.Contains(t, r.Compressed, "++new_in_merge")
	assert.Greater(t, stats.FilesTotal, 0, "parser found no files")
}

func TestBugfixNoNewlineMarkerPreservedDespiteDistance(t *testing.T) {
	input := "diff --git a/last.txt b/last.txt\n" +
		"--- a/last.txt\n" +
		"+++ b/last.txt\n" +
		"@@ -1,8 +1,8 @@\n" +
		"-old_first\n" +
		"+new_first\n" +
		" ctx_a\n" +
		" ctx_b\n" +
		" ctx_c\n" +
		" ctx_d\n" +
		" ctx_e\n" +
		" ctx_f\n" +
		"\\ No newline at end of file\n"
	cfg := DefaultConfig()
	cfg.MinLinesForCCR = 5
	r := New(cfg).Compress(input, "")
	assert.Contains(t, r.Compressed, "\\ No newline at end of file")
}

func TestGapDiffCombinedHeaderStartsAFile(t *testing.T) {
	input := "diff --combined merge.py\n" +
		"index abc..def..ghi 100644\n" +
		"--- a/merge.py\n" +
		"+++ b/merge.py\n" +
		"@@@ -1,3 -1,3 +1,4 @@@\n" +
		"  ctx_a\n" +
		"- removed_p1\n" +
		" -removed_p2\n" +
		"++added_in_merge\n" +
		"  ctx_b\n"
	cfg := DefaultConfig()
	cfg.MinLinesForCCR = 5
	r := New(cfg).Compress(input, "")
	assert.Equal(t, 1, r.FilesAffected)
	assert.Contains(t, r.Compressed, "diff --combined merge.py")
	assert.Contains(t, r.Compressed, "@@@ -1,3 -1,3 +1,4 @@@")
	assert.Contains(t, r.Compressed, "++added_in_merge")
}

func TestGapDiffCCHeaderStartsAFile(t *testing.T) {
	input := "diff --cc cc_target.py\n" +
		"index abc..def..ghi\n" +
		"--- a/cc_target.py\n" +
		"+++ b/cc_target.py\n" +
		"@@@ -1,3 -1,3 +1,4 @@@\n" +
		"  ctx\n" +
		"- p1_removed\n" +
		" -p2_removed\n" +
		"++merge_added\n" +
		"  more_ctx\n"
	cfg := DefaultConfig()
	cfg.MinLinesForCCR = 5
	r := New(cfg).Compress(input, "")
	assert.Equal(t, 1, r.FilesAffected)
	assert.Contains(t, r.Compressed, "diff --cc cc_target.py")
	assert.Contains(t, r.Compressed, "++merge_added")
}

func TestBugfixPreDiffContentIsPreserved(t *testing.T) {
	input := "commit abc1234567890\n" +
		"Author: Tester <t@example.com>\n" +
		"Date:   Mon Apr 25 12:00:00 2026\n" +
		"\n    Refactor: rename and modify\n\n" +
		"diff --git a/x.py b/x.py\n" +
		"--- a/x.py\n" +
		"+++ b/x.py\n" +
		"@@ -1 +1 @@\n" +
		"-a\n" +
		"+b\n"
	cfg := DefaultConfig()
	cfg.MinLinesForCCR = 5
	r := New(cfg).Compress(input, "")
	assert.True(t, strings.HasPrefix(r.Compressed, "commit abc1234567890"))
	assert.Contains(t, r.Compressed, "Author: Tester")
	assert.Contains(t, r.Compressed, "Refactor: rename and modify")
	assert.Contains(t, r.Compressed, "diff --git a/x.py b/x.py")
	assert.Contains(t, r.Compressed, "-a")
	assert.Contains(t, r.Compressed, "+b")
}
