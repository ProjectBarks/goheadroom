package pipeline

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultConfigLoadsWithoutError(t *testing.T) {
	cfg, err := DefaultPipelineConfig()
	require.NoError(t, err)
	assert.NotNil(t, cfg)
}

func TestDefaultConfigMatchesDocumentedThresholds(t *testing.T) {
	cfg, err := DefaultPipelineConfig()
	require.NoError(t, err)
	assert.Equal(t, 0.5, cfg.Pipeline.ReformatTargetRatio)
	assert.Equal(t, float32(0.5), cfg.Pipeline.BloatThreshold)
	assert.Equal(t, 0.85, cfg.Pipeline.OffloadFallbackRatio)
	assert.Equal(t, 50, cfg.Bloat.Log.MinLines)
	assert.Equal(t, 100, cfg.Bloat.Log.SampleSize)
	assert.Equal(t, 50, cfg.Bloat.Diff.MinLines)
	assert.Equal(t, 10, cfg.Bloat.Search.MinMatches)
}

func TestBloatLogWeightsSumToAtMostOne(t *testing.T) {
	cfg, err := DefaultPipelineConfig()
	require.NoError(t, err)
	total := cfg.Bloat.Log.UniquenessWeight + cfg.Bloat.Log.PriorityDilutionWeight
	assert.LessOrEqual(t, total, float32(1.0001))
}

func TestFromTomlStrOverridesDefaults(t *testing.T) {
	toml := `
[pipeline]
reformat_target_ratio = 0.3
bloat_threshold = 0.7
offload_fallback_ratio = 0.9

[bloat.log]
min_lines = 25
sample_size = 50
high_priority_threshold = 0.6
uniqueness_weight = 0.4
priority_dilution_weight = 0.6

[bloat.diff]
min_lines = 30
normal_context_ratio = 0.7

[bloat.search]
min_matches = 5
cluster_threshold = 20.0

[reformat.log_template]
min_lines = 10
min_run = 5
similarity_threshold = 0.8
min_constant_tokens = 3

[offload.json]
min_array_rows = 3
saturation_rows = 25

[offload.diff_noise]
min_lines = 20
lockfile_suffixes = ["custom.lock"]
drop_whitespace_only_hunks = false
`
	cfg, err := PipelineConfigFromString(toml)
	require.NoError(t, err)
	assert.Equal(t, 0.3, cfg.Pipeline.ReformatTargetRatio)
	assert.Equal(t, 25, cfg.Bloat.Log.MinLines)
	assert.Equal(t, float32(20.0), cfg.Bloat.Search.ClusterThreshold)
	assert.Equal(t, 5, cfg.Reformat.LogTemplate.MinRun)
	assert.Equal(t, []string{"custom.lock"}, cfg.Offload.DiffNoise.LockfileSuffixes)
}

func TestDefaultsCarryReformatAndOffloadSections(t *testing.T) {
	cfg, err := DefaultPipelineConfig()
	require.NoError(t, err)
	assert.Equal(t, 20, cfg.Reformat.LogTemplate.MinLines)
	assert.Equal(t, 3, cfg.Reformat.LogTemplate.MinRun)
	assert.Equal(t, 5, cfg.Offload.Json.MinArrayRows)
	assert.Equal(t, 50, cfg.Offload.Json.SaturationRows)
	assert.NotEmpty(t, cfg.Offload.DiffNoise.LockfileSuffixes)
	assert.Contains(t, cfg.Offload.DiffNoise.LockfileSuffixes, "Cargo.lock")
}

func TestMalformedTomlReturnsError(t *testing.T) {
	_, err := PipelineConfigFromString("this is not toml = [unterminated")
	assert.Error(t, err)
}

func TestPartialTomlUsesZeroValues(t *testing.T) {
	// Go TOML decoder fills missing sections with zero values.
	toml := `
[pipeline]
reformat_target_ratio = 0.5
bloat_threshold = 0.5
offload_fallback_ratio = 0.85
`
	cfg, err := PipelineConfigFromString(toml)
	require.NoError(t, err)
	// Missing sections should be zero-valued.
	assert.Equal(t, 0, cfg.Bloat.Log.MinLines)
	assert.Equal(t, 0, cfg.Bloat.Diff.MinLines)
}

func TestConfigFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.toml")
	// Write a valid config
	data, _ := os.ReadFile("pipeline.toml")
	require.NoError(t, os.WriteFile(path, data, 0644))

	cfg, err := PipelineConfigFromFile(path)
	require.NoError(t, err)
	assert.NotNil(t, cfg)
	assert.Equal(t, 0.5, cfg.Pipeline.ReformatTargetRatio)
}

func TestConfigFromNonexistentFile(t *testing.T) {
	_, err := PipelineConfigFromFile("/nonexistent/path/pipeline.toml")
	assert.Error(t, err)
}
