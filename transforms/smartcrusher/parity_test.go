package smartcrusher

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ParityFixture represents a single parity test case from Rust/Python.
type ParityFixture struct {
	Label  string          `json:"label"`
	Config json.RawMessage `json:"config"`
	Input  struct {
		Content string  `json:"content"`
		Query   string  `json:"query"`
		Bias    float64 `json:"bias"`
	} `json:"input"`
	Output struct {
		Compressed  string `json:"compressed"`
		Original    string `json:"original"`
		Strategy    string `json:"strategy"`
		WasModified bool   `json:"was_modified"`
	} `json:"output"`
}

func TestParityFixtures(t *testing.T) {
	fixtureDir := filepath.Join("testdata", "fixtures")
	entries, err := os.ReadDir(fixtureDir)
	require.NoError(t, err, "failed to read fixture dir")

	fixtureCount := 0
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		fixtureCount++
		t.Run(entry.Name(), func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(fixtureDir, entry.Name()))
			require.NoError(t, err, "failed to read fixture")

			var fixture ParityFixture
			require.NoError(t, json.Unmarshal(data, &fixture), "failed to parse fixture")

			// Parse config.
			cfg := DefaultSmartCrusherConfig()
			if fixture.Config != nil {
				var cfgMap map[string]interface{}
				if err := json.Unmarshal(fixture.Config, &cfgMap); err == nil {
					applyConfigMap(&cfg, cfgMap)
				}
			}

			// Build crusher.
			crusher := NewSmartCrusherBuilder(cfg).WithDefaultOSSSetup().Build()

			// Run Crush.
			result := crusher.Crush(fixture.Input.Content, fixture.Input.Query, fixture.Input.Bias)

			// Check key properties.
			assert.Equal(t, fixture.Output.WasModified, result.WasModified,
				"was_modified mismatch for %s", fixture.Label)

			// For passthrough fixtures, check exact output match.
			if fixture.Output.Strategy == "passthrough" {
				assert.Equal(t, fixture.Output.Strategy, result.Strategy,
					"strategy mismatch for %s", fixture.Label)
				assert.Equal(t, fixture.Output.Compressed, result.Compressed,
					"compressed output mismatch for %s", fixture.Label)
			}

			// Check that original is preserved.
			assert.Equal(t, fixture.Output.Original, result.Original,
				"original mismatch for %s", fixture.Label)
		})
	}

	t.Logf("Ran %d parity fixtures", fixtureCount)
	assert.Greater(t, fixtureCount, 0, "no fixtures found")
}

func applyConfigMap(cfg *SmartCrusherConfig, m map[string]interface{}) {
	if v, ok := m["enabled"].(bool); ok {
		cfg.Enabled = v
	}
	if v, ok := m["min_items_to_analyze"].(float64); ok {
		cfg.MinItemsToAnalyze = int(v)
	}
	if v, ok := m["min_tokens_to_crush"].(float64); ok {
		cfg.MinTokensToCrush = int(v)
	}
	if v, ok := m["variance_threshold"].(float64); ok {
		cfg.VarianceThreshold = v
	}
	if v, ok := m["uniqueness_threshold"].(float64); ok {
		cfg.UniquenessThreshold = v
	}
	if v, ok := m["similarity_threshold"].(float64); ok {
		cfg.SimilarityThreshold = v
	}
	if v, ok := m["max_items_after_crush"].(float64); ok {
		cfg.MaxItemsAfterCrush = int(v)
	}
	if v, ok := m["preserve_change_points"].(bool); ok {
		cfg.PreserveChangePoints = v
	}
	if v, ok := m["factor_out_constants"].(bool); ok {
		cfg.FactorOutConstants = v
	}
	if v, ok := m["include_summaries"].(bool); ok {
		cfg.IncludeSummaries = v
	}
	if v, ok := m["use_feedback_hints"].(bool); ok {
		cfg.UseFeedbackHints = v
	}
	if v, ok := m["toin_confidence_threshold"].(float64); ok {
		cfg.TOINConfidenceThreshold = v
	}
	if v, ok := m["dedup_identical_items"].(bool); ok {
		cfg.DedupIdenticalItems = v
	}
	if v, ok := m["first_fraction"].(float64); ok {
		cfg.FirstFraction = v
	}
	if v, ok := m["last_fraction"].(float64); ok {
		cfg.LastFraction = v
	}
}
