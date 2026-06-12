package smartcrusher

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testCfg() *SmartCrusherConfig {
	cfg := DefaultSmartCrusherConfig()
	return &cfg
}

// ---------- ComputeKSplit ----------

func TestKSplitBelowThresholdReturnsN(t *testing.T) {
	items := []string{"a", "b", "c", "d", "e"}
	kt, kf, kl, ki := ComputeKSplit(items, testCfg(), 1.0)
	assert.Equal(t, 5, kt)
	assert.Equal(t, 2, kf)  // round(5 * 0.3) = round(1.5) = banker's 2
	assert.Equal(t, 1, kl)  // round(5 * 0.15) = round(0.75) = 1
	assert.Equal(t, 2, ki)  // 5 - 2 - 1 = 2
}

func TestBug4KSplitNoOvershootWhenKTotalIsOne(t *testing.T) {
	items := []string{"only"}
	kt, kf, kl, ki := ComputeKSplit(items, testCfg(), 1.0)
	assert.Equal(t, 1, kt)
	assert.LessOrEqual(t, kf+kl, kt, "BUG #4: k_first + k_last must not exceed k_total")
	assert.Equal(t, ki, kt-(kf+kl))
}

func TestBug4KSplitNoOvershootWhenKTotalIsTwo(t *testing.T) {
	items := []string{"a", "b"}
	kt, kf, kl, _ := ComputeKSplit(items, testCfg(), 1.0)
	assert.Equal(t, 2, kt)
	assert.LessOrEqual(t, kf+kl, kt)
	assert.Equal(t, 1, kf)
	assert.Equal(t, 1, kl)
}

func TestKSplitLowDiversityReturnsMinK(t *testing.T) {
	items := make([]string, 10)
	for i := range items {
		items[i] = "x"
	}
	kt, kf, kl, _ := ComputeKSplit(items, testCfg(), 1.0)
	assert.Equal(t, 3, kt, "low-diversity -> max(min_k, unique_count)=3")
	assert.Equal(t, 1, kf)
	assert.Equal(t, 1, kl)
}

// ---------- CrushStringArray ----------

func TestStringArrayPassthroughAtThreshold(t *testing.T) {
	items := []string{"a", "b", "c", "d", "e", "f", "g", "h"}
	out, strat := CrushStringArray(items, testCfg(), 1.0)
	assert.Equal(t, 8, len(out))
	assert.Equal(t, "string:passthrough", strat)
}

func TestStringArrayKeepsErrorStrings(t *testing.T) {
	items := make([]string, 30)
	for i := range items {
		items[i] = "ok"
	}
	items[15] = "FATAL: out of memory"
	out, strat := CrushStringArray(items, testCfg(), 1.0)
	found := false
	for _, s := range out {
		if s == "FATAL: out of memory" {
			found = true
			break
		}
	}
	assert.True(t, found, "error item at index 15 must survive")
	assert.Contains(t, strat, "errors=1")
}

func TestStringArrayKeepsFirstAndLast(t *testing.T) {
	items := make([]string, 30)
	for i := range items {
		items[i] = fmt.Sprintf("item_%d", i)
	}
	out, _ := CrushStringArray(items, testCfg(), 1.0)
	hasFirst := false
	hasLast := false
	for _, s := range out {
		if s == "item_0" {
			hasFirst = true
		}
		if s == "item_29" {
			hasLast = true
		}
	}
	assert.True(t, hasFirst, "first item should always be kept")
	assert.True(t, hasLast, "last item should always be kept")
}

func TestStringArrayDedupCountAppearsInStrategy(t *testing.T) {
	items := make([]string, 50)
	for i := range items {
		items[i] = "dup"
	}
	_, strat := CrushStringArray(items, testCfg(), 1.0)
	assert.Contains(t, strat, "dedup=")
}

// ---------- CrushNumberArray ----------

func TestNumberArrayPassthroughAtThreshold(t *testing.T) {
	items := make([]json.RawMessage, 8)
	for i := range items {
		items[i] = json.RawMessage(fmt.Sprintf("%d", i))
	}
	out, strat := CrushNumberArray(items, testCfg(), 1.0)
	assert.Equal(t, 8, len(out))
	assert.Equal(t, "number:passthrough", strat)
}

func TestNumberArrayNoFiniteReturnsPassthrough(t *testing.T) {
	items := make([]json.RawMessage, 15)
	for i := range items {
		items[i] = json.RawMessage("null")
	}
	out, strat := CrushNumberArray(items, testCfg(), 1.0)
	assert.Equal(t, len(items), len(out))
	assert.Equal(t, "number:no_finite", strat)
}

func TestNumberArrayKeepsOutliers(t *testing.T) {
	items := make([]json.RawMessage, 31)
	for i := 0; i < 30; i++ {
		items[i] = json.RawMessage("0")
	}
	items[30] = json.RawMessage("1000")
	out, strat := CrushNumberArray(items, testCfg(), 1.0)
	found := false
	for _, raw := range out {
		var f float64
		if json.Unmarshal(raw, &f) == nil && f == 1000 {
			found = true
			break
		}
	}
	assert.True(t, found)
	assert.Contains(t, strat, "outliers=")
}

func TestNumberArrayStrategyStringIncludesSummary(t *testing.T) {
	items := make([]json.RawMessage, 20)
	for i := range items {
		items[i] = json.RawMessage(fmt.Sprintf("%d", i+1))
	}
	_, strat := CrushNumberArray(items, testCfg(), 1.0)
	assert.True(t, strings.HasPrefix(strat, "number:adaptive("))
	assert.Contains(t, strat, "min=1")
	assert.Contains(t, strat, "max=20")
	assert.Contains(t, strat, "mean=")
	assert.Contains(t, strat, "median=")
	assert.Contains(t, strat, "p25=")
	assert.Contains(t, strat, "p75=")
}

// ---------- CrushObject ----------

func TestObjectPassthroughWhenFewKeys(t *testing.T) {
	obj := make(map[string]json.RawMessage)
	for i := 0; i < 5; i++ {
		obj[fmt.Sprintf("k%d", i)] = json.RawMessage(fmt.Sprintf("%d", i))
	}
	out, strat := CrushObject(obj, testCfg(), 1.0)
	assert.Equal(t, 5, len(out))
	assert.Equal(t, "object:passthrough", strat)
}

func TestObjectPassthroughWhenTotalTokensBelowMin(t *testing.T) {
	obj := make(map[string]json.RawMessage)
	for i := 0; i < 30; i++ {
		obj[fmt.Sprintf("k%d", i)] = json.RawMessage(fmt.Sprintf("%d", i))
	}
	_, strat := CrushObject(obj, testCfg(), 1.0)
	assert.Equal(t, "object:passthrough", strat)
}

func TestObjectCrushesWhenTokenBudgetExceeded(t *testing.T) {
	obj := make(map[string]json.RawMessage)
	for i := 0; i < 30; i++ {
		val := fmt.Sprintf(`"this is a relatively long value string for entry number %d with content"`, i)
		obj[fmt.Sprintf("k%02d", i)] = json.RawMessage(val)
	}
	out, strat := CrushObject(obj, testCfg(), 1.0)
	if strat == "object:passthrough" {
		assert.Equal(t, 30, len(out))
	} else {
		assert.True(t, strings.HasPrefix(strat, "object:adaptive("))
		assert.LessOrEqual(t, len(out), 30)
	}
}

func TestObjectKeepsSmallValues(t *testing.T) {
	obj := make(map[string]json.RawMessage)
	obj["tiny"] = json.RawMessage("1")
	for i := 0; i < 30; i++ {
		val := fmt.Sprintf(`"this is a long string with content for entry number %d that exceeds the small threshold"`, i)
		obj[fmt.Sprintf("big%02d", i)] = json.RawMessage(val)
	}
	out, _ := CrushObject(obj, testCfg(), 1.0)
	_, hasTiny := out["tiny"]
	assert.True(t, hasTiny, "tiny key (small value) must survive")
}

func TestObjectKeepsErrorKeywords(t *testing.T) {
	obj := make(map[string]json.RawMessage)
	longFatal := fmt.Sprintf(`"FATAL: %s"`, strings.Repeat("x", 200))
	obj["msg1"] = json.RawMessage(longFatal)
	for i := 0; i < 30; i++ {
		val := fmt.Sprintf(`"padding content for entry %d with text"`, i)
		obj[fmt.Sprintf("k%02d", i)] = json.RawMessage(val)
	}
	out, _ := CrushObject(obj, testCfg(), 1.0)
	_, hasMsg1 := out["msg1"]
	assert.True(t, hasMsg1, "key with error-keyword value must survive")
}

// ---------- BUG #1 percentile tests ----------

func TestBug1PercentileProperLinearInterpolation(t *testing.T) {
	items := make([]json.RawMessage, 14)
	for i := 0; i < 9; i++ {
		items[i] = json.RawMessage(fmt.Sprintf("%d", i+1))
	}
	for i := 9; i < 14; i++ {
		items[i] = json.RawMessage("null")
	}
	_, strat := CrushNumberArray(items, testCfg(), 1.0)
	assert.Contains(t, strat, "p25=3", "got: %s", strat)
	assert.Contains(t, strat, "p75=7", "got: %s", strat)
}

// ============================================================
// Core SmartCrusher tests
// ============================================================

func testCrusher() *SmartCrusher {
	cfg := DefaultSmartCrusherConfig()
	return NewSmartCrusherBuilder(cfg).WithDefaultOSSSetup().Build()
}

// ---------- ExecutePlan ----------

func TestExecutePlanEmptyIndicesReturnsEmpty(t *testing.T) {
	c := testCrusher()
	plan := &CompressionPlan{}
	items := make([]json.RawMessage, 5)
	for i := range items {
		items[i] = json.RawMessage(fmt.Sprintf(`{"id": %d}`, i))
	}
	result := c.ExecutePlan(plan, items)
	assert.Empty(t, result)
}

func TestExecutePlanReturnsSortedOrder(t *testing.T) {
	c := testCrusher()
	items := make([]json.RawMessage, 10)
	for i := range items {
		items[i] = json.RawMessage(fmt.Sprintf(`{"id": %d}`, i))
	}
	plan := &CompressionPlan{KeepIndices: []int{5, 2, 8, 0}}
	result := c.ExecutePlan(plan, items)
	assert.Equal(t, 4, len(result))
	// Should be in sorted index order: 0, 2, 5, 8.
	var id0, id1, id2, id3 map[string]interface{}
	json.Unmarshal(result[0], &id0)
	json.Unmarshal(result[1], &id1)
	json.Unmarshal(result[2], &id2)
	json.Unmarshal(result[3], &id3)
	assert.Equal(t, float64(0), id0["id"])
	assert.Equal(t, float64(2), id1["id"])
	assert.Equal(t, float64(5), id2["id"])
	assert.Equal(t, float64(8), id3["id"])
}

func TestExecutePlanSkipsOutOfBounds(t *testing.T) {
	c := testCrusher()
	items := make([]json.RawMessage, 3)
	for i := range items {
		items[i] = json.RawMessage(fmt.Sprintf(`{"id": %d}`, i))
	}
	plan := &CompressionPlan{KeepIndices: []int{0, 5, 2}}
	result := c.ExecutePlan(plan, items)
	assert.Equal(t, 2, len(result))
}

// ---------- CrushArray ----------

func TestCrushArrayPassthroughWhenBelowAdaptiveK(t *testing.T) {
	c := testCrusher()
	items := make([]json.RawMessage, 3)
	for i := range items {
		items[i] = json.RawMessage(fmt.Sprintf(`{"id": %d}`, i))
	}
	result := c.CrushArray(items, "", 1.0)
	assert.Equal(t, 3, len(result.Items))
	assert.Equal(t, "none:adaptive_at_limit", result.StrategyInfo)
}

func TestCrushArrayLowUniquenessCompresses(t *testing.T) {
	c := testCrusher()
	items := make([]json.RawMessage, 30)
	for i := range items {
		items[i] = json.RawMessage(`{"status": "ok"}`)
	}
	result := c.CrushArray(items, "", 1.0)
	assert.LessOrEqual(t, len(result.Items), 30)
}

func TestCrushArrayKeepsErrorItems(t *testing.T) {
	c := testCrusher()
	items := make([]json.RawMessage, 31)
	for i := 0; i < 30; i++ {
		items[i] = json.RawMessage(fmt.Sprintf(`{"id": %d, "status": "ok"}`, i))
	}
	items[30] = json.RawMessage(`{"id": 30, "status": "error", "msg": "FATAL"}`)
	result := c.CrushArray(items, "", 1.0)
	found := false
	for _, raw := range result.Items {
		var obj map[string]interface{}
		if json.Unmarshal(raw, &obj) == nil {
			if obj["status"] == "error" {
				found = true
				break
			}
		}
	}
	assert.True(t, found, "error item must survive crush_array")
}

// ---------- CrushMixedArray ----------

func TestCrushMixedPassthroughAtThreshold(t *testing.T) {
	c := testCrusher()
	items := []json.RawMessage{
		json.RawMessage("1"),
		json.RawMessage(`"two"`),
		json.RawMessage(`{"k":"v"}`),
		json.RawMessage("[1,2]"),
		json.RawMessage("null"),
		json.RawMessage("true"),
		json.RawMessage("3"),
		json.RawMessage(`"four"`),
	}
	result, strat := c.CrushMixedArray(items, "", 1.0)
	assert.Equal(t, 8, len(result))
	assert.Equal(t, "mixed:passthrough", strat)
}

func TestCrushMixedGroupsAndCompressesDicts(t *testing.T) {
	c := testCrusher()
	items := make([]json.RawMessage, 30)
	for i := 0; i < 25; i++ {
		items[i] = json.RawMessage(fmt.Sprintf(`{"id": %d, "status": "ok"}`, i))
	}
	for i := 25; i < 30; i++ {
		items[i] = json.RawMessage(fmt.Sprintf(`"string_%d"`, i))
	}
	result, strat := c.CrushMixedArray(items, "", 1.0)
	assert.True(t, strings.HasPrefix(strat, "mixed:adaptive("))
	// The 5 strings (small group) all survive.
	strCount := 0
	for _, raw := range result {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			strCount++
		}
	}
	assert.Equal(t, 5, strCount)
}

// ---------- top-level Crush ----------

func TestCrushNonJsonPassesThroughUnchanged(t *testing.T) {
	c := testCrusher()
	result := c.Crush("not json at all", "", 1.0)
	assert.False(t, result.WasModified)
	assert.Equal(t, "not json at all", result.Compressed)
	assert.Equal(t, "passthrough", result.Strategy)
}

func TestCrushScalarJsonPassesThrough(t *testing.T) {
	c := testCrusher()
	result := c.Crush("42", "", 1.0)
	assert.Equal(t, "42", result.Compressed)
	assert.False(t, result.WasModified)
}

func TestCrushSmallArrayPassesThrough(t *testing.T) {
	c := testCrusher()
	result := c.Crush("[1,2,3]", "", 1.0)
	assert.False(t, result.WasModified)
	assert.Equal(t, "[1,2,3]", result.Compressed)
}

func TestCrusherConstructionDefault(t *testing.T) {
	cfg := DefaultSmartCrusherConfig()
	c := NewSmartCrusherBuilder(cfg).WithDefaultOSSSetup().Build()
	assert.Equal(t, 15, c.Config.MaxItemsAfterCrush)
}

func TestSmartCrusherBuilderDefault(t *testing.T) {
	cfg := DefaultSmartCrusherConfig()
	crusher := NewSmartCrusherBuilder(cfg).Build()
	require.NotNil(t, crusher)
}

func TestSmartCrusherBuilderWithConfig(t *testing.T) {
	cfg := DefaultSmartCrusherConfig()
	cfg.MaxItemsAfterCrush = 1000
	crusher := NewSmartCrusherBuilder(cfg).Build()
	assert.Equal(t, 1000, crusher.Config.MaxItemsAfterCrush)
}

func TestSmartCrusherBuilderWithConstraint(t *testing.T) {
	cfg := DefaultSmartCrusherConfig()
	crusher := NewSmartCrusherBuilder(cfg).
		AddConstraint(KeepErrorsConstraint{}).
		Build()
	assert.Equal(t, 1, len(crusher.Constraints))
}

func TestSmartCrusherBuilderWithObserver(t *testing.T) {
	cfg := DefaultSmartCrusherConfig()
	crusher := NewSmartCrusherBuilder(cfg).
		AddObserver(TracingObserver{}).
		Build()
	assert.Equal(t, 1, len(crusher.Observers))
}

func TestSmartCrusherBuilderChaining(t *testing.T) {
	cfg := DefaultSmartCrusherConfig()
	crusher := NewSmartCrusherBuilder(cfg).
		AddDefaultOSSConstraints().
		AddObserver(TracingObserver{}).
		Build()
	require.NotNil(t, crusher)
	assert.Equal(t, 2, len(crusher.Constraints))
}
