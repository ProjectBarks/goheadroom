package smartcrusher

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"unicode/utf8"
)

// marshalOrderedJSON serializes value as JSON, preserving the key order from originalJSON.
// Keys present in value but not in originalJSON are appended at the end.
// Keys in originalJSON but not in value are skipped.
// For non-object types and arrays, it recurses to handle nested objects.
func marshalOrderedJSON(originalJSON []byte, value interface{}) ([]byte, error) {
	// Extract the key-order tree from the original JSON using lightweight scanner.
	orderTree, err := extractKeyOrder(originalJSON)
	if err != nil {
		// Fall back to standard marshal if we can't parse the original.
		return json.Marshal(value)
	}
	// Pre-allocate buffer with capacity matching original JSON size.
	buf := bytes.NewBuffer(make([]byte, 0, len(originalJSON)))
	if err := writeOrdered(buf, value, orderTree); err != nil {
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

// extractKeyOrder parses originalJSON using a lightweight byte-level scanner
// and builds a tree of key orders without the overhead of json.Decoder token allocation.
func extractKeyOrder(data []byte) (*keyOrderNode, error) {
	s := &jsonScanner{data: data, pos: 0}
	node, err := s.parseNode()
	if err != nil {
		return nil, err
	}
	return node, nil
}

type rawBackedArray struct {
	items    []interface{}
	rawItems []json.RawMessage
}

func unmarshalPreservingArrays(data []byte) (interface{}, error) {
	s := &jsonValueScanner{data: data, pos: 0}
	s.skipWhitespace()
	if s.pos >= len(s.data) {
		return nil, fmt.Errorf("unexpected end of JSON")
	}
	val, err := s.parseValue()
	if err != nil {
		return nil, err
	}
	s.skipWhitespace()
	if s.pos < len(s.data) {
		return nil, fmt.Errorf("trailing data at pos %d", s.pos)
	}
	return val, nil
}

type jsonValueScanner struct {
	data []byte
	pos  int
}

func (s *jsonValueScanner) parseValue() (interface{}, error) {
	s.skipWhitespace()
	if s.pos >= len(s.data) {
		return nil, fmt.Errorf("unexpected end of JSON")
	}
	switch s.data[s.pos] {
	case '{':
		return s.parseObject()
	case '[':
		return s.parseArray()
	case '"':
		return s.parseString()
	case 't':
		if s.pos+4 <= len(s.data) && s.data[s.pos+1] == 'r' && s.data[s.pos+2] == 'u' && s.data[s.pos+3] == 'e' {
			s.pos += 4
			return true, nil
		}
		return nil, fmt.Errorf("invalid JSON at pos %d", s.pos)
	case 'f':
		if s.pos+5 <= len(s.data) && s.data[s.pos+1] == 'a' && s.data[s.pos+2] == 'l' && s.data[s.pos+3] == 's' && s.data[s.pos+4] == 'e' {
			s.pos += 5
			return false, nil
		}
		return nil, fmt.Errorf("invalid JSON at pos %d", s.pos)
	case 'n':
		if s.pos+4 <= len(s.data) && s.data[s.pos+1] == 'u' && s.data[s.pos+2] == 'l' && s.data[s.pos+3] == 'l' {
			s.pos += 4
			return nil, nil
		}
		return nil, fmt.Errorf("invalid JSON at pos %d", s.pos)
	default:
		return s.parseNumber()
	}
}

func (s *jsonValueScanner) parseObject() (map[string]interface{}, error) {
	s.pos++
	s.skipWhitespace()
	m := make(map[string]interface{})
	if s.pos < len(s.data) && s.data[s.pos] == '}' {
		s.pos++
		return m, nil
	}
	for {
		s.skipWhitespace()
		if s.pos >= len(s.data) || s.data[s.pos] != '"' {
			return nil, fmt.Errorf("expected string key at pos %d", s.pos)
		}
		key, err := s.parseString()
		if err != nil {
			return nil, err
		}
		s.skipWhitespace()
		if s.pos >= len(s.data) || s.data[s.pos] != ':' {
			return nil, fmt.Errorf("expected ':' at pos %d", s.pos)
		}
		s.pos++
		val, err := s.parseValue()
		if err != nil {
			return nil, err
		}
		m[key] = val
		s.skipWhitespace()
		if s.pos >= len(s.data) {
			return nil, fmt.Errorf("unexpected end of object")
		}
		if s.data[s.pos] == '}' {
			s.pos++
			return m, nil
		}
		if s.data[s.pos] == ',' {
			s.pos++
			continue
		}
		return nil, fmt.Errorf("expected ',' or '}' at pos %d", s.pos)
	}
}

func (s *jsonValueScanner) parseArray() (interface{}, error) {
	s.pos++
	s.skipWhitespace()
	if s.pos < len(s.data) && s.data[s.pos] == ']' {
		s.pos++
		return &rawBackedArray{}, nil
	}
	var items []interface{}
	var rawItems []json.RawMessage
	for {
		s.skipWhitespace()
		start := s.pos
		val, err := s.parseValue()
		if err != nil {
			return nil, err
		}
		end := s.pos
		raw := make([]byte, end-start)
		copy(raw, s.data[start:end])
		items = append(items, val)
		rawItems = append(rawItems, json.RawMessage(raw))
		s.skipWhitespace()
		if s.pos >= len(s.data) {
			return nil, fmt.Errorf("unexpected end of array")
		}
		if s.data[s.pos] == ']' {
			s.pos++
			return &rawBackedArray{items: items, rawItems: rawItems}, nil
		}
		if s.data[s.pos] == ',' {
			s.pos++
			continue
		}
		return nil, fmt.Errorf("expected ',' or ']' at pos %d", s.pos)
	}
}

func (s *jsonValueScanner) parseString() (string, error) {
	if s.pos >= len(s.data) || s.data[s.pos] != '"' {
		return "", fmt.Errorf("expected '\"' at pos %d", s.pos)
	}
	s.pos++
	start := s.pos
	hasEscape := false
	for s.pos < len(s.data) {
		c := s.data[s.pos]
		if c == '\\' {
			hasEscape = true
			s.pos++
			if s.pos >= len(s.data) {
				return "", fmt.Errorf("unexpected end of string escape")
			}
			if s.data[s.pos] == 'u' {
				s.pos += 4
				if s.pos > len(s.data) {
					return "", fmt.Errorf("unexpected end of unicode escape")
				}
			} else {
				s.pos++
			}
			continue
		}
		if c == '"' {
			raw := s.data[start:s.pos]
			s.pos++
			if !hasEscape {
				return string(raw), nil
			}
			quoted := s.data[start-1 : s.pos]
			var result string
			if err := json.Unmarshal(quoted, &result); err != nil {
				return "", err
			}
			return result, nil
		}
		s.pos++
	}
	return "", fmt.Errorf("unterminated string")
}

func (s *jsonValueScanner) parseNumber() (float64, error) {
	start := s.pos
	for s.pos < len(s.data) {
		c := s.data[s.pos]
		if c == '-' || c == '+' || c == '.' || c == 'e' || c == 'E' || (c >= '0' && c <= '9') {
			s.pos++
			continue
		}
		break
	}
	f, err := strconv.ParseFloat(string(s.data[start:s.pos]), 64)
	if err != nil {
		return 0, err
	}
	return f, nil
}

func (s *jsonValueScanner) skipWhitespace() {
	for s.pos < len(s.data) {
		c := s.data[s.pos]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			s.pos++
			continue
		}
		break
	}
}

// jsonScanner is a lightweight JSON scanner that extracts key order
// without allocating json.Token values. It only needs to identify
// object keys (strings) and skip over values to find structure.
type jsonScanner struct {
	data []byte
	pos  int
}

func (s *jsonScanner) parseNode() (*keyOrderNode, error) {
	s.skipWhitespace()
	if s.pos >= len(s.data) {
		return nil, fmt.Errorf("unexpected end of JSON")
	}

	switch s.data[s.pos] {
	case '{':
		return s.parseObject()
	case '[':
		return s.parseArray()
	default:
		// Scalar value -- skip it entirely, no ordering info needed.
		// Return nil instead of allocating an empty keyOrderNode.
		if err := s.skipValue(); err != nil {
			return nil, err
		}
		return nil, nil
	}
}

func (s *jsonScanner) parseObject() (*keyOrderNode, error) {
	s.pos++ // consume '{'
	s.skipWhitespace()

	if s.pos < len(s.data) && s.data[s.pos] == '}' {
		s.pos++ // consume '}'
		return &keyOrderNode{}, nil
	}

	// Lazily allocate with small initial capacity; most objects have < 8 keys.
	node := &keyOrderNode{
		keys:     make([]string, 0, 8),
		children: make(map[string]*keyOrderNode, 8),
	}

	for {
		s.skipWhitespace()
		if s.pos >= len(s.data) {
			return nil, fmt.Errorf("unexpected end of JSON in object")
		}

		// Parse key (must be a string).
		if s.data[s.pos] != '"' {
			return nil, fmt.Errorf("expected '\"' for object key at pos %d, got %c", s.pos, s.data[s.pos])
		}
		key, err := s.parseString()
		if err != nil {
			return nil, err
		}

		s.skipWhitespace()
		if s.pos >= len(s.data) || s.data[s.pos] != ':' {
			return nil, fmt.Errorf("expected ':' after object key at pos %d", s.pos)
		}
		s.pos++ // consume ':'

		// Parse value.
		child, err := s.parseNode()
		if err != nil {
			return nil, err
		}
		node.keys = append(node.keys, key)
		if child != nil {
			node.children[key] = child
		}

		s.skipWhitespace()
		if s.pos >= len(s.data) {
			return nil, fmt.Errorf("unexpected end of JSON in object")
		}
		if s.data[s.pos] == '}' {
			s.pos++
			return node, nil
		}
		if s.data[s.pos] == ',' {
			s.pos++
			continue
		}
		return nil, fmt.Errorf("expected ',' or '}' in object at pos %d", s.pos)
	}
}

func (s *jsonScanner) parseArray() (*keyOrderNode, error) {
	s.pos++ // consume '['
	s.skipWhitespace()

	node := &keyOrderNode{}

	if s.pos < len(s.data) && s.data[s.pos] == ']' {
		s.pos++ // consume ']'
		return node, nil
	}

	// Parse only the first element fully (to capture representative key order).
	// Skip remaining elements since array items after crushing won't match
	// original indices anyway, and homogeneous arrays share the same key order.
	first := true
	for {
		if first {
			child, err := s.parseNode()
			if err != nil {
				return nil, err
			}
			node.items = append(node.items, child)
			first = false
		} else {
			// Skip subsequent elements without building key-order nodes.
			s.skipWhitespace()
			if s.pos >= len(s.data) {
				return nil, fmt.Errorf("unexpected end of JSON in array")
			}
			switch s.data[s.pos] {
			case '{':
				if err := s.skipObject(); err != nil {
					return nil, err
				}
			case '[':
				if err := s.skipArray(); err != nil {
					return nil, err
				}
			default:
				if err := s.skipValue(); err != nil {
					return nil, err
				}
			}
		}

		s.skipWhitespace()
		if s.pos >= len(s.data) {
			return nil, fmt.Errorf("unexpected end of JSON in array")
		}
		if s.data[s.pos] == ']' {
			s.pos++
			return node, nil
		}
		if s.data[s.pos] == ',' {
			s.pos++
			continue
		}
		return nil, fmt.Errorf("expected ',' or ']' in array at pos %d", s.pos)
	}
}

// parseString parses a JSON string and returns the unescaped content.
// This avoids json.Decoder's token allocation overhead.
func (s *jsonScanner) parseString() (string, error) {
	if s.pos >= len(s.data) || s.data[s.pos] != '"' {
		return "", fmt.Errorf("expected '\"' at pos %d", s.pos)
	}
	s.pos++ // consume opening '"'

	start := s.pos
	hasEscape := false

	for s.pos < len(s.data) {
		c := s.data[s.pos]
		if c == '\\' {
			hasEscape = true
			s.pos++ // skip the backslash
			if s.pos >= len(s.data) {
				return "", fmt.Errorf("unexpected end of JSON in string escape")
			}
			if s.data[s.pos] == 'u' {
				// \uXXXX -- skip 4 hex digits
				s.pos += 4
				if s.pos > len(s.data) {
					return "", fmt.Errorf("unexpected end of JSON in unicode escape")
				}
			} else {
				s.pos++ // skip escaped char
			}
			continue
		}
		if c == '"' {
			// End of string.
			raw := s.data[start:s.pos]
			s.pos++ // consume closing '"'
			if !hasEscape {
				// Fast path: no escapes, return string backed by the original
				// byte slice without copying. The data slice lives for the
				// duration of the marshal call, so this is safe.
				return string(raw), nil
			}
			// Slow path: unescape using json.Unmarshal on the quoted string.
			quoted := s.data[start-1 : s.pos] // include quotes
			var result string
			if err := json.Unmarshal(quoted, &result); err != nil {
				return "", err
			}
			return result, nil
		}
		s.pos++
	}
	return "", fmt.Errorf("unterminated string at pos %d", start-1)
}

// skipValue skips over a complete JSON value (string, number, bool, null).
// Objects and arrays are handled by parseObject/parseArray, not here.
func (s *jsonScanner) skipValue() error {
	s.skipWhitespace()
	if s.pos >= len(s.data) {
		return fmt.Errorf("unexpected end of JSON")
	}

	switch s.data[s.pos] {
	case '"':
		return s.skipString()
	case '{':
		return s.skipObject()
	case '[':
		return s.skipArray()
	case 't': // true
		if s.pos+4 <= len(s.data) {
			s.pos += 4
			return nil
		}
		return fmt.Errorf("invalid JSON at pos %d", s.pos)
	case 'f': // false
		if s.pos+5 <= len(s.data) {
			s.pos += 5
			return nil
		}
		return fmt.Errorf("invalid JSON at pos %d", s.pos)
	case 'n': // null
		if s.pos+4 <= len(s.data) {
			s.pos += 4
			return nil
		}
		return fmt.Errorf("invalid JSON at pos %d", s.pos)
	default:
		// Number -- skip digits, -, +, ., e, E
		return s.skipNumber()
	}
}

func (s *jsonScanner) skipString() error {
	s.pos++ // consume opening '"'
	for s.pos < len(s.data) {
		c := s.data[s.pos]
		if c == '\\' {
			s.pos += 2 // skip backslash + escaped char
			continue
		}
		if c == '"' {
			s.pos++ // consume closing '"'
			return nil
		}
		s.pos++
	}
	return fmt.Errorf("unterminated string")
}

func (s *jsonScanner) skipNumber() error {
	for s.pos < len(s.data) {
		c := s.data[s.pos]
		if c == '-' || c == '+' || c == '.' || c == 'e' || c == 'E' || (c >= '0' && c <= '9') {
			s.pos++
			continue
		}
		break
	}
	return nil
}

func (s *jsonScanner) skipObject() error {
	s.pos++ // consume '{'
	s.skipWhitespace()
	if s.pos < len(s.data) && s.data[s.pos] == '}' {
		s.pos++
		return nil
	}
	for {
		s.skipWhitespace()
		// Skip key
		if err := s.skipString(); err != nil {
			return err
		}
		s.skipWhitespace()
		if s.pos >= len(s.data) || s.data[s.pos] != ':' {
			return fmt.Errorf("expected ':' in object")
		}
		s.pos++ // consume ':'
		// Skip value
		s.skipWhitespace()
		if s.pos >= len(s.data) {
			return fmt.Errorf("unexpected end of object")
		}
		switch s.data[s.pos] {
		case '{':
			if err := s.skipObject(); err != nil {
				return err
			}
		case '[':
			if err := s.skipArray(); err != nil {
				return err
			}
		default:
			if err := s.skipValue(); err != nil {
				return err
			}
		}
		s.skipWhitespace()
		if s.pos >= len(s.data) {
			return fmt.Errorf("unexpected end of object")
		}
		if s.data[s.pos] == '}' {
			s.pos++
			return nil
		}
		if s.data[s.pos] == ',' {
			s.pos++
			continue
		}
		return fmt.Errorf("expected ',' or '}' in object")
	}
}

func (s *jsonScanner) skipArray() error {
	s.pos++ // consume '['
	s.skipWhitespace()
	if s.pos < len(s.data) && s.data[s.pos] == ']' {
		s.pos++
		return nil
	}
	for {
		s.skipWhitespace()
		if s.pos >= len(s.data) {
			return fmt.Errorf("unexpected end of array")
		}
		switch s.data[s.pos] {
		case '{':
			if err := s.skipObject(); err != nil {
				return err
			}
		case '[':
			if err := s.skipArray(); err != nil {
				return err
			}
		default:
			if err := s.skipValue(); err != nil {
				return err
			}
		}
		s.skipWhitespace()
		if s.pos >= len(s.data) {
			return fmt.Errorf("unexpected end of array")
		}
		if s.data[s.pos] == ']' {
			s.pos++
			return nil
		}
		if s.data[s.pos] == ',' {
			s.pos++
			continue
		}
		return fmt.Errorf("expected ',' or ']' in array")
	}
}

func (s *jsonScanner) skipWhitespace() {
	for s.pos < len(s.data) {
		c := s.data[s.pos]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			s.pos++
			continue
		}
		break
	}
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

	case *rawBackedArray:
		return writeOrderedArray(buf, v.items, orderNode)

	case string:
		writeJSONString(buf, v)
		return nil

	case float64:
		// Format numbers directly to avoid json.Marshal allocation.
		writeFloat64(buf, v)
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

// writeJSONString writes a JSON-encoded string to buf without using json.Marshal.
// This avoids the allocation overhead of json.Marshal for string values.
func writeJSONString(buf *bytes.Buffer, s string) {
	buf.WriteByte('"')
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == '"':
			buf.WriteString(`\"`)
			i++
		case c == '\\':
			buf.WriteString(`\\`)
			i++
		case c == '\n':
			buf.WriteString(`\n`)
			i++
		case c == '\r':
			buf.WriteString(`\r`)
			i++
		case c == '\t':
			buf.WriteString(`\t`)
			i++
		case c < 0x20:
			// Control characters use \u00XX.
			buf.WriteString(`\u00`)
			buf.WriteByte(hexDigit(c >> 4))
			buf.WriteByte(hexDigit(c & 0x0f))
			i++
		case c < utf8.RuneSelf:
			// ASCII, safe to write directly.
			buf.WriteByte(c)
			i++
		default:
			// Multi-byte UTF-8: write the rune bytes directly (valid JSON).
			r, size := utf8.DecodeRuneInString(s[i:])
			if r == utf8.RuneError && size == 1 {
				// Invalid UTF-8 byte, escape it.
				buf.WriteString(`�`)
				i++
			} else {
				buf.WriteString(s[i : i+size])
				i += size
			}
		}
	}
	buf.WriteByte('"')
}

func hexDigit(b byte) byte {
	if b < 10 {
		return '0' + b
	}
	return 'a' + b - 10
}

// writeFloat64 formats a float64 into buf matching Go's json.Marshal behavior
// without allocating. Uses strconv.AppendFloat with the same format as encoding/json.
func writeFloat64(buf *bytes.Buffer, f float64) {
	if math.IsInf(f, 0) || math.IsNaN(f) {
		// json.Marshal would error; write 0 as fallback.
		buf.WriteByte('0')
		return
	}
	// encoding/json uses 'f' format with -1 precision for numbers that
	// round-trip exactly, otherwise 'e' format. We use the same logic
	// via strconv.AppendFloat with 'f' at -1 precision, then fallback.
	abs := math.Abs(f)
	format := byte('f')
	if abs != 0 && (abs < 1e-6 || abs >= 1e21) {
		format = byte('e')
	}
	b := strconv.AppendFloat(buf.AvailableBuffer(), f, format, -1, 64)
	buf.Write(b)
}

// writeOrderedMap writes a map as a JSON object with keys in the order specified by orderNode.
func writeOrderedMap(buf *bytes.Buffer, m map[string]interface{}, orderNode *keyOrderNode) error {
	buf.WriteByte('{')

	first := true

	if orderNode != nil && len(orderNode.keys) > 0 {
		// Count how many of the map's keys appear in the order node.
		// If all of them do, we can skip tracking which keys were written.
		orderedCount := 0
		for _, key := range orderNode.keys {
			if _, exists := m[key]; exists {
				orderedCount++
			}
		}

		if orderedCount == len(m) {
			// Fast path: all keys in m are covered by orderNode.keys.
			for _, key := range orderNode.keys {
				val, exists := m[key]
				if !exists {
					continue
				}
				if !first {
					buf.WriteByte(',')
				}
				first = false
				writeJSONString(buf, key)
				buf.WriteByte(':')
				childNode := orderNode.children[key]
				if err := writeOrdered(buf, val, childNode); err != nil {
					return err
				}
			}
		} else {
			// Slow path: map has keys not in the order node.
			written := make(map[string]bool, len(m))
			for _, key := range orderNode.keys {
				val, exists := m[key]
				if !exists {
					continue
				}
				if !first {
					buf.WriteByte(',')
				}
				first = false
				writeJSONString(buf, key)
				buf.WriteByte(':')
				childNode := orderNode.children[key]
				if err := writeOrdered(buf, val, childNode); err != nil {
					return err
				}
				written[key] = true
			}
			// Append extra keys not in the original order.
			for key, val := range m {
				if written[key] {
					continue
				}
				if !first {
					buf.WriteByte(',')
				}
				first = false
				writeJSONString(buf, key)
				buf.WriteByte(':')
				if err := writeOrdered(buf, val, nil); err != nil {
					return err
				}
			}
		}
	} else {
		// No order info -- write all keys.
		for key, val := range m {
			if !first {
				buf.WriteByte(',')
			}
			first = false
			writeJSONString(buf, key)
			buf.WriteByte(':')
			if err := writeOrdered(buf, val, nil); err != nil {
				return err
			}
		}
	}

	buf.WriteByte('}')
	return nil
}

// writeOrderedArray writes a slice as a JSON array, recursing into elements
// with the corresponding order nodes. Since the scanner only stores a
// representative (first) element's key order, all items reuse items[0].
func writeOrderedArray(buf *bytes.Buffer, arr []interface{}, orderNode *keyOrderNode) error {
	buf.WriteByte('[')
	// Use the first (representative) item's key order for all elements.
	var repNode *keyOrderNode
	if orderNode != nil && len(orderNode.items) > 0 {
		repNode = orderNode.items[0]
	}
	for i, item := range arr {
		if i > 0 {
			buf.WriteByte(',')
		}
		if err := writeOrdered(buf, item, repNode); err != nil {
			return err
		}
	}
	buf.WriteByte(']')
	return nil
}
