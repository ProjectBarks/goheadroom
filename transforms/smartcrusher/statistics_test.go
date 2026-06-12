package smartcrusher

import (
	"encoding/json"
	"fmt"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

// ---------- IsUUIDFormat ----------

func TestUUIDFormatCanonicalLowercase(t *testing.T) {
	assert.True(t, IsUUIDFormat("550e8400-e29b-41d4-a716-446655440000"))
}

func TestUUIDFormatUppercase(t *testing.T) {
	assert.True(t, IsUUIDFormat("550E8400-E29B-41D4-A716-446655440000"))
}

func TestUUIDFormatWrongLengthRejected(t *testing.T) {
	assert.False(t, IsUUIDFormat("550e8400-e29b-41d4-a716-44665544000"))
	assert.False(t, IsUUIDFormat("550e8400-e29b-41d4-a716-4466554400000"))
}

func TestUUIDFormatWrongSegmentCount(t *testing.T) {
	assert.False(t, IsUUIDFormat("550e8400e29b41d4a716446655440000"))
}

func TestUUIDFormatNonHexRejected(t *testing.T) {
	assert.False(t, IsUUIDFormat("550e8400-e29b-41d4-a716-44665544000z"))
}

func TestUUIDFormatEmptyRejected(t *testing.T) {
	assert.False(t, IsUUIDFormat(""))
}

// ---------- CalculateStringEntropy ----------

func TestEntropyEmptyStringIsZero(t *testing.T) {
	assert.Equal(t, 0.0, CalculateStringEntropy(""))
}

func TestEntropySingleCharIsZero(t *testing.T) {
	assert.Equal(t, 0.0, CalculateStringEntropy("a"))
}

func TestEntropyAllSameCharsIsZero(t *testing.T) {
	assert.Equal(t, 0.0, CalculateStringEntropy("aaaa"))
}

func TestEntropyPerfectlyUniformNormalizedToOne(t *testing.T) {
	e := CalculateStringEntropy("ab")
	assert.True(t, math.Abs(e-1.0) < 1e-9)
}

func TestEntropyMostlyRepeatedLow(t *testing.T) {
	e := CalculateStringEntropy("aaaaaab")
	assert.Less(t, e, 0.7)
}

func TestEntropyHighForRandomLookingString(t *testing.T) {
	e := CalculateStringEntropy("a3f7b2c9d8e1f4a7")
	assert.Greater(t, e, 0.7)
}

// ---------- DetectSequentialPattern ----------

func jsonNums(nums ...int) []json.RawMessage {
	result := make([]json.RawMessage, len(nums))
	for i, n := range nums {
		result[i], _ = json.Marshal(n)
	}
	return result
}

func jsonFloats(nums ...float64) []json.RawMessage {
	result := make([]json.RawMessage, len(nums))
	for i, n := range nums {
		result[i], _ = json.Marshal(n)
	}
	return result
}

func jsonStr(s string) json.RawMessage {
	b, _ := json.Marshal(s)
	return b
}

func jsonBool(b bool) json.RawMessage {
	data, _ := json.Marshal(b)
	return data
}

func TestSequentialSimpleIntAscending(t *testing.T) {
	v := jsonNums(1, 2, 3, 4, 5, 6, 7, 8, 9, 10)
	assert.True(t, DetectSequentialPattern(v, true))
}

func TestSequentialTooFewValues(t *testing.T) {
	v := jsonNums(1, 2, 3)
	assert.False(t, DetectSequentialPattern(v, true))
}

func TestSequentialRandomNumbersNotDetected(t *testing.T) {
	v := jsonNums(100, 2, 85, 7, 43, 17)
	assert.False(t, DetectSequentialPattern(v, true))
}

func TestSequentialDescendingWithCheckOrderRejected(t *testing.T) {
	v := jsonNums(10, 9, 8, 7, 6, 5, 4, 3, 2, 1)
	assert.False(t, DetectSequentialPattern(v, true))
}

func TestSequentialDescendingWithoutCheckOrderAccepted(t *testing.T) {
	v := jsonNums(10, 9, 8, 7, 6, 5, 4, 3, 2, 1)
	assert.True(t, DetectSequentialPattern(v, false))
}

func TestBug2ZeroPaddedStringsNoLongerMisclassified(t *testing.T) {
	v := make([]json.RawMessage, 10)
	for i := 0; i < 10; i++ {
		s := fmt.Sprintf("%03d", i+1)
		v[i], _ = json.Marshal(s)
	}
	assert.False(t, DetectSequentialPattern(v, true),
		"BUG #2 fix: zero-padded string IDs must not be classified as sequential")
}

func TestBug2MixedStringAndIntStillDetected(t *testing.T) {
	v := make([]json.RawMessage, 0)
	for _, n := range []int{1, 2} {
		b, _ := json.Marshal(n)
		v = append(v, b)
	}
	v = append(v, jsonStr("3"))
	for _, n := range []int{4, 5, 6} {
		b, _ := json.Marshal(n)
		v = append(v, b)
	}
	assert.True(t, DetectSequentialPattern(v, true))
}

func TestSequentialBoolsExcluded(t *testing.T) {
	v := []json.RawMessage{
		jsonBool(true), jsonBool(false), jsonBool(true),
		jsonBool(false), jsonBool(true), jsonBool(false),
	}
	assert.False(t, DetectSequentialPattern(v, true))
}

func TestSequentialFloatsWithUnitStep(t *testing.T) {
	v := jsonFloats(1, 2, 3, 4, 5, 6, 7, 8, 9, 10)
	assert.True(t, DetectSequentialPattern(v, true))
}

func TestSequentialFractionalUnitStep(t *testing.T) {
	v := jsonFloats(1.5, 2.5, 3.5, 4.5, 5.5)
	assert.True(t, DetectSequentialPattern(v, true))
}

func TestBug2AllUnparseableStringsReturnsFalse(t *testing.T) {
	v := []json.RawMessage{
		jsonStr("abc"), jsonStr("def"), jsonStr("ghi"), jsonStr("jkl"), jsonStr("mno"),
	}
	assert.False(t, DetectSequentialPattern(v, true))
}

func TestBug2SingleIntAmongStringsStillDetects(t *testing.T) {
	n3, _ := json.Marshal(3)
	v := []json.RawMessage{
		jsonStr("001"), jsonStr("002"), n3, jsonStr("004"), jsonStr("005"), jsonStr("006"),
	}
	assert.True(t, DetectSequentialPattern(v, true))
}

// ---------- PythonIntParse ----------

func TestPythonIntParseBasic(t *testing.T) {
	v, ok := PythonIntParse("5")
	assert.True(t, ok)
	assert.Equal(t, int64(5), v)

	v, ok = PythonIntParse("-5")
	assert.True(t, ok)
	assert.Equal(t, int64(-5), v)

	v, ok = PythonIntParse("+5")
	assert.True(t, ok)
	assert.Equal(t, int64(5), v)
}

func TestPythonIntParseStripsWhitespace(t *testing.T) {
	v, ok := PythonIntParse("  5  ")
	assert.True(t, ok)
	assert.Equal(t, int64(5), v)

	v, ok = PythonIntParse("\t-3\n")
	assert.True(t, ok)
	assert.Equal(t, int64(-3), v)
}

func TestPythonIntParseUnderscores(t *testing.T) {
	v, ok := PythonIntParse("3_000")
	assert.True(t, ok)
	assert.Equal(t, int64(3000), v)

	v, ok = PythonIntParse("1_000_000")
	assert.True(t, ok)
	assert.Equal(t, int64(1000000), v)
}

func TestPythonIntParseUnderscoreEdgeCasesRejected(t *testing.T) {
	_, ok := PythonIntParse("_5")
	assert.False(t, ok)
	_, ok = PythonIntParse("5_")
	assert.False(t, ok)
	_, ok = PythonIntParse("3__000")
	assert.False(t, ok)
}

func TestPythonIntParseRejectsFloats(t *testing.T) {
	_, ok := PythonIntParse("3.14")
	assert.False(t, ok)
}

func TestPythonIntParseRejectsNonNumeric(t *testing.T) {
	_, ok := PythonIntParse("abc")
	assert.False(t, ok)
	_, ok = PythonIntParse("")
	assert.False(t, ok)
	_, ok = PythonIntParse("   ")
	assert.False(t, ok)
}

func TestSequentialWithWhitespacePaddedStrings(t *testing.T) {
	n1, _ := json.Marshal(1)
	n3, _ := json.Marshal(3)
	n5, _ := json.Marshal(5)
	n6, _ := json.Marshal(6)
	v := []json.RawMessage{
		n1, jsonStr("  2  "), n3, jsonStr(" 4 "), n5, n6,
	}
	assert.True(t, DetectSequentialPattern(v, true))
}
