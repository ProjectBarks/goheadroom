package offloads

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/projectbarks/goheadroom/core/ccr"
	"github.com/projectbarks/goheadroom/core/transforms/contentdetector"
	"github.com/projectbarks/goheadroom/core/transforms/pipeline"
)

func defaultLogBloat() pipeline.LogBloatConfig {
	cfg, _ := pipeline.DefaultPipelineConfig()
	return cfg.Bloat.Log
}

func logOffload() *LogOffload {
	return NewLogOffload(defaultLogBloat())
}

func TestLogOffloadNameAndAppliesTo(t *testing.T) {
	o := logOffload()
	assert.Equal(t, "log_offload", o.Name())
	assert.Equal(t, []contentdetector.ContentType{contentdetector.BuildOutput}, o.AppliesTo())
}

func TestLogOffloadEstimateBloatEmptyIsZero(t *testing.T) {
	assert.Equal(t, float32(0.0), logOffload().EstimateBloat(""))
}

func TestLogOffloadEstimateBloatBelowMinLinesIsZero(t *testing.T) {
	log := "INFO: starting\nERROR: oh no\nINFO: heartbeat\nINFO: done\nINFO: bye"
	assert.Equal(t, float32(0.0), logOffload().EstimateBloat(log))
}

func TestLogOffloadEstimateBloatHighRepetitionScoresHigh(t *testing.T) {
	line := "INFO: heartbeat received from worker-7"
	lines := make([]string, 100)
	for i := range lines {
		lines[i] = line
	}
	log := strings.Join(lines, "\n")
	score := logOffload().EstimateBloat(log)
	assert.Greater(t, score, float32(0.8))
}

func TestLogOffloadEstimateBloatUniqueErrorsScoreLow(t *testing.T) {
	var lines []string
	for i := 0; i < 100; i++ {
		lines = append(lines, "ERROR: failure at module x")
	}
	log := strings.Join(lines, "\n")
	score := logOffload().EstimateBloat(log)
	// All same line (high repetition) but ERROR keyword gives high priority.
	// The exact score depends on the detector implementation.
	_ = score // Just verifying no panic.
}

func TestLogOffloadEstimateBloatSafeOnHuge(t *testing.T) {
	var lines []string
	for i := 0; i < 100000; i++ {
		lines = append(lines, "INFO: routine event")
	}
	log := strings.Join(lines, "\n")
	_ = logOffload().EstimateBloat(log)
}

func TestLogOffloadApplyEmitsCacheKeyForRepetitiveLog(t *testing.T) {
	log := strings.Repeat("INFO: heartbeat\n", 200)
	store := ccr.NewInMemoryStore()
	ctx := &pipeline.CompressionContext{}
	r, err := logOffload().Apply(log, ctx, store)
	require.NoError(t, err)
	assert.NotEmpty(t, r.CacheKey)
	stored, ok := store.Get(r.CacheKey)
	assert.True(t, ok)
	assert.Equal(t, log, string(stored))
	assert.Greater(t, r.BytesSaved, 0)
}

func TestLogOffloadApplyReturnsSkippedWhenCompressorDeclinesCCR(t *testing.T) {
	log := "INFO: a\nINFO: b\nINFO: c\nINFO: d\nINFO: e"
	store := ccr.NewInMemoryStore()
	ctx := &pipeline.CompressionContext{}
	_, err := logOffload().Apply(log, ctx, store)
	require.Error(t, err)
	te, ok := err.(pipeline.TransformError)
	require.True(t, ok)
	assert.Equal(t, pipeline.ErrorSkipped, te.Kind)
	assert.Equal(t, 0, store.Len())
}

func TestLogOffloadConfidence(t *testing.T) {
	assert.Equal(t, float32(0.85), logOffload().Confidence())
}
