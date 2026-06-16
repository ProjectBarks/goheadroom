package smartcrusher

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/projectbarks/goheadroom/core/transforms/anchorselector"
)

// MapToAnchorPattern maps a compression strategy to its anchor data pattern.
// Port of Python _map_to_anchor_pattern (smart_crusher.py:1565-1579).
func MapToAnchorPattern(strategy CompressionStrategy) anchorselector.DataPattern {
	switch strategy {
	case StrategyTimeSeries:
		return anchorselector.TimeSeries
	case StrategyTopN:
		return anchorselector.SearchResults
	case StrategyClusterSample:
		return anchorselector.Logs
	default:
		return anchorselector.Generic
	}
}

// ItemHasPreserveFieldMatch checks if any of an item's preserve_field values
// matches the query. Port of Rust item_has_preserve_field_match.
func ItemHasPreserveFieldMatch(item json.RawMessage, preserveFieldHashes []string, queryContext string) bool {
	if queryContext == "" || len(preserveFieldHashes) == 0 {
		return false
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(item, &obj); err != nil {
		return false
	}
	queryLower := toLower(queryContext)

	for fieldName, rawVal := range obj {
		h := HashFieldName(fieldName)
		found := false
		for _, p := range preserveFieldHashes {
			if p == h {
				found = true
				break
			}
		}
		if !found {
			continue
		}
		// Skip null values.
		if string(rawVal) == "null" {
			continue
		}
		valStr := toLower(rawString(rawVal))
		if contains(valStr, queryLower) || contains(queryLower, valStr) {
			return true
		}
	}
	return false
}

// SmartCrusherPlanner is a stateless planner that computes compression plans.
type SmartCrusherPlanner struct {
	Config         *SmartCrusherConfig
	AnchorSelector *anchorselector.AnchorSelector
	Analyzer       *SmartAnalyzer
	Constraints    []Constraint
}

// NewSmartCrusherPlanner creates a new planner.
func NewSmartCrusherPlanner(
	config *SmartCrusherConfig,
	anchorSel *anchorselector.AnchorSelector,
	analyzer *SmartAnalyzer,
	constraints []Constraint,
) *SmartCrusherPlanner {
	return &SmartCrusherPlanner{
		Config:         config,
		AnchorSelector: anchorSel,
		Analyzer:       analyzer,
		Constraints:    constraints,
	}
}

// applyConstraints unions all constraint MustKeep results into keep.
func (p *SmartCrusherPlanner) applyConstraints(items []json.RawMessage, itemStrings []string, keep map[int]bool) {
	for _, c := range p.Constraints {
		for _, idx := range c.MustKeep(items, itemStrings) {
			keep[idx] = true
		}
	}
}

// CreatePlan dispatches to the strategy-specific planner.
// Port of Rust SmartCrusherPlanner::create_plan.
func (p *SmartCrusherPlanner) CreatePlan(
	analysis *ArrayAnalysis,
	items []json.RawMessage,
	queryContext string,
	preserveFields []string,
	effectiveMaxItems *int,
	itemStrings []string,
) CompressionPlan {
	maxItems := p.Config.MaxItemsAfterCrush
	if effectiveMaxItems != nil {
		maxItems = *effectiveMaxItems
	}

	plan := CompressionPlan{
		Strategy: analysis.RecommendedStrategy,
	}
	if p.Config.FactorOutConstants {
		plan.ConstantFields = analysis.ConstantFields
	}

	// Skip path: keep all items.
	if analysis.RecommendedStrategy == StrategySkip {
		plan.KeepIndices = make([]int, len(items))
		for i := range items {
			plan.KeepIndices[i] = i
		}
		return plan
	}

	switch analysis.RecommendedStrategy {
	case StrategyTimeSeries:
		return p.PlanTimeSeries(analysis, items, plan, queryContext, preserveFields, maxItems, itemStrings)
	case StrategyClusterSample:
		return p.PlanClusterSample(analysis, items, plan, queryContext, preserveFields, maxItems, itemStrings)
	case StrategyTopN:
		return p.PlanTopN(analysis, items, plan, queryContext, preserveFields, maxItems, itemStrings)
	default:
		return p.PlanSmartSample(analysis, items, plan, queryContext, preserveFields, maxItems, itemStrings)
	}
}

// PlanSmartSample plans SmartSample - the default/fallback strategy.
// Port of Rust plan_smart_sample.
func (p *SmartCrusherPlanner) PlanSmartSample(
	analysis *ArrayAnalysis,
	items []json.RawMessage,
	plan CompressionPlan,
	queryContext string,
	preserveFields []string,
	maxItems int,
	itemStrings []string,
) CompressionPlan {
	n := len(items)
	keep := make(map[int]bool)

	// 1. Dynamic anchors.
	anchorPattern := MapToAnchorPattern(StrategySmartSample)
	anchorItems := rawToInterfaceSlice(items)
	qPtr := queryOrNone(queryContext)
	for _, idx := range p.AnchorSelector.SelectAnchors(anchorItems, maxItems, anchorPattern, qPtr) {
		keep[idx] = true
	}

	// 2. Constraints (structural outliers + error keywords).
	p.applyConstraints(items, itemStrings, keep)

	// 3. Numeric anomalies (>variance_threshold sigma from per-field mean).
	for name, stats := range analysis.FieldStats {
		forEachAnomaly(name, stats, items, p.Config.VarianceThreshold, keep)
	}

	// 4. Items around change points (window +/-1).
	if p.Config.PreserveChangePoints {
		for _, stats := range analysis.FieldStats {
			for _, cp := range stats.ChangePoints {
				for offset := -1; offset <= 1; offset++ {
					idx := cp + offset
					if idx >= 0 && idx < n {
						keep[idx] = true
					}
				}
			}
		}
	}

	// 5/6. Query-anchor matches + relevance scores.
	p.applyQuerySignals(items, queryContext, itemStrings, keep)

	// TOIN preserve_fields.
	p.applyPreserveFieldMatches(items, queryContext, preserveFields, keep)

	finalKeep := PrioritizeIndices(p.Config, keep, items, n, analysis, maxItems)
	plan.KeepIndices = setToSortedSlice(finalKeep)
	return plan
}

// PlanTopN plans TopN - for ranked/scored data.
// Port of Rust plan_top_n.
func (p *SmartCrusherPlanner) PlanTopN(
	analysis *ArrayAnalysis,
	items []json.RawMessage,
	plan CompressionPlan,
	queryContext string,
	preserveFields []string,
	maxItems int,
	itemStrings []string,
) CompressionPlan {
	// Locate highest-confidence score field.
	var scoreField string
	maxConfidence := 0.0
	for _, stats := range analysis.FieldStats {
		isScore, confidence := DetectScoreFieldStatistically(stats, items)
		if isScore && confidence > maxConfidence {
			scoreField = stats.Name
			maxConfidence = confidence
		}
	}

	if scoreField == "" {
		return p.PlanSmartSample(analysis, items, plan, queryContext, preserveFields, maxItems, itemStrings)
	}

	plan.SortField = scoreField
	keep := make(map[int]bool)

	// 1. TOP-N by score.
	type scored struct {
		idx   int
		score float64
	}
	var scores []scored
	for i, raw := range items {
		var obj map[string]json.RawMessage
		s := 0.0
		if err := json.Unmarshal(raw, &obj); err == nil {
			if v, ok := obj[scoreField]; ok {
				var f float64
				if err := json.Unmarshal(v, &f); err == nil {
					s = f
				}
			}
		}
		scores = append(scores, scored{i, s})
	}
	sort.Slice(scores, func(i, j int) bool { return scores[i].score > scores[j].score })

	topCount := maxItems - 3
	if topCount < 0 {
		topCount = 0
	}
	for i := 0; i < topCount && i < len(scores); i++ {
		keep[scores[i].idx] = true
	}

	// 2. Constraints.
	p.applyConstraints(items, itemStrings, keep)

	// 3. Query-anchor matches.
	if queryContext != "" {
		anchors := ExtractQueryAnchors(queryContext)
		strs := ensureItemStrings(items, itemStrings)
		for i, s := range strs {
			if !keep[i] && ItemMatchesAnchors(s, anchors) {
				keep[i] = true
			}
		}
	}

	// 4. HIGH-CONFIDENCE relevance matches (additive only) - stubbed for now
	// since we don't have the RelevanceScorer integrated.

	p.applyPreserveFieldMatches(items, queryContext, preserveFields, keep)

	plan.KeepCount = len(keep)
	plan.KeepIndices = setToSortedSlice(keep)
	return plan
}

// PlanClusterSample plans ClusterSample - for log-style data.
// Port of Rust plan_cluster_sample.
func (p *SmartCrusherPlanner) PlanClusterSample(
	analysis *ArrayAnalysis,
	items []json.RawMessage,
	plan CompressionPlan,
	queryContext string,
	preserveFields []string,
	maxItems int,
	itemStrings []string,
) CompressionPlan {
	n := len(items)
	keep := make(map[int]bool)

	// 1. Anchors.
	anchorPattern := MapToAnchorPattern(StrategyClusterSample)
	anchorItems := rawToInterfaceSlice(items)
	qPtr := queryOrNone(queryContext)
	for _, idx := range p.AnchorSelector.SelectAnchors(anchorItems, maxItems, anchorPattern, qPtr) {
		keep[idx] = true
	}

	// 2. Constraints.
	p.applyConstraints(items, itemStrings, keep)

	// 3. Cluster by message-like field (highest unique_ratio > 0.3).
	var messageField string
	maxUniqueness := 0.0
	for name, stats := range analysis.FieldStats {
		if stats.FieldType == "string" && stats.UniqueRatio > maxUniqueness && stats.UniqueRatio > 0.3 {
			messageField = name
			maxUniqueness = stats.UniqueRatio
		}
	}

	if messageField != "" {
		plan.ClusterField = messageField
		// Group by md5(first 50 chars of message)[:8].
		clusters := make(map[string][]int)
		for i, raw := range items {
			var obj map[string]json.RawMessage
			msg := ""
			if err := json.Unmarshal(raw, &obj); err == nil {
				if v, ok := obj[messageField]; ok {
					var s string
					if err := json.Unmarshal(v, &s); err == nil {
						msg = s
					}
				}
			}
			truncated := msg
			if len(truncated) > 50 {
				truncated = truncated[:50]
			}
			digest := md5.Sum([]byte(truncated))
			hash := fmt.Sprintf("%x", digest)[:8]
			clusters[hash] = append(clusters[hash], i)
		}
		// Keep up to 2 representatives from each cluster.
		for _, indices := range clusters {
			for j := 0; j < 2 && j < len(indices); j++ {
				keep[indices[j]] = true
			}
		}
	}

	// 4/5. Query signals.
	p.applyQuerySignals(items, queryContext, itemStrings, keep)

	// TOIN preserve_fields.
	p.applyPreserveFieldMatches(items, queryContext, preserveFields, keep)

	finalKeep := PrioritizeIndices(p.Config, keep, items, n, analysis, maxItems)
	plan.KeepIndices = setToSortedSlice(finalKeep)
	return plan
}

// PlanTimeSeries plans TimeSeries compression.
// Port of Rust plan_time_series.
func (p *SmartCrusherPlanner) PlanTimeSeries(
	analysis *ArrayAnalysis,
	items []json.RawMessage,
	plan CompressionPlan,
	queryContext string,
	preserveFields []string,
	maxItems int,
	itemStrings []string,
) CompressionPlan {
	n := len(items)
	keep := make(map[int]bool)

	// 1. Anchors.
	anchorPattern := MapToAnchorPattern(StrategyTimeSeries)
	anchorItems := rawToInterfaceSlice(items)
	qPtr := queryOrNone(queryContext)
	for _, idx := range p.AnchorSelector.SelectAnchors(anchorItems, maxItems, anchorPattern, qPtr) {
		keep[idx] = true
	}

	// 2. Items around change points (window +/-2 - wider than smart_sample).
	for _, stats := range analysis.FieldStats {
		for _, cp := range stats.ChangePoints {
			for offset := -2; offset <= 2; offset++ {
				idx := cp + offset
				if idx >= 0 && idx < n {
					keep[idx] = true
				}
			}
		}
	}

	// 3. Constraints.
	p.applyConstraints(items, itemStrings, keep)

	// 4/5. Query signals.
	p.applyQuerySignals(items, queryContext, itemStrings, keep)

	// TOIN preserve_fields.
	p.applyPreserveFieldMatches(items, queryContext, preserveFields, keep)

	finalKeep := PrioritizeIndices(p.Config, keep, items, n, analysis, maxItems)
	plan.KeepIndices = setToSortedSlice(finalKeep)
	return plan
}

// applyQuerySignals applies query-anchor matches + relevance scoring.
func (p *SmartCrusherPlanner) applyQuerySignals(
	items []json.RawMessage,
	queryContext string,
	itemStrings []string,
	keep map[int]bool,
) {
	if queryContext == "" {
		return
	}

	// Deterministic anchor match.
	anchors := ExtractQueryAnchors(queryContext)
	strs := ensureItemStrings(items, itemStrings)
	for i, s := range strs {
		if ItemMatchesAnchors(s, anchors) {
			keep[i] = true
		}
	}

	// Probabilistic relevance scoring is stubbed for now.
	// The full integration with RelevanceScorer will come when
	// the crusher is wired up in Task 7.
}

// applyPreserveFieldMatches applies TOIN preserve_fields matches.
func (p *SmartCrusherPlanner) applyPreserveFieldMatches(
	items []json.RawMessage,
	queryContext string,
	preserveFields []string,
	keep map[int]bool,
) {
	if len(preserveFields) == 0 || queryContext == "" {
		return
	}
	for i, item := range items {
		if ItemHasPreserveFieldMatch(item, preserveFields, queryContext) {
			keep[i] = true
		}
	}
}

// forEachAnomaly adds numeric anomaly indices to keep.
func forEachAnomaly(fieldName string, stats *FieldStats, items []json.RawMessage, varianceThreshold float64, keep map[int]bool) {
	if stats.FieldType != "numeric" {
		return
	}
	if stats.MeanVal == nil || stats.Variance == nil {
		return
	}
	mean := *stats.MeanVal
	variance := *stats.Variance
	if variance <= 0 {
		return
	}
	std := sqrt(variance)
	if std <= 0 {
		return
	}
	threshold := varianceThreshold * std

	for i, raw := range items {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(raw, &obj); err != nil {
			continue
		}
		v, ok := obj[fieldName]
		if !ok {
			continue
		}
		var num float64
		if err := json.Unmarshal(v, &num); err != nil {
			continue
		}
		if !isFinite(num) {
			continue
		}
		if abs(num-mean) > threshold {
			keep[i] = true
		}
	}
}

// --- helpers ---

func queryOrNone(q string) *string {
	if q == "" {
		return nil
	}
	return &q
}

func rawToInterfaceSlice(items []json.RawMessage) []interface{} {
	result := make([]interface{}, len(items))
	for i, raw := range items {
		var v interface{}
		_ = json.Unmarshal(raw, &v)
		result[i] = v
	}
	return result
}

func ensureItemStrings(items []json.RawMessage, itemStrings []string) []string {
	if len(itemStrings) > 0 {
		return itemStrings
	}
	result := make([]string, len(items))
	for i, raw := range items {
		result[i] = string(raw)
	}
	return result
}

func setToSortedSlice(set map[int]bool) []int {
	result := make([]int, 0, len(set))
	for k := range set {
		result = append(result, k)
	}
	sort.Ints(result)
	return result
}

func rawString(raw json.RawMessage) string {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return string(raw)
}

func toLower(s string) string {
	return lower(s)
}

func lower(s string) string {
	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b[i] = c
	}
	return string(b)
}

func contains(s, sub string) bool {
	return len(sub) > 0 && len(s) >= len(sub) && findSubstring(s, sub)
}

func findSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
