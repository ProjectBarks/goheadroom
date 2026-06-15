package smartcrusher

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
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
	// Known strategy divergences: Go's csv_schema path fires for dict-array inputs
	// where Python/Rust chose smart_sample or skip strategies. These are real parity
	// gaps to be fixed in the SmartCrusher strategy selection logic.
	knownDivergent := map[string]bool{
		"dict_array_100_sequential_b91d1fff5ba3.json": true,
		"dict_array_30_b0ba1d26fd03.json":             true,
		"dict_array_30_bias_high_dc3b36e60560.json":   true,
		"dict_array_30_bias_low_a850512901bd.json":     true,
		"duplicate_dicts_40_40a0670dec17.json":         true,
		"nested_object_with_array_b66d43299ea1.json":   true,
		"time_series_50_25cd28df5a50.json":             true,
		"unicode_dict_array_4820ed6c0c9a.json":         true,
	}
	_ = knownDivergent // retained for documentation
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

			// Build crusher without compaction: fixtures were recorded from
			// pre-compaction Rust, so the lossless csv_schema path must not
			// fire or it overrides the expected smart_sample strategy.
			crusher := NewSmartCrusherBuilder(cfg).WithDefaultOSSSetup().Build()
			crusher.Compaction = nil

			// Run Crush.
			result := crusher.Crush(fixture.Input.Content, fixture.Input.Query, fixture.Input.Bias)

			// Strip CCR markers from expected output: Rust/Python fixtures
			// inject {"_ccr_dropped":"<<ccr:...>>"} markers that Go handles
			// at the pipeline layer, not in SmartCrusher itself.
			expectedCompressed := stripCCRMarkers(fixture.Output.Compressed)

			// Normalize JSON float formatting: Rust serializes 0.0/3.0 while
			// Go's encoding/json drops the trailing .0 for whole numbers.
			expectedCompressed = normalizeJSONFloats(expectedCompressed)

			// Check key properties.
			assert.Equal(t, fixture.Output.WasModified, result.WasModified,
				"was_modified mismatch for %s", fixture.Label)

			assert.Equal(t, fixture.Output.Strategy, result.Strategy,
				"strategy mismatch for %s", fixture.Label)
			assert.Equal(t, expectedCompressed, result.Compressed,
				"compressed output mismatch for %s", fixture.Label)

			// Check that original is preserved.
			assert.Equal(t, fixture.Output.Original, result.Original,
				"original mismatch for %s", fixture.Label)
		})
	}

	t.Logf("Ran %d parity fixtures", fixtureCount)
	assert.Greater(t, fixtureCount, 0, "no fixtures found")
}

var ccrMarkerRe = regexp.MustCompile(`,?\{"_ccr_dropped":"<<ccr:[^>]+>>"\}`)

func stripCCRMarkers(s string) string {
	return ccrMarkerRe.ReplaceAllString(s, "")
}

var floatDotZeroRe = regexp.MustCompile(`(\d)\.0([,}\]\s])`)

func normalizeJSONFloats(s string) string {
	return floatDotZeroRe.ReplaceAllString(s, "${1}${2}")
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
