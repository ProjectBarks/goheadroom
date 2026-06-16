package offloads

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/projectbarks/goheadroom/core/ccr"
	"github.com/projectbarks/goheadroom/core/transforms/contentdetector"
	"github.com/projectbarks/goheadroom/core/transforms/pipeline"
)

func defaultDiffBloat() pipeline.DiffBloatConfig {
	cfg, _ := pipeline.DefaultPipelineConfig()
	return cfg.Bloat.Diff
}

func diffOffload() *DiffOffload {
	return NewDiffOffload(defaultDiffBloat())
}

func buildDiff(numFiles, contextPerFile, changesPerFile int) string {
	var s strings.Builder
	for f := 0; f < numFiles; f++ {
		total := contextPerFile + changesPerFile
		s.WriteString(fmt.Sprintf("diff --git a/file%d.txt b/file%d.txt\n", f, f))
		s.WriteString(fmt.Sprintf("--- a/file%d.txt\n+++ b/file%d.txt\n", f, f))
		s.WriteString(fmt.Sprintf("@@ -1,%d +1,%d @@\n", total, total))
		for i := 0; i < contextPerFile; i++ {
			s.WriteString(" context line\n")
		}
		for c := 0; c < changesPerFile; c++ {
			s.WriteString(fmt.Sprintf("-removed line %d\n", c))
			s.WriteString(fmt.Sprintf("+added line %d\n", c))
		}
	}
	return s.String()
}

func TestDiffOffloadNameAndAppliesTo(t *testing.T) {
	o := diffOffload()
	assert.Equal(t, "diff_offload", o.Name())
	assert.Equal(t, []contentdetector.ContentType{contentdetector.GitDiff}, o.AppliesTo())
}

func TestDiffOffloadEstimateBloatEmptyIsZero(t *testing.T) {
	assert.Equal(t, float32(0.0), diffOffload().EstimateBloat(""))
}

func TestDiffOffloadEstimateBloatBelowMinLinesIsZero(t *testing.T) {
	small := buildDiff(1, 5, 1)
	assert.Equal(t, float32(0.0), diffOffload().EstimateBloat(small))
}

func TestDiffOffloadEstimateBloatDenseDiffScoresZero(t *testing.T) {
	diff := buildDiff(2, 5, 60)
	score := diffOffload().EstimateBloat(diff)
	assert.Equal(t, float32(0.0), score)
}

func TestDiffOffloadEstimateBloatContextHeavyDiffScoresHigh(t *testing.T) {
	diff := buildDiff(1, 200, 5)
	score := diffOffload().EstimateBloat(diff)
	assert.Greater(t, score, float32(0.7))
}

func TestDiffOffloadEstimateBloatAtThresholdScoresZero(t *testing.T) {
	diff := buildDiff(1, 60, 20)
	score := diffOffload().EstimateBloat(diff)
	assert.Equal(t, float32(0.0), score)
}

func TestDiffOffloadEstimateBloatSafeOnHuge(t *testing.T) {
	diff := buildDiff(50, 100, 50)
	_ = diffOffload().EstimateBloat(diff)
}

func TestDiffOffloadApplyEmitsKeyAndPersistsOriginal(t *testing.T) {
	diff := buildDiff(1, 200, 5)
	store := ccr.NewInMemoryStore()
	ctx := &pipeline.CompressionContext{}
	r, err := diffOffload().Apply(diff, ctx, store)
	require.NoError(t, err)
	assert.NotEmpty(t, r.CacheKey)
	stored, ok := store.Get(r.CacheKey)
	assert.True(t, ok)
	assert.Equal(t, diff, string(stored))
}

func TestDiffOffloadApplySkippedWhenCompressorDeclinesccr(t *testing.T) {
	diff := buildDiff(1, 5, 2)
	store := ccr.NewInMemoryStore()
	ctx := &pipeline.CompressionContext{}
	_, err := diffOffload().Apply(diff, ctx, store)
	require.Error(t, err)
	te, ok := err.(pipeline.TransformError)
	require.True(t, ok)
	assert.Equal(t, pipeline.ErrorSkipped, te.Kind)
	assert.Equal(t, 0, store.Len())
}

func TestDiffOffloadConfidence(t *testing.T) {
	assert.Equal(t, float32(0.85), diffOffload().Confidence())
}
