package smartcrusher

import (
	"encoding/json"
	"fmt"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testAnalyzer() *SmartAnalyzer {
	return NewSmartAnalyzer(DefaultSmartCrusherConfig())
}

// ---------- AnalyzeArray ----------

func TestAnalyzeArrayEmptyReturnsNoneStrategy(t *testing.T) {
	a := testAnalyzer().AnalyzeArray(nil)
	assert.Equal(t, 0, a.ItemCount)
	assert.Empty(t, a.FieldStats)
	assert.Equal(t, "generic", a.DetectedPattern)
	assert.Equal(t, StrategyNone, a.RecommendedStrategy)
	assert.Equal(t, 0.0, a.EstimatedReduction)
	assert.Nil(t, a.Crushability)
}

func TestAnalyzeArrayNonDictFirstReturnsNone(t *testing.T) {
	items := []json.RawMessage{[]byte(`"hello"`), []byte(`"world"`)}
	a := testAnalyzer().AnalyzeArray(items)
	assert.Equal(t, 2, a.ItemCount)
	assert.Equal(t, StrategyNone, a.RecommendedStrategy)
}

func TestAnalyzeArraySmallBelowThresholdReturnsNone(t *testing.T) {
	items := make([]json.RawMessage, 4)
	for i := 0; i < 4; i++ {
		items[i], _ = json.Marshal(map[string]int{"id": i, "v": i})
	}
	a := testAnalyzer().AnalyzeArray(items)
	assert.Equal(t, StrategyNone, a.RecommendedStrategy)
}

// ---------- AnalyzeField ----------

func TestAnalyzeFieldAllNullYieldsNullType(t *testing.T) {
	items := make([]json.RawMessage, 5)
	for i := 0; i < 5; i++ {
		items[i] = []byte(`{"x":null}`)
	}
	s := testAnalyzer().AnalyzeField("x", items)
	assert.Equal(t, "null", s.FieldType)
	assert.True(t, s.IsConstant)
	assert.Equal(t, 0, s.UniqueCount)
	assert.Equal(t, 5, s.Count)
}

func TestAnalyzeFieldNumericBasicStats(t *testing.T) {
	items := make([]json.RawMessage, 10)
	for i := 0; i < 10; i++ {
		items[i], _ = json.Marshal(map[string]int{"n": i + 1})
	}
	s := testAnalyzer().AnalyzeField("n", items)
	assert.Equal(t, "numeric", s.FieldType)
	require.NotNil(t, s.MinVal)
	assert.Equal(t, 1.0, *s.MinVal)
	require.NotNil(t, s.MaxVal)
	assert.Equal(t, 10.0, *s.MaxVal)
	require.NotNil(t, s.MeanVal)
	assert.Equal(t, 5.5, *s.MeanVal)
	require.NotNil(t, s.Variance)
	assert.True(t, math.Abs(*s.Variance-9.166666666666666) < 1e-9)
}

func TestAnalyzeFieldNumericOverflowResetsStats(t *testing.T) {
	huge := 1e200
	items := []json.RawMessage{
		[]byte(fmt.Sprintf(`{"n":%e}`, huge)),
		[]byte(fmt.Sprintf(`{"n":%e}`, -huge)),
	}
	s := testAnalyzer().AnalyzeField("n", items)
	assert.Equal(t, "numeric", s.FieldType)
	assert.Nil(t, s.MinVal)
	assert.Nil(t, s.MaxVal)
	assert.Nil(t, s.MeanVal)
	require.NotNil(t, s.Variance)
	assert.Equal(t, 0.0, *s.Variance)
	assert.Empty(t, s.ChangePoints)
	assert.Equal(t, 2, s.Count)
	assert.Equal(t, 2, s.UniqueCount)
}

func TestAnalyzeFieldNumericFilterNanInf(t *testing.T) {
	items := []json.RawMessage{
		[]byte(`{"n":42.0}`),
		[]byte(`{"n":42.0}`),
	}
	s := testAnalyzer().AnalyzeField("n", items)
	require.NotNil(t, s.Variance)
	assert.Equal(t, 0.0, *s.Variance)
}

func TestAnalyzeFieldStringAvgLengthAndTopValues(t *testing.T) {
	items := []json.RawMessage{
		[]byte(`{"s":"ok"}`),
		[]byte(`{"s":"ok"}`),
		[]byte(`{"s":"warn"}`),
		[]byte(`{"s":"fail"}`),
		[]byte(`{"s":"ok"}`),
	}
	s := testAnalyzer().AnalyzeField("s", items)
	assert.Equal(t, "string", s.FieldType)
	require.NotNil(t, s.AvgLength)
	assert.Equal(t, 2.8, *s.AvgLength)
	assert.Equal(t, "ok", s.TopValues[0].Value)
	assert.Equal(t, 3, s.TopValues[0].Count)
	assert.Equal(t, 1, s.TopValues[1].Count)
	assert.Equal(t, 1, s.TopValues[2].Count)
}

func TestAnalyzeFieldConstantDetected(t *testing.T) {
	items := make([]json.RawMessage, 10)
	for i := 0; i < 10; i++ {
		items[i] = []byte(`{"flag":true}`)
	}
	s := testAnalyzer().AnalyzeField("flag", items)
	assert.True(t, s.IsConstant)
	assert.Equal(t, true, s.ConstantValue)
}

// ---------- DetectChangePoints ----------

func TestChangePointsTooFewValuesEmpty(t *testing.T) {
	cps := testAnalyzer().DetectChangePoints([]float64{1, 2, 3}, 5)
	assert.Empty(t, cps)
}

func TestChangePointsConstantValuesEmpty(t *testing.T) {
	vals := make([]float64, 20)
	for i := range vals {
		vals[i] = 5.0
	}
	cps := testAnalyzer().DetectChangePoints(vals, 5)
	assert.Empty(t, cps)
}

func TestChangePointsStepFunctionDetected(t *testing.T) {
	v := make([]float64, 90)
	for i := 0; i < 30; i++ {
		v[i] = 0
	}
	for i := 30; i < 60; i++ {
		v[i] = 100
	}
	for i := 60; i < 90; i++ {
		v[i] = 0
	}
	cps := testAnalyzer().DetectChangePoints(v, 5)
	hasExpected := false
	for _, cp := range cps {
		if cp == 30 || cp == 60 {
			hasExpected = true
			break
		}
	}
	assert.True(t, hasExpected, "expected change point near i=30 or i=60, got %v", cps)
}

// ---------- DetectPattern ----------

func TestPatternLogsMessageAndLevel(t *testing.T) {
	items := make([]json.RawMessage, 30)
	for i := 0; i < 30; i++ {
		level := "INFO"
		if i%2 != 0 {
			level = "ERROR"
		}
		items[i], _ = json.Marshal(map[string]string{
			"msg":   fmt.Sprintf("Some long unique log message body text #%d", i),
			"level": level,
		})
	}
	an := testAnalyzer()
	fieldStats := make(map[string]*FieldStats)
	for _, k := range []string{"msg", "level"} {
		fieldStats[k] = an.AnalyzeField(k, items)
	}
	p := an.DetectPattern(fieldStats, items)
	assert.Equal(t, "logs", p)
}

func TestPatternGenericWhenNothingMatches(t *testing.T) {
	items := make([]json.RawMessage, 10)
	for i := 0; i < 10; i++ {
		items[i], _ = json.Marshal(map[string]int{"a": i, "b": i * 2})
	}
	an := testAnalyzer()
	fs := make(map[string]*FieldStats)
	for _, k := range []string{"a", "b"} {
		fs[k] = an.AnalyzeField(k, items)
	}
	p := an.DetectPattern(fs, items)
	assert.Equal(t, "generic", p)
}

// ---------- DetectTemporalField ----------

func TestTemporalISODate(t *testing.T) {
	items := make([]json.RawMessage, 10)
	for i := 1; i <= 10; i++ {
		items[i-1], _ = json.Marshal(map[string]string{"d": fmt.Sprintf("2025-01-%02d", i)})
	}
	an := testAnalyzer()
	fs := map[string]*FieldStats{"d": an.AnalyzeField("d", items)}
	assert.True(t, an.DetectTemporalField(fs, items))
}

func TestTemporalISODatetime(t *testing.T) {
	items := make([]json.RawMessage, 10)
	for i := 1; i <= 10; i++ {
		items[i-1], _ = json.Marshal(map[string]string{"t": fmt.Sprintf("2025-01-%02dT12:00:00Z", i)})
	}
	an := testAnalyzer()
	fs := map[string]*FieldStats{"t": an.AnalyzeField("t", items)}
	assert.True(t, an.DetectTemporalField(fs, items))
}

func TestTemporalUnixSecondsRange(t *testing.T) {
	items := make([]json.RawMessage, 10)
	for i := 0; i < 10; i++ {
		items[i], _ = json.Marshal(map[string]int64{"ts": 1700000000 + int64(i)*86400})
	}
	an := testAnalyzer()
	fs := map[string]*FieldStats{"ts": an.AnalyzeField("ts", items)}
	assert.True(t, an.DetectTemporalField(fs, items))
}

func TestTemporalNormalNumbersNotDetected(t *testing.T) {
	items := make([]json.RawMessage, 10)
	for i := 1; i <= 10; i++ {
		items[i-1], _ = json.Marshal(map[string]int{"n": i})
	}
	an := testAnalyzer()
	fs := map[string]*FieldStats{"n": an.AnalyzeField("n", items)}
	assert.False(t, an.DetectTemporalField(fs, items))
}

// ---------- AnalyzeCrushability ----------

func TestCrushabilityLowUniquenessSafeToSample(t *testing.T) {
	items := make([]json.RawMessage, 30)
	for i := 0; i < 30; i++ {
		items[i], _ = json.Marshal(map[string]string{"status": "ok"})
	}
	an := testAnalyzer()
	fs := map[string]*FieldStats{"status": an.AnalyzeField("status", items)}
	c := an.AnalyzeCrushability(items, fs)
	assert.True(t, c.Crushable)
	assert.Equal(t, "low_uniqueness_safe_to_sample", c.Reason)
}

func TestCrushabilityUniqueEntitiesNoSignalSkips(t *testing.T) {
	items := make([]json.RawMessage, 20)
	for i := 0; i < 20; i++ {
		items[i], _ = json.Marshal(map[string]interface{}{"id": i, "name": fmt.Sprintf("user_%d", i)})
	}
	an := testAnalyzer()
	fs := make(map[string]*FieldStats)
	for _, k := range []string{"id", "name"} {
		fs[k] = an.AnalyzeField(k, items)
	}
	c := an.AnalyzeCrushability(items, fs)
	assert.False(t, c.Crushable)
	assert.Equal(t, "unique_entities_no_signal", c.Reason)
}

func TestCrushabilityRepetitiveContentWithIDsCrushes(t *testing.T) {
	items := make([]json.RawMessage, 20)
	for i := 0; i < 20; i++ {
		items[i], _ = json.Marshal(map[string]interface{}{"id": i, "status": "ok"})
	}
	an := testAnalyzer()
	fs := make(map[string]*FieldStats)
	for _, k := range []string{"id", "status"} {
		fs[k] = an.AnalyzeField(k, items)
	}
	c := an.AnalyzeCrushability(items, fs)
	assert.True(t, c.Crushable)
	assert.Equal(t, "repetitive_content_with_ids", c.Reason)
}

// ---------- SelectStrategy ----------

func TestSelectStrategyBelowMinReturnsNone(t *testing.T) {
	fs := make(map[string]*FieldStats)
	s := testAnalyzer().SelectStrategy(fs, "generic", 3, nil)
	assert.Equal(t, StrategyNone, s)
}

func TestSelectStrategySkipWhenNotCrushable(t *testing.T) {
	fs := make(map[string]*FieldStats)
	crush := CrushabilitySkip("nope", 0.9)
	s := testAnalyzer().SelectStrategy(fs, "generic", 100, &crush)
	assert.Equal(t, StrategySkip, s)
}

func TestSelectStrategySearchResultsReturnsTopN(t *testing.T) {
	fs := make(map[string]*FieldStats)
	s := testAnalyzer().SelectStrategy(fs, "search_results", 100, nil)
	assert.Equal(t, StrategyTopN, s)
}

func TestSelectStrategyGenericReturnsSmartSample(t *testing.T) {
	fs := make(map[string]*FieldStats)
	s := testAnalyzer().SelectStrategy(fs, "generic", 100, nil)
	assert.Equal(t, StrategySmartSample, s)
}

// ---------- EstimateReduction ----------

func TestEstimateReductionNoneReturnsZero(t *testing.T) {
	fs := make(map[string]*FieldStats)
	r := testAnalyzer().EstimateReduction(fs, StrategyNone, 100)
	assert.Equal(t, 0.0, r)
}

func TestEstimateReductionCapsAt095(t *testing.T) {
	fs := map[string]*FieldStats{
		"a": {Name: "a", FieldType: "string", Count: 10, UniqueCount: 1, UniqueRatio: 0.1, IsConstant: true, ConstantValue: "v"},
		"b": {Name: "b", FieldType: "string", Count: 10, UniqueCount: 1, UniqueRatio: 0.1, IsConstant: true, ConstantValue: "v"},
	}
	r := testAnalyzer().EstimateReduction(fs, StrategyClusterSample, 10)
	assert.Equal(t, 0.95, r)
}

func TestEstimateReductionSmartSampleNoConstants(t *testing.T) {
	fs := map[string]*FieldStats{
		"id": {Name: "id", FieldType: "numeric", Count: 100, UniqueCount: 100, UniqueRatio: 1.0, IsConstant: false},
	}
	r := testAnalyzer().EstimateReduction(fs, StrategySmartSample, 100)
	assert.Equal(t, 0.5, r)
}

// ---------- helpers ----------

func TestISODatetimePatternMatches(t *testing.T) {
	assert.True(t, isISODatetime("2025-01-15T12:00:00"))
	assert.True(t, isISODatetime("2025-01-15 12:00:00"))
	assert.True(t, isISODatetime("2025-01-15T12:00:00.123Z"))
	assert.False(t, isISODatetime("2025-01-15"))
	assert.False(t, isISODatetime("not a date"))
}

func TestISODatePatternMatches(t *testing.T) {
	assert.True(t, isISODate("2025-01-15"))
	assert.False(t, isISODate("2025-01-15T12:00:00"))
	assert.False(t, isISODate("2025/01/15"))
}

func TestPythonReprBasics(t *testing.T) {
	assert.Equal(t, "None", pythonRepr(nil, true))
	assert.Equal(t, "True", pythonRepr(true, false))
	assert.Equal(t, "False", pythonRepr(false, false))
	assert.Equal(t, "42", pythonRepr(float64(42), false))
	assert.Equal(t, "hello", pythonRepr("hello", false))
}

func TestTopNFirstOccurrenceTieBreak(t *testing.T) {
	strs := []string{"a", "b", "a", "b", "c"}
	top := topNByCount(strs, 5)
	assert.Equal(t, "a", top[0].Value)
	assert.Equal(t, "b", top[1].Value)
	assert.Equal(t, "c", top[2].Value)
}
