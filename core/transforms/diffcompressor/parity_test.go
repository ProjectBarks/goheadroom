package diffcompressor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type parityFixture struct {
	Config      json.RawMessage `json:"config"`
	Input       string          `json:"input"`
	Output      parityOutput    `json:"output"`
	InputSHA256 string          `json:"input_sha256"`
	Transform   string          `json:"transform"`
}

type parityOutput struct {
	Compressed          string  `json:"compressed"`
	CompressedLineCount int     `json:"compressed_line_count"`
	OriginalLineCount   int     `json:"original_line_count"`
	FilesAffected       int     `json:"files_affected"`
	Additions           int     `json:"additions"`
	Deletions           int     `json:"deletions"`
	HunksKept           int     `json:"hunks_kept"`
	HunksRemoved        int     `json:"hunks_removed"`
	CacheKey            *string `json:"cache_key"`
}

type parityConfig struct {
	AlwaysKeepAdditions *bool `json:"always_keep_additions,omitempty"`
	AlwaysKeepDeletions *bool `json:"always_keep_deletions,omitempty"`
	MaxContextLines     *int  `json:"max_context_lines,omitempty"`
	MaxFiles            *int  `json:"max_files,omitempty"`
	MaxHunksPerFile     *int  `json:"max_hunks_per_file,omitempty"`
	EnableCCR           *bool `json:"enable_ccr,omitempty"`
	MinLinesForCCR      *int  `json:"min_lines_for_ccr,omitempty"`
}

func configFromParity(raw json.RawMessage) DiffCompressorConfig {
	cfg := DefaultConfig()
	var pc parityConfig
	if err := json.Unmarshal(raw, &pc); err != nil {
		return cfg
	}
	if pc.AlwaysKeepAdditions != nil {
		cfg.AlwaysKeepAdditions = *pc.AlwaysKeepAdditions
	}
	if pc.AlwaysKeepDeletions != nil {
		cfg.AlwaysKeepDeletions = *pc.AlwaysKeepDeletions
	}
	if pc.MaxContextLines != nil {
		cfg.MaxContextLines = *pc.MaxContextLines
	}
	if pc.MaxFiles != nil {
		cfg.MaxFiles = *pc.MaxFiles
	}
	if pc.MaxHunksPerFile != nil {
		cfg.MaxHunksPerFile = *pc.MaxHunksPerFile
	}
	if pc.EnableCCR != nil {
		cfg.EnableCCR = *pc.EnableCCR
	}
	if pc.MinLinesForCCR != nil {
		cfg.MinLinesForCCR = *pc.MinLinesForCCR
	}
	return cfg
}

func TestDiffCompressorParity(t *testing.T) {
	fixtureDir := filepath.Join("..", "..", "testdata", "parity", "diff_compressor")
	entries, err := os.ReadDir(fixtureDir)
	require.NoError(t, err, "failed to read fixture dir: %s", fixtureDir)
	require.NotEmpty(t, entries, "no fixtures found")

	count := 0
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		count++
		t.Run(entry.Name(), func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(fixtureDir, entry.Name()))
			require.NoError(t, err)

			var fixture parityFixture
			require.NoError(t, json.Unmarshal(data, &fixture))
			assert.Equal(t, "diff_compressor", fixture.Transform)

			cfg := configFromParity(fixture.Config)
			c := New(cfg)
			result := c.Compress(fixture.Input, "")

			assert.Equal(t, fixture.Output.Compressed, result.Compressed,
				"compressed output mismatch for %s", entry.Name())
			assert.Equal(t, fixture.Output.CompressedLineCount, result.CompressedLineCount,
				"compressed_line_count mismatch for %s", entry.Name())
			assert.Equal(t, fixture.Output.OriginalLineCount, result.OriginalLineCount,
				"original_line_count mismatch for %s", entry.Name())
			assert.Equal(t, fixture.Output.FilesAffected, result.FilesAffected,
				"files_affected mismatch for %s", entry.Name())
			assert.Equal(t, fixture.Output.Additions, result.Additions,
				"additions mismatch for %s", entry.Name())
			assert.Equal(t, fixture.Output.Deletions, result.Deletions,
				"deletions mismatch for %s", entry.Name())
			assert.Equal(t, fixture.Output.HunksKept, result.HunksKept,
				"hunks_kept mismatch for %s", entry.Name())
			assert.Equal(t, fixture.Output.HunksRemoved, result.HunksRemoved,
				"hunks_removed mismatch for %s", entry.Name())
			if fixture.Output.CacheKey == nil {
				assert.Nil(t, result.CacheKey, "cache_key should be nil for %s", entry.Name())
			} else {
				require.NotNil(t, result.CacheKey, "cache_key should not be nil for %s", entry.Name())
				assert.Equal(t, *fixture.Output.CacheKey, *result.CacheKey,
					"cache_key mismatch for %s", entry.Name())
			}
		})
	}
	assert.Equal(t, 27, count, "expected 27 fixture files")
}
