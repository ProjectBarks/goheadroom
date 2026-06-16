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

func defaultDiffNoiseConfig() pipeline.DiffNoiseConfig {
	cfg, _ := pipeline.DefaultPipelineConfig()
	return cfg.Offload.DiffNoise
}

func diffNoise() *DiffNoise {
	return NewDiffNoise(defaultDiffNoiseConfig())
}

type diffFile struct {
	path string
	body []string
}

func buildNoiseDiff(files []diffFile) string {
	var s strings.Builder
	for _, f := range files {
		s.WriteString(fmt.Sprintf("diff --git a/%s b/%s\n", f.path, f.path))
		s.WriteString(fmt.Sprintf("--- a/%s\n+++ b/%s\n", f.path, f.path))
		s.WriteString("@@ -1 +1 @@\n")
		for _, line := range f.body {
			s.WriteString(line)
			s.WriteByte('\n')
		}
	}
	return s.String()
}

func TestDiffNoiseNameAndAppliesTo(t *testing.T) {
	o := diffNoise()
	assert.Equal(t, "diff_noise", o.Name())
	assert.Equal(t, []contentdetector.ContentType{contentdetector.GitDiff}, o.AppliesTo())
}

func TestDiffNoiseEstimateBloatBelowMinLinesZero(t *testing.T) {
	diff := buildNoiseDiff([]diffFile{{path: "Cargo.lock", body: []string{"-old", "+new"}}})
	assert.Equal(t, float32(0.0), diffNoise().EstimateBloat(diff))
}

func TestDiffNoiseEstimateBloatLockfileDominatesScoresHigh(t *testing.T) {
	var lockBody []string
	for i := 0; i < 200; i++ {
		lockBody = append(lockBody, fmt.Sprintf("-old%d", i), fmt.Sprintf("+new%d", i))
	}
	diff := buildNoiseDiff([]diffFile{
		{path: "Cargo.lock", body: lockBody},
		{path: "Cargo.toml", body: []string{`-foo = "1"`, `+foo = "2"`}},
	})
	score := diffNoise().EstimateBloat(diff)
	assert.Greater(t, score, float32(0.9))
}

func TestDiffNoiseEstimateBloatNoNoiseZero(t *testing.T) {
	var body []string
	for i := 0; i < 40; i++ {
		body = append(body, fmt.Sprintf("-old line %d", i), fmt.Sprintf("+new line %d", i))
	}
	diff := buildNoiseDiff([]diffFile{{path: "src/main.rs", body: body}})
	score := diffNoise().EstimateBloat(diff)
	assert.Equal(t, float32(0.0), score)
}

func TestDiffNoiseEstimateBloatWhitespaceOnlyScoresHigh(t *testing.T) {
	var body []string
	for i := 0; i < 40; i++ {
		body = append(body, fmt.Sprintf("-line %d   ", i), fmt.Sprintf("+line %d", i))
	}
	diff := buildNoiseDiff([]diffFile{{path: "src/main.rs", body: body}})
	score := diffNoise().EstimateBloat(diff)
	assert.Greater(t, score, float32(0.5))
}

func TestDiffNoiseApplyDropsLockfileAndStoresOriginal(t *testing.T) {
	var lockBody []string
	for i := 0; i < 200; i++ {
		lockBody = append(lockBody, fmt.Sprintf("-old%d", i), fmt.Sprintf("+new%d", i))
	}
	diff := buildNoiseDiff([]diffFile{
		{path: "Cargo.lock", body: lockBody},
		{path: "Cargo.toml", body: []string{`-foo = "1"`, `+foo = "2"`}},
	})
	store := ccr.NewInMemoryStore()
	ctx := &pipeline.CompressionContext{}
	r, err := diffNoise().Apply(diff, ctx, store)
	require.NoError(t, err)
	assert.Greater(t, r.BytesSaved, 0)
	assert.Contains(t, r.Output, "[diff_noise: lockfile hunks dropped")
	assert.Contains(t, r.Output, `foo = "2"`)
	stored, ok := store.Get(r.CacheKey)
	assert.True(t, ok)
	assert.Equal(t, diff, string(stored))
}

func TestDiffNoiseApplyDropsWhitespaceOnlyHunk(t *testing.T) {
	var body []string
	for i := 0; i < 40; i++ {
		body = append(body, fmt.Sprintf("-line %d   ", i), fmt.Sprintf("+line %d", i))
	}
	diff := buildNoiseDiff([]diffFile{{path: "src/main.rs", body: body}})
	store := ccr.NewInMemoryStore()
	ctx := &pipeline.CompressionContext{}
	r, err := diffNoise().Apply(diff, ctx, store)
	require.NoError(t, err)
	assert.Contains(t, r.Output, "[diff_noise: whitespace-only hunks dropped")
	assert.Greater(t, r.BytesSaved, 0)
}

func TestDiffNoiseApplySkippedWhenNoDroppableHunks(t *testing.T) {
	var body []string
	for i := 0; i < 40; i++ {
		body = append(body, fmt.Sprintf("-old line %d", i), fmt.Sprintf("+new line %d", i))
	}
	diff := buildNoiseDiff([]diffFile{{path: "src/main.rs", body: body}})
	store := ccr.NewInMemoryStore()
	ctx := &pipeline.CompressionContext{}
	_, err := diffNoise().Apply(diff, ctx, store)
	require.Error(t, err)
	te, ok := err.(pipeline.TransformError)
	require.True(t, ok)
	assert.Equal(t, pipeline.ErrorSkipped, te.Kind)
	assert.Equal(t, 0, store.Len())
}

func TestDiffNoiseLockfilePathMatchIsSegmentAware(t *testing.T) {
	o := diffNoise()
	assert.True(t, o.isLockfile("Cargo.lock"))
	assert.True(t, o.isLockfile("crates/foo/Cargo.lock"))
	assert.False(t, o.isLockfile("MyCargo.lock"))
	assert.False(t, o.isLockfile("FakeCargo.lockfile"))
}

func TestDiffNoiseHandlesDiffWithLeadingCommitMessage(t *testing.T) {
	var lock []string
	for i := 0; i < 40; i++ {
		lock = append(lock, fmt.Sprintf("-old%d", i), fmt.Sprintf("+new%d", i))
	}
	diff := "From abc123 Mon Sep 17 2025\nSubject: bump deps\n\ncommit body line\n\n" +
		buildNoiseDiff([]diffFile{{path: "yarn.lock", body: lock}})
	store := ccr.NewInMemoryStore()
	ctx := &pipeline.CompressionContext{}
	r, err := diffNoise().Apply(diff, ctx, store)
	require.NoError(t, err)
	assert.Contains(t, r.Output, "Subject: bump deps")
	assert.Contains(t, r.Output, "[diff_noise: lockfile hunks dropped")
}

func TestDiffNoiseEmptyInputIsSafe(t *testing.T) {
	assert.Equal(t, float32(0.0), diffNoise().EstimateBloat(""))
	store := ccr.NewInMemoryStore()
	ctx := &pipeline.CompressionContext{}
	_, err := diffNoise().Apply("", ctx, store)
	require.Error(t, err)
	te, ok := err.(pipeline.TransformError)
	require.True(t, ok)
	assert.Equal(t, pipeline.ErrorSkipped, te.Kind)
}

func TestDiffNoiseConfidence(t *testing.T) {
	assert.Equal(t, float32(0.9), diffNoise().Confidence())
}
