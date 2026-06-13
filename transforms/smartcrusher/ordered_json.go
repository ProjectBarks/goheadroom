package smartcrusher

import (
	"bytes"
	"encoding/json"
	"fmt"
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
		if err := s.skipValue(); err != nil {
			return nil, err
		}
		return &keyOrderNode{}, nil
	}
}

func (s *jsonScanner) parseObject() (*keyOrderNode, error) {
	s.pos++ // consume '{'
	s.skipWhitespace()

	node := &keyOrderNode{
		keys:     make([]string, 0, 16), // pre-allocate for typical objects
		children: make(map[string]*keyOrderNode, 16),
	}

	if s.pos < len(s.data) && s.data[s.pos] == '}' {
		s.pos++ // consume '}'
		return node, nil
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
		node.children[key] = child

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

	for {
		child, err := s.parseNode()
		if err != nil {
			return nil, err
		}
		node.items = append(node.items, child)

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
				// Fast path: no escapes, return string directly from buffer.
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

	case string:
		writeJSONString(buf, v)
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
			// Write key using fast string writer.
			writeJSONString(buf, key)
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
		writeJSONString(buf, key)
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
