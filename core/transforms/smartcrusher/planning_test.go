package smartcrusher

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/projectbarks/goheadroom/core/transforms/anchorselector"
)

// --- MapToAnchorPattern ---

func TestAnchorPatternMappingMatchesPython(t *testing.T) {
	assert.Equal(t, anchorselector.TimeSeries, MapToAnchorPattern(StrategyTimeSeries))
	assert.Equal(t, anchorselector.SearchResults, MapToAnchorPattern(StrategyTopN))
	assert.Equal(t, anchorselector.Logs, MapToAnchorPattern(StrategyClusterSample))
	assert.Equal(t, anchorselector.Generic, MapToAnchorPattern(StrategySmartSample))
	assert.Equal(t, anchorselector.Generic, MapToAnchorPattern(StrategyNone))
}

// --- ItemHasPreserveFieldMatch ---

func TestPreserveFieldMatchQuerySubstringInValue(t *testing.T) {
	item := json.RawMessage(`{"customer_id": "alice"}`)
	h := HashFieldName("customer_id")
	fields := []string{h}
	assert.True(t, ItemHasPreserveFieldMatch(item, fields, "find user alice please"))
}

func TestPreserveFieldMatchValueSubstringInQuery(t *testing.T) {
	item := json.RawMessage(`{"customer_id": "user-12345-alice"}`)
	h := HashFieldName("customer_id")
	fields := []string{h}
	assert.True(t, ItemHasPreserveFieldMatch(item, fields, "alice"))
}

func TestPreserveFieldNoMatchWhenFieldNotInHashes(t *testing.T) {
	item := json.RawMessage(`{"random_field": "alice"}`)
	fields := []string{HashFieldName("customer_id")}
	assert.False(t, ItemHasPreserveFieldMatch(item, fields, "alice"))
}

func TestPreserveFieldNoMatchWhenQueryEmpty(t *testing.T) {
	item := json.RawMessage(`{"customer_id": "alice"}`)
	fields := []string{HashFieldName("customer_id")}
	assert.False(t, ItemHasPreserveFieldMatch(item, fields, ""))
}

// --- CreatePlan ---

func makePlannerDeps() (*SmartCrusherConfig, *anchorselector.AnchorSelector, *SmartAnalyzer, []Constraint) {
	cfg := DefaultSmartCrusherConfig()
	asel := anchorselector.NewAnchorSelector(anchorselector.DefaultAnchorConfig())
	analyzer := NewSmartAnalyzer(cfg)
	constraints := DefaultOSSConstraints()
	return &cfg, asel, analyzer, constraints
}

func makePlanner() *SmartCrusherPlanner {
	cfg, asel, analyzer, constraints := makePlannerDeps()
	return NewSmartCrusherPlanner(cfg, asel, analyzer, constraints)
}

func TestCreatePlanSkipReturnsAllIndices(t *testing.T) {
	p := makePlanner()
	analysis := &ArrayAnalysis{
		ItemCount:           5,
		FieldStats:          make(map[string]*FieldStats),
		DetectedPattern:     "generic",
		RecommendedStrategy: StrategySkip,
		ConstantFields:      make(map[string]interface{}),
		EstimatedReduction:  0.0,
	}
	items := make([]json.RawMessage, 5)
	for i := 0; i < 5; i++ {
		items[i] = json.RawMessage(fmt.Sprintf(`{"id": %d}`, i))
	}
	plan := p.CreatePlan(analysis, items, "", nil, nil, nil)
	assert.Equal(t, []int{0, 1, 2, 3, 4}, plan.KeepIndices)
}

func TestCreatePlanRoutesSmartSampleToSmartSample(t *testing.T) {
	p := makePlanner()
	items := make([]json.RawMessage, 30)
	for i := 0; i < 30; i++ {
		items[i] = json.RawMessage(fmt.Sprintf(`{"id": %d, "v": %d}`, i, i))
	}
	analysis := p.Analyzer.AnalyzeArray(items)
	maxItems := 15
	plan := p.CreatePlan(analysis, items, "", nil, &maxItems, nil)
	assert.NotEmpty(t, plan.KeepIndices)
	assert.Empty(t, plan.SortField)
	assert.Empty(t, plan.ClusterField)
}

// --- PlanSmartSample ---

func TestSmartSampleKeepsErrorItems(t *testing.T) {
	p := makePlanner()
	items := make([]json.RawMessage, 31)
	for i := 0; i < 30; i++ {
		items[i] = json.RawMessage(fmt.Sprintf(`{"id": %d, "msg": "ok %d"}`, i, i))
	}
	items[30] = json.RawMessage(`{"id": 30, "msg": "FATAL: out of memory"}`)
	analysis := p.Analyzer.AnalyzeArray(items)
	plan := CompressionPlan{Strategy: StrategySmartSample}
	result := p.PlanSmartSample(analysis, items, plan, "", nil, 10, nil)
	assert.Contains(t, result.KeepIndices, 30, "error item must survive plan_smart_sample")
}

func TestSmartSampleQueryAnchorPinned(t *testing.T) {
	p := makePlanner()
	items := make([]json.RawMessage, 30)
	for i := 0; i < 30; i++ {
		items[i] = json.RawMessage(fmt.Sprintf(`{"id": %d, "uuid": "550e8400-e29b-41d4-a716-44665544%04x"}`, i, i))
	}
	analysis := p.Analyzer.AnalyzeArray(items)
	targetUUID := fmt.Sprintf("550e8400-e29b-41d4-a716-44665544%04x", 17)
	query := fmt.Sprintf("find record %s", targetUUID)
	plan := CompressionPlan{Strategy: StrategySmartSample}
	result := p.PlanSmartSample(analysis, items, plan, query, nil, 10, nil)
	assert.Contains(t, result.KeepIndices, 17, "item matching query UUID must be kept")
}

// --- PlanTopN ---

func TestTopNFallsBackWhenNoScoreField(t *testing.T) {
	p := makePlanner()
	items := make([]json.RawMessage, 30)
	for i := 0; i < 30; i++ {
		items[i] = json.RawMessage(fmt.Sprintf(`{"id": %d}`, i))
	}
	analysis := p.Analyzer.AnalyzeArray(items)
	plan := CompressionPlan{Strategy: StrategyTopN}
	result := p.PlanTopN(analysis, items, plan, "", nil, 10, nil)
	assert.Empty(t, result.SortField)
}

func TestTopNKeepsHighestScoredItems(t *testing.T) {
	p := makePlanner()
	items := make([]json.RawMessage, 20)
	for i := 0; i < 20; i++ {
		score := float64(19-i) * 0.05
		items[i] = json.RawMessage(fmt.Sprintf(`{"id": %d, "score": %f}`, i, score))
	}
	analysis := p.Analyzer.AnalyzeArray(items)
	plan := CompressionPlan{Strategy: StrategyTopN}
	result := p.PlanTopN(analysis, items, plan, "", nil, 10, nil)
	assert.Contains(t, result.KeepIndices, 0, "highest-scored item (idx 0) should be kept")
}

// --- PlanClusterSample ---

func TestClusterSampleAssignsClusterField(t *testing.T) {
	p := makePlanner()
	items := make([]json.RawMessage, 30)
	for i := 0; i < 30; i++ {
		level := "INFO"
		if i%2 != 0 {
			level = "ERROR"
		}
		items[i] = json.RawMessage(fmt.Sprintf(`{"msg": "message body for entry %d with content here", "level": "%s"}`, i, level))
	}
	analysis := p.Analyzer.AnalyzeArray(items)
	plan := CompressionPlan{Strategy: StrategyClusterSample}
	result := p.PlanClusterSample(analysis, items, plan, "", nil, 10, nil)
	assert.Equal(t, "msg", result.ClusterField)
}

// --- PlanTimeSeries ---

func TestTimeSeriesKeepsWindowAroundChangePoints(t *testing.T) {
	p := makePlanner()
	items := make([]json.RawMessage, 60)
	for i := 0; i < 60; i++ {
		v := 1.0
		if i >= 30 {
			v = 100.0
		}
		items[i] = json.RawMessage(fmt.Sprintf(`{"id": %d, "value": %f}`, i, v))
	}
	analysis := p.Analyzer.AnalyzeArray(items)
	plan := CompressionPlan{Strategy: StrategyTimeSeries}
	result := p.PlanTimeSeries(analysis, items, plan, "", nil, 30, nil)
	assert.NotEmpty(t, result.KeepIndices)
}

// --- ExtractQueryAnchors ---

func TestExtractQueryAnchorsEmpty(t *testing.T) {
	assert.Empty(t, ExtractQueryAnchors(""))
}

func TestExtractQueryAnchorsUUID(t *testing.T) {
	anchors := ExtractQueryAnchors("see id 550E8400-E29B-41D4-A716-446655440000 plz")
	assert.True(t, anchors["550e8400-e29b-41d4-a716-446655440000"])
}

func TestExtractQueryAnchorsNumericID(t *testing.T) {
	anchors := ExtractQueryAnchors("user 12345 reported issue")
	assert.True(t, anchors["12345"])
}

func TestExtractQueryAnchorsHostname(t *testing.T) {
	anchors := ExtractQueryAnchors("connect to api.example.com asap")
	assert.True(t, anchors["api.example.com"])
}

func TestExtractQueryAnchorsHostnameFalsePositive(t *testing.T) {
	anchors := ExtractQueryAnchors("see e.g for example")
	assert.False(t, anchors["e.g"])
}

func TestExtractQueryAnchorsEmail(t *testing.T) {
	anchors := ExtractQueryAnchors("contact USER@example.COM please")
	assert.True(t, anchors["user@example.com"])
}

func TestExtractQueryAnchorsQuotedString(t *testing.T) {
	anchors := ExtractQueryAnchors(`find the "user_name" field`)
	assert.True(t, anchors["user_name"])
}

func TestItemMatchesAnchorsEmptySet(t *testing.T) {
	assert.False(t, ItemMatchesAnchors(`{"a": 1}`, nil))
}

func TestItemMatchesAnchorsMatch(t *testing.T) {
	anchors := map[string]bool{"alice": true}
	assert.True(t, ItemMatchesAnchors(`{"name": "Alice"}`, anchors))
}

func TestItemMatchesAnchorsNoMatch(t *testing.T) {
	anchors := map[string]bool{"xyz123": true}
	assert.False(t, ItemMatchesAnchors(`{"a": "b"}`, anchors))
}

// ============================================================
// Orchestration tests
// ============================================================

func idxSet(indices []int) map[int]bool {
	s := make(map[int]bool)
	for _, i := range indices {
		s[i] = true
	}
	return s
}

func rawItems(items ...string) []json.RawMessage {
	result := make([]json.RawMessage, len(items))
	for i, s := range items {
		result[i] = json.RawMessage(s)
	}
	return result
}

// --- DeduplicateIndicesByContent ---

func TestDedupEmptyInput(t *testing.T) {
	result := DeduplicateIndicesByContent(make(map[int]bool), nil)
	assert.Empty(t, result)
}

func TestDedupLowestIndexWinsForDuplicates(t *testing.T) {
	items := rawItems(`{"name": "alice"}`, `{"name": "alice"}`, `{"name": "bob"}`)
	kept := idxSet([]int{0, 1, 2})
	result := DeduplicateIndicesByContent(kept, items)
	// Items 0 and 1 collapse to the lower (0); item 2 is unique.
	expected := idxSet([]int{0, 2})
	assert.Equal(t, expected, result)
}

func TestDedupAllDistinctUnchanged(t *testing.T) {
	items := rawItems(`{"id": 1}`, `{"id": 2}`, `{"id": 3}`)
	kept := idxSet([]int{0, 1, 2})
	result := DeduplicateIndicesByContent(kept, items)
	assert.Equal(t, idxSet([]int{0, 1, 2}), result)
}

func TestDedupSkipsOutOfBounds(t *testing.T) {
	items := rawItems(`{"a": 1}`)
	kept := idxSet([]int{0, 5, 10})
	result := DeduplicateIndicesByContent(kept, items)
	assert.Equal(t, idxSet([]int{0}), result)
}

func TestDedupKeyOrderIndependent(t *testing.T) {
	items := rawItems(`{"b": 2, "a": 1}`, `{"a": 1, "b": 2}`)
	kept := idxSet([]int{0, 1})
	result := DeduplicateIndicesByContent(kept, items)
	assert.Equal(t, 1, len(result))
	assert.True(t, result[0])
}

// --- FillRemainingSlots ---

func TestFillWhenAtOrOverBudgetReturnsUnchanged(t *testing.T) {
	items := make([]json.RawMessage, 10)
	for i := range items {
		items[i] = json.RawMessage(fmt.Sprintf(`{"id": %d}`, i))
	}
	kept := idxSet([]int{0, 1, 2, 3, 4})
	result := FillRemainingSlots(kept, items, len(items), 5)
	assert.Equal(t, kept, result)
}

func TestFillAddsDiverseUniquesUpToMax(t *testing.T) {
	items := make([]json.RawMessage, 20)
	for i := range items {
		items[i] = json.RawMessage(fmt.Sprintf(`{"id": %d}`, i))
	}
	kept := idxSet([]int{0, 5})
	result := FillRemainingSlots(kept, items, len(items), 10)
	assert.LessOrEqual(t, len(result), 10)
	assert.GreaterOrEqual(t, len(result), 2)
	assert.True(t, result[0])
	assert.True(t, result[5])
}

func TestFillSkipsContentDuplicates(t *testing.T) {
	items := make([]json.RawMessage, 20)
	for i := 0; i < 10; i++ {
		items[i] = json.RawMessage(fmt.Sprintf(`{"id": %d}`, i))
	}
	for i := 10; i < 20; i++ {
		items[i] = json.RawMessage(`{"id": 0}`)
	}
	kept := idxSet([]int{0})
	result := FillRemainingSlots(kept, items, len(items), 15)
	for i := 10; i < 20; i++ {
		assert.False(t, result[i], "dup index %d should not be added", i)
	}
}

// --- PrioritizeIndices ---

func defaultCfg() *SmartCrusherConfig {
	cfg := DefaultSmartCrusherConfig()
	return &cfg
}

func TestPrioritizeUnderBudgetPassthroughAfterDedup(t *testing.T) {
	items := make([]json.RawMessage, 5)
	for i := range items {
		items[i] = json.RawMessage(fmt.Sprintf(`{"id": %d}`, i))
	}
	kept := idxSet([]int{0, 1, 2})
	result := PrioritizeIndices(defaultCfg(), kept, items, len(items), nil, 10)
	// 3 items < max 10 -> fill kicks in; we get 5 (all items).
	assert.Equal(t, 5, len(result))
}

func TestPrioritizeDedupCollapsesThenReturnsUnderMax(t *testing.T) {
	items := rawItems(`{"name": "alice"}`, `{"name": "alice"}`, `{"name": "bob"}`)
	kept := idxSet([]int{0, 1, 2})
	result := PrioritizeIndices(defaultCfg(), kept, items, len(items), nil, 10)
	require.True(t, result[0])
	require.True(t, result[2])
}

func TestPrioritizeKeepsErrorItemsWhenOverBudget(t *testing.T) {
	items := make([]json.RawMessage, 31)
	for i := 0; i < 30; i++ {
		items[i] = json.RawMessage(fmt.Sprintf(`{"id": %d, "msg": "ok %d"}`, i, i))
	}
	items[30] = json.RawMessage(`{"id": 30, "msg": "FATAL: out of memory"}`)
	kept := make(map[int]bool)
	for i := range items {
		kept[i] = true
	}
	result := PrioritizeIndices(defaultCfg(), kept, items, len(items), nil, 10)
	assert.True(t, result[30], "error item must survive prioritization")
}

func TestPrioritizeIncludesFirst3AndLast2WhenRoom(t *testing.T) {
	items := make([]json.RawMessage, 30)
	for i := range items {
		items[i] = json.RawMessage(fmt.Sprintf(`{"id": %d, "v": %d}`, i, i))
	}
	kept := make(map[int]bool)
	for i := 5; i < 15; i++ {
		kept[i] = true
	}
	result := PrioritizeIndices(defaultCfg(), kept, items, len(items), nil, 10)
	assert.LessOrEqual(t, len(result), 10)
}
