package smartcrusher

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMarshalOrderedJSONPreservesKeyOrder(t *testing.T) {
	original := []byte(`{"z_last":"1","a_first":"2","m_middle":"3"}`)
	var value interface{}
	require.NoError(t, json.Unmarshal(original, &value))

	result, err := marshalOrderedJSON(original, value)
	require.NoError(t, err)
	assert.Equal(t, `{"z_last":"1","a_first":"2","m_middle":"3"}`, string(result))
}

func TestMarshalOrderedJSONNestedObjects(t *testing.T) {
	original := []byte(`{"outer_b":"val","outer_a":{"inner_z":1,"inner_a":2}}`)
	var value interface{}
	require.NoError(t, json.Unmarshal(original, &value))

	result, err := marshalOrderedJSON(original, value)
	require.NoError(t, err)
	assert.Equal(t, `{"outer_b":"val","outer_a":{"inner_z":1,"inner_a":2}}`, string(result))
}

func TestMarshalOrderedJSONRemovedKeys(t *testing.T) {
	original := []byte(`{"keep":"1","remove":"2","also_keep":"3"}`)

	// Simulate ProcessValue removing a key.
	value := map[string]interface{}{
		"keep":      "1",
		"also_keep": "3",
	}

	result, err := marshalOrderedJSON(original, value)
	require.NoError(t, err)
	assert.Equal(t, `{"keep":"1","also_keep":"3"}`, string(result))
}

func TestMarshalOrderedJSONNewKeysAppended(t *testing.T) {
	original := []byte(`{"b":"1","a":"2"}`)

	// Value has a key not in original.
	value := map[string]interface{}{
		"b":   "1",
		"a":   "2",
		"new": "3",
	}

	result, err := marshalOrderedJSON(original, value)
	require.NoError(t, err)
	// Original keys first in original order, then new key.
	assert.Equal(t, `{"b":"1","a":"2","new":"3"}`, string(result))
}

func TestMarshalOrderedJSONArrayOfObjects(t *testing.T) {
	original := []byte(`[{"z":1,"a":2},{"y":3,"b":4}]`)
	var value interface{}
	require.NoError(t, json.Unmarshal(original, &value))

	result, err := marshalOrderedJSON(original, value)
	require.NoError(t, err)
	assert.Equal(t, `[{"z":1,"a":2},{"y":3,"b":4}]`, string(result))
}

func TestMarshalOrderedJSONScalarPassthrough(t *testing.T) {
	original := []byte(`"hello"`)
	var value interface{}
	require.NoError(t, json.Unmarshal(original, &value))

	result, err := marshalOrderedJSON(original, value)
	require.NoError(t, err)
	assert.Equal(t, `"hello"`, string(result))
}

func TestMarshalOrderedJSONNullValue(t *testing.T) {
	original := []byte(`null`)
	result, err := marshalOrderedJSON(original, nil)
	require.NoError(t, err)
	assert.Equal(t, `null`, string(result))
}

func TestMarshalOrderedJSONBoolValues(t *testing.T) {
	original := []byte(`{"flag":true,"other":false}`)
	var value interface{}
	require.NoError(t, json.Unmarshal(original, &value))

	result, err := marshalOrderedJSON(original, value)
	require.NoError(t, err)
	assert.Equal(t, `{"flag":true,"other":false}`, string(result))
}

func TestMarshalOrderedJSONInvalidOriginalFallsBack(t *testing.T) {
	original := []byte(`not valid json`)
	value := map[string]interface{}{"a": 1.0}

	result, err := marshalOrderedJSON(original, value)
	require.NoError(t, err)
	// Falls back to json.Marshal which sorts keys.
	assert.Equal(t, `{"a":1}`, string(result))
}

func TestMarshalOrderedJSONParityFixture(t *testing.T) {
	// Matches the actual failing fixture: request_id before events.
	original := []byte(`{"request_id": "req-1", "events": [{"step": 0, "kind": "trace"}]}`)
	value := map[string]interface{}{
		"request_id": "req-1",
		"events": []interface{}{
			map[string]interface{}{"step": 0.0, "kind": "trace"},
		},
	}

	result, err := marshalOrderedJSON(original, value)
	require.NoError(t, err)

	// Verify request_id comes before events.
	var check map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(result, &check))

	resultStr := string(result)
	reqIdx := indexOf(resultStr, `"request_id"`)
	evtIdx := indexOf(resultStr, `"events"`)
	assert.True(t, reqIdx < evtIdx, "request_id should appear before events, got: %s", resultStr)

	// Also verify inner object key order: step before kind.
	stepIdx := indexOf(resultStr, `"step"`)
	kindIdx := indexOf(resultStr, `"kind"`)
	assert.True(t, stepIdx < kindIdx, "step should appear before kind, got: %s", resultStr)
}

func TestMarshalOrderedJSONEmptyObject(t *testing.T) {
	original := []byte(`{}`)
	value := map[string]interface{}{}

	result, err := marshalOrderedJSON(original, value)
	require.NoError(t, err)
	assert.Equal(t, `{}`, string(result))
}

func TestMarshalOrderedJSONEmptyArray(t *testing.T) {
	original := []byte(`[]`)
	value := []interface{}{}

	result, err := marshalOrderedJSON(original, value)
	require.NoError(t, err)
	assert.Equal(t, `[]`, string(result))
}

func TestMarshalOrderedJSONStringEscaping(t *testing.T) {
	original := []byte(`{"key":"value with \"quotes\" and \\backslash"}`)
	var value interface{}
	require.NoError(t, json.Unmarshal(original, &value))

	result, err := marshalOrderedJSON(original, value)
	require.NoError(t, err)

	// Re-parse to verify correctness.
	var roundtrip interface{}
	require.NoError(t, json.Unmarshal(result, &roundtrip))
	m := roundtrip.(map[string]interface{})
	assert.Equal(t, `value with "quotes" and \backslash`, m["key"])
}

func TestMarshalOrderedJSONModifiedArrayLength(t *testing.T) {
	// Original has 3 items, crushed has 2 (items removed by crushing).
	original := []byte(`{"data":[{"a":1},{"a":2},{"a":3}]}`)
	value := map[string]interface{}{
		"data": []interface{}{
			map[string]interface{}{"a": 1.0},
			map[string]interface{}{"a": 3.0},
		},
	}

	result, err := marshalOrderedJSON(original, value)
	require.NoError(t, err)
	assert.Equal(t, `{"data":[{"a":1},{"a":3}]}`, string(result))
}

// indexOf returns the position of substr in s, or -1 if not found.
func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
