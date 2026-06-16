package transforms_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/projectbarks/goheadroom/core/ccr"
	"github.com/projectbarks/goheadroom/core/transforms/diffcompressor"
	"github.com/projectbarks/goheadroom/core/transforms/logcompressor"
	"github.com/projectbarks/goheadroom/core/transforms/searchcompressor"
)

const sampleDiff = `diff --git a/main.go b/main.go
--- a/main.go
+++ b/main.go
@@ -1,3 +1,4 @@
 package main
+import "fmt"
 func main() {
 }
`

const sampleSearch = `src/main.go:10:func main() {
src/main.go:15:    fmt.Println("hello")
src/lib.go:5:func Helper() string {
`

func buildSampleLog() string {
	var b strings.Builder
	for i := 0; i < 55; i++ {
		fmt.Fprintf(&b, "INFO line %d\n", i)
	}
	b.WriteString("ERROR something exploded\n")
	b.WriteString("warning: check config\n")
	return b.String()
}

func TestAllCompressorsWithCCRStore(t *testing.T) {
	store := ccr.NewInMemoryStore()

	// Compress a diff -- need enough lines to clear min_lines_for_ccr.
	dcCfg := diffcompressor.DefaultConfig()
	dcCfg.MinLinesForCCR = 3
	dc := diffcompressor.New(dcCfg)
	diffResult, _ := dc.CompressWithStore(sampleDiff, "", store)
	assert.Greater(t, diffResult.CompressedLineCount, 0)

	// Compress a log.
	lc := logcompressor.New(logcompressor.DefaultConfig())
	logResult, _ := lc.CompressWithStore(buildSampleLog(), 1.0, store)
	assert.Greater(t, logResult.CompressedLineCount, 0)

	// Compress search results.
	sc := searchcompressor.New(searchcompressor.DefaultConfig())
	searchResult, _ := sc.CompressWithStore(sampleSearch, "", 1.0, store)
	assert.Greater(t, searchResult.CompressedMatchCount, 0)

	// Verify store has entries.
	assert.Greater(t, store.Len(), 0)
}

func TestAllCompressorsEmptyInput(t *testing.T) {
	dc := diffcompressor.New(diffcompressor.DefaultConfig())
	assert.Equal(t, "", dc.Compress("", "").Compressed)

	lc := logcompressor.New(logcompressor.DefaultConfig())
	lr, _ := lc.Compress("", 1.0)
	assert.Equal(t, "", lr.Compressed)

	sc := searchcompressor.New(searchcompressor.DefaultConfig())
	sr, _ := sc.Compress("", "", 1.0)
	assert.Equal(t, "", sr.Compressed)
}

func TestAllCompressorsReturnConsistentMetadata(t *testing.T) {
	// Diff compressor with small threshold.
	dcCfg := diffcompressor.DefaultConfig()
	dcCfg.MinLinesForCCR = 3
	dc := diffcompressor.New(dcCfg)
	dr := dc.Compress(sampleDiff, "")
	assert.Greater(t, dr.FilesAffected, 0)
	assert.GreaterOrEqual(t, dr.Additions, 0)
	assert.GreaterOrEqual(t, dr.Deletions, 0)

	// Log compressor returns format detection.
	lc := logcompressor.New(logcompressor.DefaultConfig())
	lr, _ := lc.Compress(buildSampleLog(), 1.0)
	assert.NotEqual(t, "", lr.FormatDetected.String())
	assert.Greater(t, lr.OriginalLineCount, 0)

	// Search compressor returns file count.
	sc := searchcompressor.New(searchcompressor.DefaultConfig())
	sr, _ := sc.Compress(sampleSearch, "", 1.0)
	assert.Greater(t, sr.FilesAffected, 0)
	assert.GreaterOrEqual(t, sr.OriginalMatchCount, sr.CompressedMatchCount)
}

func TestCCRStoreRoundTrip(t *testing.T) {
	store := ccr.NewInMemoryStore()

	// Use a big enough diff to clear thresholds.
	var diffInput strings.Builder
	for i := 0; i < 10; i++ {
		fmt.Fprintf(&diffInput, "diff --git a/file_%d.go b/file_%d.go\n--- a/file_%d.go\n+++ b/file_%d.go\n@@ -1,5 +1,7 @@\n", i, i, i, i)
		for k := 0; k < 3; k++ {
			fmt.Fprintf(&diffInput, " ctx_%d_%d\n", k, i)
		}
		for k := 0; k < 2; k++ {
			fmt.Fprintf(&diffInput, "-old_%d_%d\n", k, i)
		}
		for k := 0; k < 4; k++ {
			fmt.Fprintf(&diffInput, "+new_%d_%d\n", k, i)
		}
	}

	dc := diffcompressor.New(diffcompressor.DefaultConfig())
	r, _ := dc.CompressWithStore(diffInput.String(), "", store)
	if r.CacheKey != nil {
		got, ok := store.Get(*r.CacheKey)
		require.True(t, ok)
		assert.Equal(t, diffInput.String(), string(got))
	}
}
