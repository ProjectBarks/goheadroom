package comparators

import (
	"encoding/json"

	"github.com/projectbarks/goheadroom/core/parity"
	"github.com/projectbarks/goheadroom/core/transforms/smartcrusher"
)

type SmartCrusher struct{}

func (SmartCrusher) Name() string { return "smart_crusher" }

func (SmartCrusher) Run(input, config json.RawMessage) (interface{}, error) {
	var inp struct {
		Content string  `json:"content"`
		Query   string  `json:"query"`
		Bias    float64 `json:"bias"`
	}
	if err := json.Unmarshal(input, &inp); err != nil {
		return nil, err
	}

	cfg := smartcrusher.DefaultSmartCrusherConfig()
	if config != nil {
		var cfgMap map[string]interface{}
		if err := json.Unmarshal(config, &cfgMap); err == nil {
			applySmartCrusherConfig(&cfg, cfgMap)
		}
	}

	crusher := smartcrusher.NewSmartCrusherBuilder(cfg).WithDefaultOSSSetup().Build()
	crusher.Compaction = nil

	result := crusher.Crush(inp.Content, inp.Query, inp.Bias)

	out := map[string]interface{}{
		"compressed":   result.Compressed,
		"original":     result.Original,
		"was_modified": result.WasModified,
		"strategy":     result.Strategy,
	}
	return out, nil
}

func applySmartCrusherConfig(cfg *smartcrusher.SmartCrusherConfig, m map[string]interface{}) {
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
	if v, ok := m["lossless_min_savings_ratio"].(float64); ok {
		cfg.LosslessMinSavingsRatio = v
	}
	if v, ok := m["enable_ccr_marker"].(bool); ok {
		cfg.EnableCCRMarker = v
	}
}

var _ parity.Comparator = SmartCrusher{}
