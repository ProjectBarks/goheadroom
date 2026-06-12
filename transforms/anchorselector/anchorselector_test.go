package anchorselector

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mustParseJSON parses JSON preserving number types as json.Number.
func mustParseJSON(s string) interface{} {
	var v interface{}
	d := json.NewDecoder(strings.NewReader(s))
	d.UseNumber()
	if err := d.Decode(&v); err != nil {
		panic(err)
	}
	return v
}

func defaultSelector() *AnchorSelector {
	return NewAnchorSelector(DefaultAnchorConfig())
}

// ============================================================================
// PythonJsonDumps tests
// ============================================================================

func TestJsonDumpsBasicSortKeys(t *testing.T) {
	v := mustParseJSON(`{"b": 1, "a": 2}`)
	assert.Equal(t, `{"a": 2, "b": 1}`, PythonJsonDumps(v))
}

func TestJsonDumpsArraySpacing(t *testing.T) {
	v := mustParseJSON(`[1, 2, 3]`)
	assert.Equal(t, "[1, 2, 3]", PythonJsonDumps(v))
}

func TestJsonDumpsNestedSortKeys(t *testing.T) {
	v := mustParseJSON(`{"outer": {"z": 1, "a": 2}}`)
	assert.Equal(t, `{"outer": {"a": 2, "z": 1}}`, PythonJsonDumps(v))
}

func TestJsonDumpsStringEscapes(t *testing.T) {
	v := mustParseJSON(`{"k": "hello\nworld"}`)
	assert.Equal(t, "{\"k\": \"hello\\nworld\"}", PythonJsonDumps(v))
}

func TestJsonDumpsNonASCIIEscaped(t *testing.T) {
	// Python ensure_ascii=True: e-acute (U+00E9) -> é
	// "café" is "café" with precomposed e-acute
	v := map[string]interface{}{"k": "café"}
	result := PythonJsonDumps(v)
	assert.Equal(t, "{\"k\": \"caf\\u00e9\"}", result)
}

func TestJsonDumpsEmojiSurrogatePair(t *testing.T) {
	// U+1F600 (grinning face) -> surrogate pair 😀
	v := map[string]interface{}{"k": "\U0001F600"}
	result := PythonJsonDumps(v)
	assert.Equal(t, "{\"k\": \"\\ud83d\\ude00\"}", result)
}

func TestJsonDumpsNullBool(t *testing.T) {
	v := mustParseJSON(`{"a": null, "b": true, "c": false}`)
	assert.Equal(t, `{"a": null, "b": true, "c": false}`, PythonJsonDumps(v))
}

func TestJsonDumpsNumbers(t *testing.T) {
	v := mustParseJSON(`{"int": 42, "neg": -7, "zero": 0}`)
	result := PythonJsonDumps(v)
	assert.Contains(t, result, `"int": 42`)
	assert.Contains(t, result, `"neg": -7`)
	assert.Contains(t, result, `"zero": 0`)
}

func TestJsonDumpsEmptyObject(t *testing.T) {
	v := mustParseJSON(`{}`)
	assert.Equal(t, `{}`, PythonJsonDumps(v))
}

func TestJsonDumpsEmptyArray(t *testing.T) {
	v := mustParseJSON(`[]`)
	assert.Equal(t, `[]`, PythonJsonDumps(v))
}

func TestJsonDumpsFloatNumber(t *testing.T) {
	v := mustParseJSON(`{"k": 3.14}`)
	result := PythonJsonDumps(v)
	assert.Equal(t, `{"k": 3.14}`, result)
}

func TestJsonDumpsDeeplySorted(t *testing.T) {
	v := mustParseJSON(`{"z": {"b": [1, {"d": 2, "c": 3}], "a": 4}, "y": 5}`)
	result := PythonJsonDumps(v)
	assert.Equal(t, `{"y": 5, "z": {"a": 4, "b": [1, {"c": 3, "d": 2}]}}`, result)
}

func TestJsonDumpsBackslashAndQuote(t *testing.T) {
	v := map[string]interface{}{"k": "a\\b\"c"}
	result := PythonJsonDumps(v)
	assert.Equal(t, "{\"k\": \"a\\\\b\\\"c\"}", result)
}

func TestJsonDumpsControlChars(t *testing.T) {
	v := map[string]interface{}{"k": "a\x01b"}
	result := PythonJsonDumps(v)
	assert.Equal(t, "{\"k\": \"a\\u0001b\"}", result)
}

func TestJsonDumpsMultipleNonASCII(t *testing.T) {
	// Japanese: each kana char gets \uXXXX escaped
	v := map[string]interface{}{"k": "こん"}
	result := PythonJsonDumps(v)
	assert.Equal(t, "{\"k\": \"\\u3053\\u3093\"}", result)
}

// ============================================================================
// ComputeItemHash tests
// ============================================================================

func TestComputeItemHashDeterministic(t *testing.T) {
	h1 := ComputeItemHash(mustParseJSON(`{"a": 1, "b": 2}`))
	h2 := ComputeItemHash(mustParseJSON(`{"b": 2, "a": 1}`))
	assert.Equal(t, h1, h2, "hash is independent of key insertion order")
}

func TestComputeItemHashDifferentItems(t *testing.T) {
	h1 := ComputeItemHash(mustParseJSON(`{"a": 1}`))
	h2 := ComputeItemHash(mustParseJSON(`{"a": 2}`))
	assert.NotEqual(t, h1, h2)
}

func TestComputeItemHashMatchesPythonBasic(t *testing.T) {
	// Python: hashlib.md5(json.dumps({"a":1,"b":2}, sort_keys=True).encode()).hexdigest()[:16]
	// = "8aacdb17187e6acf"
	h := ComputeItemHash(mustParseJSON(`{"a": 1, "b": 2}`))
	assert.Equal(t, "8aacdb17187e6acf", h)
}

func TestComputeItemHashMatchesPythonUnicode(t *testing.T) {
	// Python: hashlib.md5(json.dumps({"k":"café"}, sort_keys=True).encode())
	//   .hexdigest()[:16] = "6761da28ed7eb489"
	h := ComputeItemHash(map[string]interface{}{"k": "café"})
	assert.Equal(t, "6761da28ed7eb489", h)
}

func TestComputeItemHash16HexChars(t *testing.T) {
	h := ComputeItemHash(mustParseJSON(`{"x": 1}`))
	assert.Equal(t, 16, len(h))
	for _, c := range h {
		assert.True(t, (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f'),
			"char %c must be hex", c)
	}
}

// ============================================================================
// AnchorWeights tests
// ============================================================================

func TestWeightsNormalizeSumsToOne(t *testing.T) {
	w := AnchorWeights{Front: 1.0, Middle: 1.0, Back: 2.0}.Normalize()
	assert.InDelta(t, 0.25, w.Front, 1e-9)
	assert.InDelta(t, 0.25, w.Middle, 1e-9)
	assert.InDelta(t, 0.5, w.Back, 1e-9)
	assert.InDelta(t, 1.0, w.Front+w.Middle+w.Back, 1e-9)
}

func TestWeightsNormalizeZeroReturnsDefault(t *testing.T) {
	w := AnchorWeights{Front: 0, Middle: 0, Back: 0}.Normalize()
	d := DefaultAnchorWeights()
	assert.InDelta(t, d.Front, w.Front, 1e-9)
	assert.InDelta(t, d.Middle, w.Middle, 1e-9)
	assert.InDelta(t, d.Back, w.Back, 1e-9)
}

// ============================================================================
// DataPattern tests
// ============================================================================

func TestPatternFromStringKnown(t *testing.T) {
	assert.Equal(t, SearchResults, DataPatternFromString("search_results"))
	assert.Equal(t, Logs, DataPatternFromString("LOGS"))
	assert.Equal(t, TimeSeries, DataPatternFromString("time_series"))
	assert.Equal(t, Generic, DataPatternFromString("generic"))
}

func TestPatternFromStringUnknown(t *testing.T) {
	assert.Equal(t, Generic, DataPatternFromString("unknown"))
	assert.Equal(t, Generic, DataPatternFromString(""))
}

// ============================================================================
// Budget tests
// ============================================================================

func TestBudgetZeroNoCompression(t *testing.T) {
	s := defaultSelector()
	assert.Equal(t, 0, s.CalculateAnchorBudget(10, 10))
	assert.Equal(t, 0, s.CalculateAnchorBudget(5, 10))
}

func TestBudgetRespectsMinFloor(t *testing.T) {
	s := defaultSelector()
	// max_items=8 * 0.25 = 2 -> max(min=3, 2) = 3
	assert.Equal(t, 3, s.CalculateAnchorBudget(100, 8))
}

func TestBudgetRespectsMaxCeiling(t *testing.T) {
	s := defaultSelector()
	// max_items=100 * 0.25 = 25 -> min(max=12, 25) = 12
	assert.Equal(t, 12, s.CalculateAnchorBudget(1000, 100))
}

func TestBudgetCappedByArraySize(t *testing.T) {
	cfg := DefaultAnchorConfig()
	cfg.MinAnchorSlots = 50
	s := NewAnchorSelector(cfg)
	assert.Equal(t, 10, s.CalculateAnchorBudget(10, 5))
}

// ============================================================================
// Strategy tests
// ============================================================================

func TestStrategyMappings(t *testing.T) {
	s := defaultSelector()
	assert.Equal(t, FrontHeavy, s.StrategyForPattern(SearchResults))
	assert.Equal(t, BackHeavy, s.StrategyForPattern(Logs))
	assert.Equal(t, Balanced, s.StrategyForPattern(TimeSeries))
	assert.Equal(t, Distributed, s.StrategyForPattern(Generic))
}

// ============================================================================
// AdjustWeightsForQuery tests
// ============================================================================

func TestAdjustWeightsRecencyShiftsBack(t *testing.T) {
	s := defaultSelector()
	base := AnchorWeights{Front: 0.5, Middle: 0.1, Back: 0.4}
	q := "show me the latest errors"
	adjusted := s.AdjustWeightsForQuery(base, &q)
	assert.Greater(t, adjusted.Back, base.Back, "recency 'latest' should boost back")
	assert.Less(t, adjusted.Front, base.Front)
}

func TestAdjustWeightsHistoricalShiftsFront(t *testing.T) {
	s := defaultSelector()
	base := AnchorWeights{Front: 0.5, Middle: 0.1, Back: 0.4}
	q := "what was the original cause"
	adjusted := s.AdjustWeightsForQuery(base, &q)
	assert.Greater(t, adjusted.Front, base.Front)
	assert.Less(t, adjusted.Back, base.Back)
}

func TestAdjustWeightsBothKeywordsNoChange(t *testing.T) {
	s := defaultSelector()
	base := AnchorWeights{Front: 0.5, Middle: 0.1, Back: 0.4}
	q := "first and latest"
	adjusted := s.AdjustWeightsForQuery(base, &q)
	assert.InDelta(t, base.Front, adjusted.Front, 1e-9)
	assert.InDelta(t, base.Back, adjusted.Back, 1e-9)
}

func TestAdjustWeightsNoQueryNoChange(t *testing.T) {
	s := defaultSelector()
	base := DefaultAnchorWeights()
	assert.Equal(t, base, s.AdjustWeightsForQuery(base, nil))
	empty := ""
	assert.Equal(t, base, s.AdjustWeightsForQuery(base, &empty))
}

// ============================================================================
// SelectAnchors tests
// ============================================================================

func TestSelectAnchorsEmptyReturnsEmpty(t *testing.T) {
	result := defaultSelector().SelectAnchors(nil, 10, Generic, nil)
	assert.Empty(t, result)
}

func TestSelectAnchorsNoCompressionReturnsAll(t *testing.T) {
	items := make([]interface{}, 5)
	for i := range items {
		items[i] = map[string]interface{}{"id": float64(i)}
	}
	anchors := defaultSelector().SelectAnchors(items, 10, Generic, nil)
	assert.Equal(t, 5, len(anchors))
	for i := 0; i < 5; i++ {
		assert.Contains(t, anchors, i)
	}
}

func TestSelectAnchorsDistributedCoversBothEnds(t *testing.T) {
	items := make([]interface{}, 100)
	for i := range items {
		items[i] = map[string]interface{}{"id": float64(i)}
	}
	anchors := defaultSelector().SelectAnchors(items, 10, Generic, nil)
	require.NotEmpty(t, anchors)

	minIdx := anchors[0]
	maxIdx := anchors[len(anchors)-1]
	assert.Less(t, minIdx, 20, "first anchor should be near start")
	assert.Greater(t, maxIdx, 80, "last anchor should be near end")
}

func TestSelectAnchorsDedupIdentical(t *testing.T) {
	items := make([]interface{}, 100)
	for i := range items {
		items[i] = map[string]interface{}{"value": "same"}
	}
	anchors := defaultSelector().SelectAnchors(items, 10, Generic, nil)
	assert.LessOrEqual(t, len(anchors), 3,
		"duplicate items should dedup: got %d anchors", len(anchors))
}

func TestSelectAnchorsNonObjectItemsNotDeduped(t *testing.T) {
	items := make([]interface{}, 50)
	for i := range items {
		items[i] = "same string"
	}
	anchors := defaultSelector().SelectAnchors(items, 10, Generic, nil)
	assert.Greater(t, len(anchors), 1)
}

// ============================================================================
// Information density tests
// ============================================================================

func TestInfoScoreZeroForNonDict(t *testing.T) {
	item := "string value"
	all := []interface{}{map[string]interface{}{"a": float64(1)}}
	assert.Equal(t, 0.0, CalculateInformationScore(item, all))
}

func TestInfoScoreInRange(t *testing.T) {
	item := map[string]interface{}{"a": float64(1), "b": float64(2)}
	all := make([]interface{}, 10)
	for i := range all {
		all[i] = map[string]interface{}{"a": float64(i)}
	}
	s := CalculateInformationScore(item, all)
	assert.GreaterOrEqual(t, s, 0.0)
	assert.LessOrEqual(t, s, 1.0)
}

func TestInfoScoreHigherForUniqueValues(t *testing.T) {
	all := make([]interface{}, 11)
	for i := 0; i < 10; i++ {
		all[i] = map[string]interface{}{"status": "ok"}
	}
	all[10] = map[string]interface{}{"status": "error"}

	commonScore := CalculateInformationScore(all[0], all)
	rareScore := CalculateInformationScore(all[10], all)
	assert.Greater(t, rareScore, commonScore,
		"rare-value item should score higher: rare=%f, common=%f", rareScore, commonScore)
}

func TestInfoScoreEmptyItems(t *testing.T) {
	item := map[string]interface{}{"a": float64(1)}
	assert.Equal(t, 0.0, CalculateInformationScore(item, nil))
	assert.Equal(t, 0.0, CalculateInformationScore(item, []interface{}{}))
}

func TestInfoScoreSingleItem(t *testing.T) {
	item := map[string]interface{}{"a": float64(1)}
	all := []interface{}{item}
	// With < 2 items, sub-scores return 0.5 each -> 0.5*0.4 + 0.5*0.3 + 0.5*0.3 = 0.5
	assert.InDelta(t, 0.5, CalculateInformationScore(item, all), 1e-9)
}
