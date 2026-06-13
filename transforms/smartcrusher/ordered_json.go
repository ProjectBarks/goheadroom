package smartcrusher

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// marshalOrderedJSON serializes value as JSON, preserving the key order from originalJSON.
// Keys present in value but not in originalJSON are appended at the end.
// Keys in originalJSON but not in value are skipped.
// For non-object types and arrays, it recurses to handle nested objects.
func marshalOrderedJSON(originalJSON []byte, value interface{}) ([]byte, error) {
	// Extract the key-order tree from the original JSON.
	orderTree, err := extractKeyOrder(originalJSON)
	if err != nil {
		// Fall back to standard marshal if we can't parse the original.
		return json.Marshal(value)
	}
	var buf bytes.Buffer
	if err := writeOrdered(&buf, value, orderTree); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// keyOrderNode stores the key order for an object and recursively for nested values.
type keyOrderNode struct {
	// keys stores the ordered keys for this object (only meaningful if this is an object).
	keys []string
	// children maps each key to the order tree of its value.
	children map[string]*keyOrderNode
	// items stores order trees for array elements (by index).
	items []*keyOrderNode
}

// extractKeyOrder parses originalJSON token-by-token and builds a tree of key orders.
func extractKeyOrder(data []byte) (*keyOrderNode, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	node, err := parseOrderNode(dec)
	if err != nil {
		return nil, err
	}
	return node, nil
}

func parseOrderNode(dec *json.Decoder) (*keyOrderNode, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}

	switch v := tok.(type) {
	case json.Delim:
		switch v {
		case '{':
			node := &keyOrderNode{
				children: make(map[string]*keyOrderNode),
			}
			for dec.More() {
				// Next token is a key.
				keyTok, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key, ok := keyTok.(string)
				if !ok {
					return nil, fmt.Errorf("expected string key, got %T", keyTok)
				}
				node.keys = append(node.keys, key)
				// Parse the value.
				child, err := parseOrderNode(dec)
				if err != nil {
					return nil, err
				}
				node.children[key] = child
			}
			// Consume closing '}'.
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return node, nil

		case '[':
			node := &keyOrderNode{}
			for dec.More() {
				child, err := parseOrderNode(dec)
				if err != nil {
					return nil, err
				}
				node.items = append(node.items, child)
			}
			// Consume closing ']'.
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return node, nil
		}
	}
	// Scalar value - no ordering info needed.
	return &keyOrderNode{}, nil
}

// writeOrdered serializes value into buf using the key order from orderNode.
func writeOrdered(buf *bytes.Buffer, value interface{}, orderNode *keyOrderNode) error {
	if value == nil {
		buf.WriteString("null")
		return nil
	}

	switch v := value.(type) {
	case map[string]interface{}:
		return writeOrderedMap(buf, v, orderNode)

	case []interface{}:
		return writeOrderedArray(buf, v, orderNode)

	case string:
		data, err := json.Marshal(v)
		if err != nil {
			return err
		}
		buf.Write(data)
		return nil

	case float64:
		// Match Go's default json.Marshal behavior for numbers.
		data, err := json.Marshal(v)
		if err != nil {
			return err
		}
		buf.Write(data)
		return nil

	case json.Number:
		buf.WriteString(v.String())
		return nil

	case bool:
		if v {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
		return nil

	case json.RawMessage:
		buf.Write(v)
		return nil

	default:
		// Fallback for other types.
		data, err := json.Marshal(v)
		if err != nil {
			return err
		}
		buf.Write(data)
		return nil
	}
}

// writeOrderedMap writes a map as a JSON object with keys in the order specified by orderNode.
func writeOrderedMap(buf *bytes.Buffer, m map[string]interface{}, orderNode *keyOrderNode) error {
	buf.WriteByte('{')

	// Determine the key order. Start with keys from the original order,
	// then append any keys present in m but not in the original.
	written := make(map[string]bool, len(m))
	first := true

	if orderNode != nil {
		for _, key := range orderNode.keys {
			val, exists := m[key]
			if !exists {
				continue
			}
			if !first {
				buf.WriteByte(',')
			}
			first = false
			// Write key.
			keyJSON, err := json.Marshal(key)
			if err != nil {
				return err
			}
			buf.Write(keyJSON)
			buf.WriteByte(':')

			// Get child order node.
			childNode := orderNode.children[key]
			if err := writeOrdered(buf, val, childNode); err != nil {
				return err
			}
			written[key] = true
		}
	}

	// Append any keys not in the original order (new keys from ProcessValue).
	// Use sorted order for determinism, matching json.Marshal behavior for new keys.
	for key, val := range m {
		if written[key] {
			continue
		}
		if !first {
			buf.WriteByte(',')
		}
		first = false
		keyJSON, err := json.Marshal(key)
		if err != nil {
			return err
		}
		buf.Write(keyJSON)
		buf.WriteByte(':')
		if err := writeOrdered(buf, val, nil); err != nil {
			return err
		}
	}

	buf.WriteByte('}')
	return nil
}

// writeOrderedArray writes a slice as a JSON array, recursing into elements
// with the corresponding order nodes.
func writeOrderedArray(buf *bytes.Buffer, arr []interface{}, orderNode *keyOrderNode) error {
	buf.WriteByte('[')
	for i, item := range arr {
		if i > 0 {
			buf.WriteByte(',')
		}
		var childNode *keyOrderNode
		if orderNode != nil && i < len(orderNode.items) {
			childNode = orderNode.items[i]
		}
		if err := writeOrdered(buf, item, childNode); err != nil {
			return err
		}
	}
	buf.WriteByte(']')
	return nil
}

