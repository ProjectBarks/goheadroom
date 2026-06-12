package smartcrusher

import (
	"bytes"
	"encoding/json"
)

// ArrayType classifies JSON array element types.
type ArrayType int

const (
	ArrayDictArray   ArrayType = iota
	ArrayStringArray
	ArrayNumberArray
	ArrayBoolArray
	ArrayNestedArray
	ArrayMixedArray
	ArrayEmpty
)

func (a ArrayType) String() string {
	switch a {
	case ArrayDictArray:
		return "dict_array"
	case ArrayStringArray:
		return "string_array"
	case ArrayNumberArray:
		return "number_array"
	case ArrayBoolArray:
		return "bool_array"
	case ArrayNestedArray:
		return "nested_array"
	case ArrayMixedArray:
		return "mixed_array"
	case ArrayEmpty:
		return "empty"
	default:
		return "unknown"
	}
}

// ClassifyArray classifies a JSON array by its element types.
func ClassifyArray(items []json.RawMessage) ArrayType {
	if len(items) == 0 {
		return ArrayEmpty
	}

	var hasBool, hasNumber, hasString, hasObject, hasArray, hasNull bool

	for _, raw := range items {
		trimmed := bytes.TrimSpace(raw)
		if len(trimmed) == 0 {
			continue
		}
		switch trimmed[0] {
		case '{':
			hasObject = true
		case '[':
			hasArray = true
		case '"':
			hasString = true
		case 't', 'f':
			// Check if it's a boolean (true/false)
			if bytes.Equal(trimmed, []byte("true")) || bytes.Equal(trimmed, []byte("false")) {
				hasBool = true
			} else {
				hasString = true // edge case, shouldn't happen with valid JSON
			}
		case 'n':
			hasNull = true
		default:
			// Number (digit, minus sign, etc.)
			hasNumber = true
		}
	}

	// Pure bool array.
	if hasBool && !hasNumber && !hasString && !hasObject && !hasArray && !hasNull {
		return ArrayBoolArray
	}
	// Pure dict array.
	if hasObject && !hasBool && !hasNumber && !hasString && !hasArray && !hasNull {
		return ArrayDictArray
	}
	// Pure string array.
	if hasString && !hasBool && !hasNumber && !hasObject && !hasArray && !hasNull {
		return ArrayStringArray
	}
	// Pure number array.
	if hasNumber && !hasBool && !hasString && !hasObject && !hasArray && !hasNull {
		return ArrayNumberArray
	}
	// Pure nested array.
	if hasArray && !hasBool && !hasNumber && !hasString && !hasObject && !hasNull {
		return ArrayNestedArray
	}

	return ArrayMixedArray
}
