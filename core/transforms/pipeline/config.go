package pipeline

import (
	_ "embed"
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

//go:embed pipeline.toml
var embeddedConfig string

// PipelineConfig is the top-level pipeline configuration.
type PipelineConfig struct {
	Pipeline OrchestratorConfig `toml:"pipeline"`
	Bloat    BloatConfigs       `toml:"bloat"`
	Reformat ReformatConfigs    `toml:"reformat"`
	Offload  OffloadConfigs     `toml:"offload"`
}

// OrchestratorConfig holds orchestrator-level knobs.
type OrchestratorConfig struct {
	ReformatTargetRatio  float64 `toml:"reformat_target_ratio"`
	BloatThreshold       float32 `toml:"bloat_threshold"`
	OffloadFallbackRatio float64 `toml:"offload_fallback_ratio"`
}

// BloatConfigs holds per-domain bloat-estimator knobs.
type BloatConfigs struct {
	Log    LogBloatConfig    `toml:"log"`
	Diff   DiffBloatConfig   `toml:"diff"`
	Search SearchBloatConfig `toml:"search"`
}

// LogBloatConfig tunes the log-domain bloat estimator.
type LogBloatConfig struct {
	MinLines               int     `toml:"min_lines"`
	SampleSize             int     `toml:"sample_size"`
	HighPriorityThreshold  float32 `toml:"high_priority_threshold"`
	UniquenessWeight       float32 `toml:"uniqueness_weight"`
	PriorityDilutionWeight float32 `toml:"priority_dilution_weight"`
}

// DiffBloatConfig tunes the diff-domain bloat estimator.
type DiffBloatConfig struct {
	MinLines           int     `toml:"min_lines"`
	NormalContextRatio float64 `toml:"normal_context_ratio"`
}

// SearchBloatConfig tunes the search-domain bloat estimator.
type SearchBloatConfig struct {
	MinMatches       int     `toml:"min_matches"`
	ClusterThreshold float32 `toml:"cluster_threshold"`
}

// ReformatConfigs holds per-reformat configuration.
type ReformatConfigs struct {
	LogTemplate LogTemplateConfig `toml:"log_template"`
}

// LogTemplateConfig tunes the LogTemplate reformat miner.
type LogTemplateConfig struct {
	MinLines           int     `toml:"min_lines"`
	MinRun             int     `toml:"min_run"`
	SimilarityThreshold float32 `toml:"similarity_threshold"`
	MinConstantTokens  int     `toml:"min_constant_tokens"`
}

// OffloadConfigs holds per-offload configuration.
type OffloadConfigs struct {
	Json          JsonOffloadConfig          `toml:"json"`
	DiffNoise     DiffNoiseConfig            `toml:"diff_noise"`
	JsonStructure JsonStructureOffloadConfig `toml:"json_structure"`
	Code          CodeOffloadConfig          `toml:"code"`
}

// JsonOffloadConfig tunes the JsonOffload (SmartCrusher wrapper).
type JsonOffloadConfig struct {
	MinArrayRows   int `toml:"min_array_rows"`
	SaturationRows int `toml:"saturation_rows"`
}

// DiffNoiseConfig tunes the DiffNoise offload.
type DiffNoiseConfig struct {
	MinLines               int      `toml:"min_lines"`
	LockfileSuffixes       []string `toml:"lockfile_suffixes"`
	DropWhitespaceOnlyHunks bool    `toml:"drop_whitespace_only_hunks"`
}

// JsonStructureOffloadConfig tunes the JsonStructureOffload.
type JsonStructureOffloadConfig struct {
	MinInputLen int `toml:"min_input_len"`
}

// CodeOffloadConfig tunes the CodeOffload.
type CodeOffloadConfig struct {
	MinInputLen int `toml:"min_input_len"`
}

// DefaultPipelineConfig loads the embedded default configuration.
func DefaultPipelineConfig() (*PipelineConfig, error) {
	return PipelineConfigFromString(embeddedConfig)
}

// PipelineConfigFromString parses a TOML configuration string.
func PipelineConfigFromString(s string) (*PipelineConfig, error) {
	var cfg PipelineConfig
	if _, err := toml.Decode(s, &cfg); err != nil {
		return nil, fmt.Errorf("invalid pipeline config TOML: %w", err)
	}
	return &cfg, nil
}

// PipelineConfigFromFile reads and parses a TOML configuration file.
func PipelineConfigFromFile(path string) (*PipelineConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("could not read pipeline config file: %w", err)
	}
	return PipelineConfigFromString(string(data))
}
