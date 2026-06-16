package transforms

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsDiffValidUnified(t *testing.T) {
	input := "--- a/file.txt\n+++ b/file.txt\n@@ -1,3 +1,4 @@\n line1\n line2\n+line3\n line4\n"
	assert.True(t, IsDiff(input))
}

func TestIsDiffGitFormat(t *testing.T) {
	input := "diff --git a/src/main.rs b/src/main.rs\nindex 1234567..abcdefg 100644\n--- a/src/main.rs\n+++ b/src/main.rs\n@@ -1,5 +1,6 @@\n fn main() {\n     println!(\"hello\");\n+    println!(\"world\");\n }\n"
	assert.True(t, IsDiff(input))
}

func TestIsDiffNotDiff(t *testing.T) {
	input := "This is just regular text.\nIt has multiple lines.\nBut it is not a diff."
	assert.False(t, IsDiff(input))
}

func TestIsDiffEmpty(t *testing.T) {
	assert.False(t, IsDiff(""))
}

func TestIsDiffOnlyMinusPlus(t *testing.T) {
	input := "--- something\n+++ something else"
	// Without @@ hunk headers, not a valid diff
	assert.False(t, IsDiff(input))
}

func TestDetectDiffSingleFile(t *testing.T) {
	input := "--- a/file.txt\n+++ b/file.txt\n@@ -1,3 +1,4 @@\n line1\n line2\n+line3\n line4\n"
	result, err := DetectDiff(input)
	assert.NoError(t, err)
	assert.True(t, result.IsDiff)
	assert.Equal(t, 1, result.FileCount)
}

func TestDetectDiffMultipleFiles(t *testing.T) {
	input := "--- a/file1.txt\n+++ b/file1.txt\n@@ -1,2 +1,3 @@\n line1\n+added\n line2\n--- a/file2.txt\n+++ b/file2.txt\n@@ -1 +1,2 @@\n existing\n+new line\n"
	result, err := DetectDiff(input)
	assert.NoError(t, err)
	assert.True(t, result.IsDiff)
	assert.Equal(t, 2, result.FileCount)
}

func TestDetectDiffNotDiff(t *testing.T) {
	input := "This is not a diff at all."
	result, err := DetectDiff(input)
	assert.NoError(t, err)
	assert.False(t, result.IsDiff)
	assert.Equal(t, 0, result.FileCount)
}

func TestDetectDiffAdditionsAndDeletions(t *testing.T) {
	input := "--- a/file.txt\n+++ b/file.txt\n@@ -1,4 +1,4 @@\n line1\n-old line\n+new line\n line3\n line4\n"
	result, err := DetectDiff(input)
	assert.NoError(t, err)
	assert.True(t, result.IsDiff)
	assert.Equal(t, 1, result.Additions)
	assert.Equal(t, 1, result.Deletions)
}

func TestIsDiffPartialDiff(t *testing.T) {
	input := "Some text before\n--- a/file.txt\n+++ b/file.txt\n@@ -1,2 +1,3 @@\n line1\n+added\n line2\nSome text after"
	// Embedded diffs should still be detected
	assert.True(t, IsDiff(input))
}

func TestDetectDiffBinaryFile(t *testing.T) {
	input := "diff --git a/image.png b/image.png\nBinary files a/image.png and b/image.png differ\n"
	result, err := DetectDiff(input)
	assert.NoError(t, err)
	// Binary diff -- the parser should detect it as diff-like
	assert.True(t, result.IsDiff)
}

func TestDetectDiffNewFile(t *testing.T) {
	input := "diff --git a/newfile.txt b/newfile.txt\nnew file mode 100644\n--- /dev/null\n+++ b/newfile.txt\n@@ -0,0 +1,3 @@\n+line1\n+line2\n+line3\n"
	result, err := DetectDiff(input)
	assert.NoError(t, err)
	assert.True(t, result.IsDiff)
	assert.Equal(t, 3, result.Additions)
	assert.Equal(t, 0, result.Deletions)
}

func TestDetectDiffDeletedFile(t *testing.T) {
	input := "diff --git a/removed.txt b/removed.txt\ndeleted file mode 100644\n--- a/removed.txt\n+++ /dev/null\n@@ -1,3 +0,0 @@\n-line1\n-line2\n-line3\n"
	result, err := DetectDiff(input)
	assert.NoError(t, err)
	assert.True(t, result.IsDiff)
	assert.Equal(t, 0, result.Additions)
	assert.Equal(t, 3, result.Deletions)
}
