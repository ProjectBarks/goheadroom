package smartcrusher

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

// ---------- DetectStructuralOutliers ----------

func TestOutliersTooFewItemsReturnsEmpty(t *testing.T) {
	items := make([]json.RawMessage, 3)
	for i := 0; i < 3; i++ {
		items[i], _ = json.Marshal(map[string]int{"a": i})
	}
	assert.Empty(t, DetectStructuralOutliers(items))
}

func TestOutliersRareFieldFlagsItem(t *testing.T) {
	items := make([]json.RawMessage, 10)
	for i := 0; i < 9; i++ {
		items[i], _ = json.Marshal(map[string]int{"a": i})
	}
	items[9], _ = json.Marshal(map[string]interface{}{"a": 9, "x": "rare"})
	outliers := DetectStructuralOutliers(items)
	assert.Contains(t, outliers, 9)
}

func TestOutliersNoDictItemsSilentlySkipped(t *testing.T) {
	items := []json.RawMessage{
		[]byte(`{"status":"ok"}`),
		[]byte(`"string-not-dict"`),
		[]byte(`{"status":"ok"}`),
		[]byte(`{"status":"ok"}`),
		[]byte(`{"status":"ok"}`),
	}
	// Should not panic.
	DetectStructuralOutliers(items)
}

// ---------- DetectRareStatusValues (BUG #3 FIX) ----------

func TestRareStatusLowCardinalityDominantValue(t *testing.T) {
	items := make([]json.RawMessage, 100)
	for i := 0; i < 95; i++ {
		items[i], _ = json.Marshal(map[string]string{"status": "ok"})
	}
	items[95], _ = json.Marshal(map[string]string{"status": "error"})
	items[96], _ = json.Marshal(map[string]string{"status": "timeout"})
	items[97], _ = json.Marshal(map[string]string{"status": "error"})
	items[98], _ = json.Marshal(map[string]string{"status": "timeout"})
	items[99], _ = json.Marshal(map[string]string{"status": "fail"})
	common := map[string]bool{"status": true}
	outliers := DetectRareStatusValues(items, common)
	assert.Len(t, outliers, 5)
}

func TestRareStatusBug3HighCardinalityBimodal(t *testing.T) {
	var items []json.RawMessage
	for i := 0; i < 60; i++ {
		b, _ := json.Marshal(map[string]string{"code": "INFO"})
		items = append(items, b)
	}
	for i := 0; i < 25; i++ {
		b, _ := json.Marshal(map[string]string{"code": "WARN"})
		items = append(items, b)
	}
	for i := 0; i < 15; i++ {
		b, _ := json.Marshal(map[string]string{"code": "ERR_" + itoa2(i)})
		items = append(items, b)
	}
	common := map[string]bool{"code": true}
	outliers := DetectRareStatusValues(items, common)
	assert.Len(t, outliers, 15)
}

func TestRareStatusUniformDistributionNoOutliers(t *testing.T) {
	items := make([]json.RawMessage, 50)
	for i := 0; i < 50; i++ {
		b, _ := json.Marshal(map[string]string{"code": "CAT_" + itoa2(i)})
		items[i] = b
	}
	common := map[string]bool{"code": true}
	outliers := DetectRareStatusValues(items, common)
	assert.Empty(t, outliers, "uniform distribution must not produce rare-status outliers")
}

func TestRareStatusCardinalityAbove50Skipped(t *testing.T) {
	items := make([]json.RawMessage, 60)
	for i := 0; i < 60; i++ {
		b, _ := json.Marshal(map[string]string{"code": "V_" + itoa2(i)})
		items[i] = b
	}
	common := map[string]bool{"code": true}
	assert.Empty(t, DetectRareStatusValues(items, common))
}

func TestRareStatusCardinalityOneSkipped(t *testing.T) {
	items := make([]json.RawMessage, 100)
	for i := 0; i < 100; i++ {
		items[i], _ = json.Marshal(map[string]string{"status": "ok"})
	}
	common := map[string]bool{"status": true}
	assert.Empty(t, DetectRareStatusValues(items, common))
}

func TestRareStatusNullsFilteredFromCardinality(t *testing.T) {
	items := make([]json.RawMessage, 100)
	for i := 0; i < 95; i++ {
		items[i], _ = json.Marshal(map[string]string{"s": "ok"})
	}
	for i := 95; i < 100; i++ {
		items[i] = []byte(`{"s":null}`)
	}
	common := map[string]bool{"s": true}
	outliers := DetectRareStatusValues(items, common)
	assert.Empty(t, outliers, "cardinality 1 (after null filter) must skip the field")
}

func TestRareStatusNullsCountInValueCountsWhenCardinalityPasses(t *testing.T) {
	items := make([]json.RawMessage, 100)
	for i := 0; i < 90; i++ {
		items[i], _ = json.Marshal(map[string]string{"s": "ok"})
	}
	for i := 90; i < 95; i++ {
		items[i], _ = json.Marshal(map[string]string{"s": "warn"})
	}
	for i := 95; i < 100; i++ {
		items[i] = []byte(`{"s":null}`)
	}
	common := map[string]bool{"s": true}
	outliers := DetectRareStatusValues(items, common)
	assert.Len(t, outliers, 10, "5 warn + 5 null = 10 outliers")
}

// ---------- DetectErrorItemsForPreservation ----------

func TestErrorKeywordsPreservationBasic(t *testing.T) {
	items := []json.RawMessage{
		[]byte(`{"status":"ok"}`),
		[]byte(`{"status":"error","msg":"boom"}`),
		[]byte(`{"status":"ok"}`),
		[]byte(`{"msg":"request failed"}`),
		[]byte(`{"status":"ok"}`),
	}
	errs := DetectErrorItemsForPreservation(items, nil)
	assert.Equal(t, []int{1, 3}, errs)
}

func TestErrorKeywordsCaseInsensitive(t *testing.T) {
	items := []json.RawMessage{
		[]byte(`{"msg":"FATAL: out of memory"}`),
		[]byte(`{"msg":"panic at line 42"}`),
	}
	errs := DetectErrorItemsForPreservation(items, nil)
	assert.Equal(t, []int{0, 1}, errs)
}

func TestErrorKeywordsNoMatch(t *testing.T) {
	items := []json.RawMessage{
		[]byte(`{"name":"alice"}`),
		[]byte(`{"count":5}`),
	}
	errs := DetectErrorItemsForPreservation(items, nil)
	assert.Empty(t, errs)
}

func TestErrorKeywordsUsesCachedStrings(t *testing.T) {
	items := []json.RawMessage{
		[]byte(`{"a":1}`),
		[]byte(`{"b":2}`),
	}
	cached := []string{"error", "ok"}
	errs := DetectErrorItemsForPreservation(items, cached)
	assert.Equal(t, []int{0}, errs)
}

func TestErrorKeywordsFallsBackWhenCacheTooShort(t *testing.T) {
	items := []json.RawMessage{
		[]byte(`{"a":1}`),
		[]byte(`{"msg":"error"}`),
	}
	cached := []string{"ok"} // only 1 entry
	errs := DetectErrorItemsForPreservation(items, cached)
	assert.Equal(t, []int{1}, errs)
}

func TestErrorKeywordsSkipsNonDictItems(t *testing.T) {
	items := []json.RawMessage{
		[]byte(`{"msg":"error"}`),
		[]byte(`"error string"`), // not a dict
		[]byte(`{"msg":"error"}`),
	}
	errs := DetectErrorItemsForPreservation(items, nil)
	assert.Equal(t, []int{0, 2}, errs)
}

func itoa2(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}
