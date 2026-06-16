package smartcrusher

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClassifyArrayEmpty(t *testing.T) {
	assert.Equal(t, ArrayEmpty, ClassifyArray(nil))
}

func TestClassifyArrayPureDictArray(t *testing.T) {
	items := parseItems(`[{"a":1},{"b":2}]`)
	assert.Equal(t, ArrayDictArray, ClassifyArray(items))
}

func TestClassifyArrayPureStringArray(t *testing.T) {
	items := parseItems(`["a","b","c"]`)
	assert.Equal(t, ArrayStringArray, ClassifyArray(items))
}

func TestClassifyArrayPureNumberArray(t *testing.T) {
	items := parseItems(`[1,2.5,3]`)
	assert.Equal(t, ArrayNumberArray, ClassifyArray(items))
}

func TestClassifyArrayPureBoolArray(t *testing.T) {
	items := parseItems(`[true,false,true]`)
	assert.Equal(t, ArrayBoolArray, ClassifyArray(items))
}

func TestClassifyArrayNestedArray(t *testing.T) {
	items := parseItems(`[[1,2],[3,4]]`)
	assert.Equal(t, ArrayNestedArray, ClassifyArray(items))
}

func TestClassifyArrayMixedDictAndString(t *testing.T) {
	items := parseItems(`[{"a":1},"str"]`)
	assert.Equal(t, ArrayMixedArray, ClassifyArray(items))
}

func TestClassifyArrayBoolWithNumberIsMixed(t *testing.T) {
	items := parseItems(`[true,false,1]`)
	assert.Equal(t, ArrayMixedArray, ClassifyArray(items))
}

func TestClassifyArrayNullInArrayIsMixed(t *testing.T) {
	items := parseItems(`[{"a":1},null]`)
	assert.Equal(t, ArrayMixedArray, ClassifyArray(items))
}

func TestClassifyArrayAsStrMatchesPython(t *testing.T) {
	assert.Equal(t, "dict_array", ArrayDictArray.String())
	assert.Equal(t, "string_array", ArrayStringArray.String())
	assert.Equal(t, "number_array", ArrayNumberArray.String())
	assert.Equal(t, "bool_array", ArrayBoolArray.String())
	assert.Equal(t, "nested_array", ArrayNestedArray.String())
	assert.Equal(t, "mixed_array", ArrayMixedArray.String())
	assert.Equal(t, "empty", ArrayEmpty.String())
}

// parseItems parses a JSON array into individual raw messages.
func parseItems(jsonArray string) []json.RawMessage {
	var items []json.RawMessage
	if err := json.Unmarshal([]byte(jsonArray), &items); err != nil {
		panic("bad test JSON: " + err.Error())
	}
	return items
}
