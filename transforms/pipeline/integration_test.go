package pipeline_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uber/goheadroom/ccr"
	"github.com/uber/goheadroom/transforms/contentdetector"
	"github.com/uber/goheadroom/transforms/pipeline"
	"github.com/uber/goheadroom/transforms/pipeline/offloads"
	"github.com/uber/goheadroom/transforms/pipeline/reformats"
)

func defaultConfig() *pipeline.PipelineConfig {
	cfg, _ := pipeline.DefaultPipelineConfig()
	return cfg
}

func TestEndToEndLogOffloadCompressesRepetitiveLog(t *testing.T) {
	cfg := defaultConfig()
	p := pipeline.NewPipelineBuilder().
		WithOffload(offloads.NewLogOffload(cfg.Bloat.Log)).
		WithConfig(cfg).
		Build()
	s := ccr.NewInMemoryStore()
	log := strings.Repeat("INFO: heartbeat\n", 200)
	ctx := &pipeline.CompressionContext{}
	r := p.Run(log, contentdetector.BuildOutput, ctx, s)
	assert.Equal(t, []string{"log_offload"}, r.StepsApplied)
	assert.Equal(t, 1, len(r.CacheKeys))
	stored, ok := s.Get(r.CacheKeys[0])
	assert.True(t, ok)
	assert.Equal(t, log, string(stored))
	assert.Greater(t, r.BytesSaved, 0)
}

func TestEndToEndDiffOffloadCompressesContextHeavyDiff(t *testing.T) {
	cfg := defaultConfig()
	p := pipeline.NewPipelineBuilder().
		WithOffload(offloads.NewDiffOffload(cfg.Bloat.Diff)).
		WithConfig(cfg).
		Build()
	s := ccr.NewInMemoryStore()
	var diff strings.Builder
	diff.WriteString("diff --git a/x.txt b/x.txt\n--- a/x.txt\n+++ b/x.txt\n@@ -1,105 +1,105 @@\n")
	for i := 0; i < 100; i++ {
		diff.WriteString(" context line\n")
	}
	for c := 0; c < 5; c++ {
		diff.WriteString(fmt.Sprintf("-old %d\n", c))
		diff.WriteString(fmt.Sprintf("+new %d\n", c))
	}
	ctx := &pipeline.CompressionContext{}
	r := p.Run(diff.String(), contentdetector.GitDiff, ctx, s)
	assert.Equal(t, []string{"diff_offload"}, r.StepsApplied)
	assert.Equal(t, 1, len(r.CacheKeys))
	_, ok := s.Get(r.CacheKeys[0])
	assert.True(t, ok)
}

func TestEndToEndSearchOffloadCompressesClusteredMatches(t *testing.T) {
	cfg := defaultConfig()
	p := pipeline.NewPipelineBuilder().
		WithOffload(offloads.NewSearchOffload(cfg.Bloat.Search)).
		WithConfig(cfg).
		Build()
	s := ccr.NewInMemoryStore()
	var lines []string
	for i := 0; i < 100; i++ {
		lines = append(lines, fmt.Sprintf("utils.py:%d:def fn_%d", i+1, i))
	}
	input := strings.Join(lines, "\n")
	ctx := &pipeline.CompressionContext{}
	r := p.Run(input, contentdetector.SearchResults, ctx, s)
	assert.Equal(t, []string{"search_offload"}, r.StepsApplied)
	assert.Equal(t, 1, len(r.CacheKeys))
	stored, ok := s.Get(r.CacheKeys[0])
	assert.True(t, ok)
	assert.Equal(t, input, string(stored))
}

func TestEndToEndDiffNoiseDropsLockfileThenDiffOffloadHandlesRest(t *testing.T) {
	cfg := defaultConfig()
	p := pipeline.NewPipelineBuilder().
		WithOffload(offloads.NewDiffNoise(cfg.Offload.DiffNoise)).
		WithOffload(offloads.NewDiffOffload(cfg.Bloat.Diff)).
		WithConfig(cfg).
		Build()
	s := ccr.NewInMemoryStore()

	var diff strings.Builder
	diff.WriteString("diff --git a/Cargo.lock b/Cargo.lock\n")
	diff.WriteString("--- a/Cargo.lock\n+++ b/Cargo.lock\n@@ -1,400 +1,400 @@\n")
	for i := 0; i < 200; i++ {
		diff.WriteString(fmt.Sprintf("-old%d\n", i))
		diff.WriteString(fmt.Sprintf("+new%d\n", i))
	}
	diff.WriteString("diff --git a/src/main.rs b/src/main.rs\n")
	diff.WriteString("--- a/src/main.rs\n+++ b/src/main.rs\n@@ -1,3 +1,3 @@\n")
	diff.WriteString("-let x = 1;\n")
	diff.WriteString("+let x = 2;\n")

	ctx := &pipeline.CompressionContext{}
	r := p.Run(diff.String(), contentdetector.GitDiff, ctx, s)
	assert.Greater(t, r.BytesSaved, 0)
	assert.Contains(t, r.StepsApplied, "diff_noise")
	assert.Contains(t, r.Output, "[diff_noise: lockfile hunks dropped")
	assert.Contains(t, r.Output, "let x = 2;")
	assert.NotEmpty(t, r.CacheKeys)
}

func TestEndToEndJsonMinifierThenJsonOffloadOnTabularArray(t *testing.T) {
	cfg := defaultConfig()
	p := pipeline.NewPipelineBuilder().
		WithReformat(reformats.NewJsonMinifier()).
		WithOffload(offloads.NewJsonOffload(cfg.Offload.Json)).
		WithConfig(cfg).
		Build()
	s := ccr.NewInMemoryStore()

	// Pretty-printed 200-row array with low uniqueness (SmartCrusher will compress)
	var input strings.Builder
	input.WriteString("[\n")
	for i := 0; i < 200; i++ {
		if i > 0 {
			input.WriteString(",\n")
		}
		input.WriteString(fmt.Sprintf(`  {"id": %d, "status": "ok", "category": "type-a", "message": "heartbeat received"}`, i))
	}
	input.WriteString("\n]")

	ctx := &pipeline.CompressionContext{}
	r := p.Run(input.String(), contentdetector.JsonArray, ctx, s)
	assert.Greater(t, r.BytesSaved, 0)
	// JsonMinifier should have run (it strips whitespace)
	assert.Contains(t, r.StepsApplied, "json_minifier")
}

func TestEndToEndLogTemplateCollapsesRepetitiveLog(t *testing.T) {
	cfg := defaultConfig()
	p := pipeline.NewPipelineBuilder().
		WithReformat(reformats.NewLogTemplate(cfg.Reformat.LogTemplate)).
		WithConfig(cfg).
		Build()
	s := ccr.NewInMemoryStore()
	var log strings.Builder
	for i := 0; i < 100; i++ {
		log.WriteString(fmt.Sprintf("[2025-01-15 12:00:%02d] INFO Connecting to db-%d on port 5432\n", i%60, i%8))
	}
	ctx := &pipeline.CompressionContext{}
	r := p.Run(log.String(), contentdetector.BuildOutput, ctx, s)
	assert.Equal(t, []string{"log_template"}, r.StepsApplied)
	assert.Empty(t, r.CacheKeys, "reformat should not produce keys")
	assert.Greater(t, r.BytesSaved, 0)
	assert.Contains(t, r.Output, "[Template T1:")
}

func TestEndToEndLogTemplateThenLogOffload(t *testing.T) {
	cfg := defaultConfig()
	p := pipeline.NewPipelineBuilder().
		WithReformat(reformats.NewLogTemplate(cfg.Reformat.LogTemplate)).
		WithOffload(offloads.NewLogOffload(cfg.Bloat.Log)).
		WithConfig(cfg).
		Build()
	s := ccr.NewInMemoryStore()
	var log strings.Builder
	for i := 0; i < 200; i++ {
		log.WriteString(fmt.Sprintf("2025-01-15T12:34:%02d INFO worker-%d processing job %d\n", i%60, i, 100+i))
	}
	ctx := &pipeline.CompressionContext{}
	r := p.Run(log.String(), contentdetector.BuildOutput, ctx, s)
	assert.Greater(t, r.BytesSaved, 0)
	assert.Contains(t, r.StepsApplied, "log_template")
	assert.Less(t, len(r.Output), len(log.String()))
}

func TestFullPipelineAllTransformsRegistered(t *testing.T) {
	cfg := defaultConfig()
	p := pipeline.NewPipelineBuilder().
		WithReformat(reformats.NewJsonMinifier()).
		WithReformat(reformats.NewLogTemplate(cfg.Reformat.LogTemplate)).
		WithOffload(offloads.NewDiffOffload(cfg.Bloat.Diff)).
		WithOffload(offloads.NewDiffNoise(cfg.Offload.DiffNoise)).
		WithOffload(offloads.NewJsonOffload(cfg.Offload.Json)).
		WithOffload(offloads.NewLogOffload(cfg.Bloat.Log)).
		WithOffload(offloads.NewSearchOffload(cfg.Bloat.Search)).
		Build()
	require.NotNil(t, p)
}
