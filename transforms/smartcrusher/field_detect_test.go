package smartcrusher

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

func testFieldStats(name, fieldType string, uniqueRatio float64) FieldStats {
	return FieldStats{
		Name:        name,
		FieldType:   fieldType,
		Count:       100,
		UniqueCount: int(100.0 * uniqueRatio),
		UniqueRatio: uniqueRatio,
	}
}

func testFieldStatsWithRange(name string, minV, maxV float64) FieldStats {
	s := testFieldStats(name, "numeric", 1.0)
	s.MinVal = &minV
	s.MaxVal = &maxV
	return s
}

// ---------- DetectIDFieldStatistically ----------

func TestIDFieldLowUniquenessRejected(t *testing.T) {
	s := testFieldStats("status", "string", 0.5)
	values := parseItems(`["ok","error","ok","ok"]`)
	isID, conf := DetectIDFieldStatistically(&s, values)
	assert.False(t, isID)
	assert.Equal(t, 0.0, conf)
}

func TestIDFieldUUIDStringsHighConfidence(t *testing.T) {
	s := testFieldStats("uid", "string", 1.0)
	var values []json.RawMessage
	for i := 0; i < 20; i++ {
		uid := "550e8400-e29b-41d4-a716-" + padHex12(i)
		b, _ := json.Marshal(uid)
		values = append(values, b)
	}
	isID, conf := DetectIDFieldStatistically(&s, values)
	assert.True(t, isID)
	assert.Equal(t, 0.95, conf)
}

func TestIDFieldHighEntropyStrings(t *testing.T) {
	s := testFieldStats("uid", "string", 0.96)
	var values []json.RawMessage
	for i := 0; i < 20; i++ {
		hex := "a3f7b2c" + padHex06(i) + "d8e1f4a7"
		b, _ := json.Marshal(hex)
		values = append(values, b)
	}
	isID, conf := DetectIDFieldStatistically(&s, values)
	assert.True(t, isID)
	assert.True(t, math.Abs(conf-0.8) < 1e-9)
}

func TestIDFieldSequentialNumeric(t *testing.T) {
	s := testFieldStats("id", "numeric", 0.96)
	minV := 1.0
	maxV := 100.0
	s.MinVal = &minV
	s.MaxVal = &maxV
	var values []json.RawMessage
	for i := 1; i <= 100; i++ {
		b, _ := json.Marshal(i)
		values = append(values, b)
	}
	isID, conf := DetectIDFieldStatistically(&s, values)
	assert.True(t, isID)
	assert.True(t, math.Abs(conf-0.9) < 1e-9)
}

func TestIDFieldHighUniquenessAloneTriggersCatchall(t *testing.T) {
	s := testFieldStats("misc", "numeric", 0.99)
	minV := 0.0
	maxV := 0.0
	s.MinVal = &minV
	s.MaxVal = &maxV
	var values []json.RawMessage
	for i := 0; i < 100; i++ {
		b, _ := json.Marshal(0)
		values = append(values, b)
	}
	isID, conf := DetectIDFieldStatistically(&s, values)
	assert.True(t, isID)
	assert.True(t, math.Abs(conf-0.7) < 1e-9)
}

// ---------- DetectScoreFieldStatistically ----------

func TestScoreFieldUnitRangeWithDescendingSort(t *testing.T) {
	s := testFieldStatsWithRange("score", 0.0, 1.0)
	var items []json.RawMessage
	for i := 9; i >= 0; i-- {
		b, _ := json.Marshal(map[string]interface{}{"score": float64(i) / 10.0})
		items = append(items, b)
	}
	isScore, conf := DetectScoreFieldStatistically(&s, items)
	assert.True(t, isScore)
	assert.GreaterOrEqual(t, conf, 0.7)
	assert.LessOrEqual(t, conf, 0.95)
}

func TestScoreFieldSequentialRejected(t *testing.T) {
	s := testFieldStatsWithRange("score", 1.0, 10.0)
	var items []json.RawMessage
	for i := 1; i <= 10; i++ {
		b, _ := json.Marshal(map[string]interface{}{"score": i})
		items = append(items, b)
	}
	isScore, _ := DetectScoreFieldStatistically(&s, items)
	assert.False(t, isScore)
}

func TestScoreFieldUnboundedRangeRejected(t *testing.T) {
	s := testFieldStatsWithRange("metric", 0.0, 1000.0)
	var items []json.RawMessage
	for i := 0; i < 10; i++ {
		b, _ := json.Marshal(map[string]interface{}{"metric": i * 100})
		items = append(items, b)
	}
	isScore, _ := DetectScoreFieldStatistically(&s, items)
	assert.False(t, isScore)
}

func TestScoreFieldSignedSimilarityRange(t *testing.T) {
	s := testFieldStatsWithRange("similarity", -0.9, 0.95)
	var items []json.RawMessage
	for i := 9; i >= 0; i-- {
		b, _ := json.Marshal(map[string]interface{}{"similarity": float64(i)/10.0 - 0.5})
		items = append(items, b)
	}
	isScore, _ := DetectScoreFieldStatistically(&s, items)
	assert.True(t, isScore)
}

func TestScoreFieldBelowThresholdRejected(t *testing.T) {
	s := testFieldStatsWithRange("metric", 0.0, 100.0)
	items := []json.RawMessage{
		[]byte(`{"metric":50}`),
		[]byte(`{"metric":10}`),
		[]byte(`{"metric":80}`),
		[]byte(`{"metric":20}`),
		[]byte(`{"metric":90}`),
	}
	isScore, _ := DetectScoreFieldStatistically(&s, items)
	assert.False(t, isScore)
}

func TestScoreFieldNonNumericRejected(t *testing.T) {
	s := testFieldStats("name", "string", 0.5)
	items := []json.RawMessage{
		[]byte(`{"name":"alice"}`),
		[]byte(`{"name":"bob"}`),
		[]byte(`{"name":"alice"}`),
	}
	isScore, _ := DetectScoreFieldStatistically(&s, items)
	assert.False(t, isScore)
}

func TestScoreFieldMissingMinMaxRejected(t *testing.T) {
	s := testFieldStats("score", "numeric", 1.0) // no min/max
	items := []json.RawMessage{[]byte(`{"score":0.5}`)}
	isScore, _ := DetectScoreFieldStatistically(&s, items)
	assert.False(t, isScore)
}

func TestScoreFieldConfidenceCappedAt95(t *testing.T) {
	s := testFieldStatsWithRange("score", 0.0, 1.0)
	var items []json.RawMessage
	for i := 49; i >= 0; i-- {
		b, _ := json.Marshal(map[string]interface{}{"score": float64(i) / 50.0})
		items = append(items, b)
	}
	_, conf := DetectScoreFieldStatistically(&s, items)
	assert.LessOrEqual(t, conf, 0.95)
}

// helpers

func padHex12(n int) string {
	s := ""
	for i := 0; i < 12; i++ {
		s = string(rune('0'+n%16)) + s
		n /= 16
	}
	// Replace non-hex chars
	result := make([]byte, 12)
	for i, c := range s {
		if c >= '0' && c <= '9' {
			result[i] = byte(c)
		} else {
			result[i] = byte('a' + (c - '0') - 10)
		}
	}
	return string(result)
}

func padHex06(n int) string {
	s := ""
	for i := 0; i < 6; i++ {
		digit := n % 16
		if digit < 10 {
			s = string(rune('0'+digit)) + s
		} else {
			s = string(rune('a'+digit-10)) + s
		}
		n /= 16
	}
	return s
}
