package offloads

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uber/goheadroom/ccr"
	"github.com/uber/goheadroom/transforms/contentdetector"
	"github.com/uber/goheadroom/transforms/pipeline"
)

func defaultSearchBloat() pipeline.SearchBloatConfig {
	cfg, _ := pipeline.DefaultPipelineConfig()
	return cfg.Bloat.Search
}

func searchOffload() *SearchOffload {
	return NewSearchOffload(defaultSearchBloat())
}

func TestSearchOffloadNameAndAppliesTo(t *testing.T) {
	o := searchOffload()
	assert.Equal(t, "search_offload", o.Name())
	assert.Equal(t, []contentdetector.ContentType{contentdetector.SearchResults}, o.AppliesTo())
}

func TestExtractFilePrefixHandlesGrep(t *testing.T) {
	assert.Equal(t, "src/utils.py", extractFilePrefix("src/utils.py:42:def foo():"))
}

func TestExtractFilePrefixHandlesRipgrepContext(t *testing.T) {
	assert.Equal(t, "src/main.py", extractFilePrefix("src/main.py-43-some context"))
}

func TestExtractFilePrefixHandlesDashedFilenames(t *testing.T) {
	assert.Equal(t, "pre-commit-config.yaml", extractFilePrefix("pre-commit-config.yaml:7:line"))
}

func TestExtractFilePrefixHandlesWindowsPaths(t *testing.T) {
	assert.Equal(t, `C:\Users\foo\bar.py`, extractFilePrefix(`C:\Users\foo\bar.py:42:line`))
}

func TestExtractFilePrefixRejectsNonMatches(t *testing.T) {
	assert.Equal(t, "", extractFilePrefix(""))
	assert.Equal(t, "", extractFilePrefix("just some text"))
	assert.Equal(t, "", extractFilePrefix("file:notdigits:content"))
}

func TestSearchOffloadEstimateBloatEmptyIsZero(t *testing.T) {
	assert.Equal(t, float32(0.0), searchOffload().EstimateBloat(""))
}

func TestSearchOffloadEstimateBloatBelowMinMatchesIsZero(t *testing.T) {
	s := "a.py:1:x\nb.py:2:y\nc.py:3:z"
	assert.Equal(t, float32(0.0), searchOffload().EstimateBloat(s))
}

func TestSearchOffloadEstimateBloatClusteredMatchesScoreHigh(t *testing.T) {
	var lines []string
	for i := 0; i < 100; i++ {
		lines = append(lines, fmt.Sprintf("utils.py:%d:line", i+1))
	}
	s := strings.Join(lines, "\n")
	score := searchOffload().EstimateBloat(s)
	assert.Greater(t, score, float32(0.9))
}

func TestSearchOffloadEstimateBloatDistributedMatchesScoreZero(t *testing.T) {
	var lines []string
	for i := 0; i < 20; i++ {
		lines = append(lines, fmt.Sprintf("file%d.py:1:line", i))
	}
	s := strings.Join(lines, "\n")
	score := searchOffload().EstimateBloat(s)
	assert.Equal(t, float32(0.0), score)
}

func TestSearchOffloadEstimateBloatModerateClustering(t *testing.T) {
	var lines []string
	for f := 0; f < 5; f++ {
		for line := 0; line < 6; line++ {
			lines = append(lines, fmt.Sprintf("file%d.py:%d:line", f, line+1))
		}
	}
	s := strings.Join(lines, "\n")
	score := searchOffload().EstimateBloat(s)
	assert.Greater(t, score, float32(0.4))
	assert.Less(t, score, float32(0.6))
}

func TestSearchOffloadApplyEmitsCacheKeyForClusteredInput(t *testing.T) {
	var lines []string
	for i := 0; i < 100; i++ {
		lines = append(lines, fmt.Sprintf("utils.py:%d:def fn_%d", i+1, i))
	}
	s := strings.Join(lines, "\n")
	store := ccr.NewInMemoryStore()
	ctx := &pipeline.CompressionContext{}
	r, err := searchOffload().Apply(s, ctx, store)
	require.NoError(t, err)
	assert.NotEmpty(t, r.CacheKey)
	stored, ok := store.Get(r.CacheKey)
	assert.True(t, ok)
	assert.Equal(t, s, string(stored))
}

func TestSearchOffloadApplySkippedWhenCompressorDeclinesCCR(t *testing.T) {
	s := "only.py:1:trivial"
	store := ccr.NewInMemoryStore()
	ctx := &pipeline.CompressionContext{}
	_, err := searchOffload().Apply(s, ctx, store)
	require.Error(t, err)
	te, ok := err.(pipeline.TransformError)
	require.True(t, ok)
	assert.Equal(t, pipeline.ErrorSkipped, te.Kind)
}

func TestSearchOffloadConfidence(t *testing.T) {
	assert.Equal(t, float32(0.85), searchOffload().Confidence())
}
