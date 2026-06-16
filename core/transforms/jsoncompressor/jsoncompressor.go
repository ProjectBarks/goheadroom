package jsoncompressor

import (
	"math"
	"strings"
)

type Config struct {
	ShortValueThreshold int
	EntropyThreshold    float64
	MaxArrayItemsFull   int
	MaxNumberDigits     int
}

func DefaultConfig() Config {
	return Config{
		ShortValueThreshold: 20,
		EntropyThreshold:    0.85,
		MaxArrayItemsFull:   3,
		MaxNumberDigits:     10,
	}
}

type Result struct {
	Compressed string
	KeyCount   int
	TokenCount int
}

type tokenType int

const (
	tokKey tokenType = iota
	tokStringValue
	tokNumber
	tokBoolean
	tokNull
	tokBracket
	tokColon
	tokComma
	tokWhitespace
)

type jsonToken struct {
	text string
	typ  tokenType
}

func Compress(content string, cfg Config) Result {
	tokens := tokenizeJSON(content)

	var sb strings.Builder
	sb.Grow(len(content))

	containerStack := make([]byte, 0, 8)
	arrayItemStack := make([]int, 0, 8)
	keyCount := 0

	for _, tok := range tokens {
		switch tok.typ {
		case tokBracket:
			sb.WriteString(tok.text)
			switch tok.text {
			case "{":
				containerStack = append(containerStack, '{')
			case "[":
				containerStack = append(containerStack, '[')
				arrayItemStack = append(arrayItemStack, 0)
			case "}":
				if len(containerStack) > 0 {
					containerStack = containerStack[:len(containerStack)-1]
				}
			case "]":
				if len(containerStack) > 0 {
					containerStack = containerStack[:len(containerStack)-1]
				}
				if len(arrayItemStack) > 0 {
					arrayItemStack = arrayItemStack[:len(arrayItemStack)-1]
				}
			}
		case tokComma:
			sb.WriteString(tok.text)
			if len(containerStack) > 0 && containerStack[len(containerStack)-1] == '[' && len(arrayItemStack) > 0 {
				arrayItemStack[len(arrayItemStack)-1]++
			}
		case tokKey:
			keyCount++
			sb.WriteString(tok.text)
		case tokStringValue:
			inArray := len(arrayItemStack) > 0
			arrayIdx := 0
			if inArray {
				arrayIdx = arrayItemStack[len(arrayItemStack)-1]
			}
			if inArray && arrayIdx >= cfg.MaxArrayItemsFull {
				continue
			}
			value := tok.text
			if len(value) >= 2 {
				value = value[1 : len(value)-1]
			}
			if len(value) <= cfg.ShortValueThreshold {
				sb.WriteString(tok.text)
				continue
			}
			// Preserve high-entropy identifiers (e.g. UUIDs, hashes) that
			// contain non-alphabetic characters — pure-alpha strings are prose.
			if cfg.EntropyThreshold > 0 && !strings.Contains(value, " ") && looksLikeIdentifier(value) {
				if normalizedEntropy(value) >= cfg.EntropyThreshold {
					sb.WriteString(tok.text)
					continue
				}
			}
			// Elide: emit nothing (matches Python mask-based behavior)
		case tokNumber:
			if len(tok.text) <= cfg.MaxNumberDigits {
				sb.WriteString(tok.text)
			} else {
				sb.WriteString("0")
			}
		default:
			if tok.typ != tokWhitespace {
				sb.WriteString(tok.text)
			}
		}
	}

	return Result{
		Compressed: sb.String(),
		KeyCount:   keyCount,
		TokenCount: len(tokens),
	}
}

// looksLikeIdentifier returns true if s contains at least one non-alphabetic
// character (digit, hyphen, underscore, etc.), indicating it may be a UUID,
// hash, or other structured identifier rather than plain prose.
func looksLikeIdentifier(s string) bool {
	for _, r := range s {
		if r < 'A' || (r > 'Z' && r < 'a') || r > 'z' {
			return true
		}
	}
	return false
}

func normalizedEntropy(s string) float64 {
	if len(s) == 0 {
		return 0
	}
	freq := make(map[rune]int, 64)
	n := 0
	for _, r := range s {
		freq[r]++
		n++
	}
	if len(freq) <= 1 {
		return 0
	}
	fn := float64(n)
	var h float64
	for _, count := range freq {
		p := float64(count) / fn
		h -= p * math.Log2(p)
	}
	return h / math.Log2(float64(len(freq)))
}

func tokenizeJSON(content string) []jsonToken {
	tokens := make([]jsonToken, 0, len(content)/4)
	i, n := 0, len(content)
	expectKey := false
	braceStack := make([]byte, 0, 8)

	for i < n {
		c := content[i]

		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			start := i
			for i < n && (content[i] == ' ' || content[i] == '\t' || content[i] == '\n' || content[i] == '\r') {
				i++
			}
			tokens = append(tokens, jsonToken{content[start:i], tokWhitespace})
			continue
		}

		if c == '{' || c == '}' || c == '[' || c == ']' {
			tokens = append(tokens, jsonToken{content[i : i+1], tokBracket})
			switch c {
			case '{':
				braceStack = append(braceStack, '{')
				expectKey = true
			case '}':
				if len(braceStack) > 0 {
					braceStack = braceStack[:len(braceStack)-1]
				}
				expectKey = false
			case '[':
				braceStack = append(braceStack, '[')
				expectKey = false
			case ']':
				if len(braceStack) > 0 {
					braceStack = braceStack[:len(braceStack)-1]
				}
			}
			i++
			continue
		}

		if c == ':' {
			tokens = append(tokens, jsonToken{":", tokColon})
			expectKey = false
			i++
			continue
		}

		if c == ',' {
			tokens = append(tokens, jsonToken{",", tokComma})
			if len(braceStack) > 0 && braceStack[len(braceStack)-1] == '{' {
				expectKey = true
			}
			i++
			continue
		}

		if c == '"' {
			start := i
			i++
			for i < n && content[i] != '"' {
				if content[i] == '\\' && i+1 < n {
					i += 2
				} else {
					i++
				}
			}
			if i < n {
				i++
			}
			text := content[start:i]
			j := i
			for j < n && (content[j] == ' ' || content[j] == '\t') {
				j++
			}
			if j < n && content[j] == ':' && expectKey {
				tokens = append(tokens, jsonToken{text, tokKey})
				expectKey = false
			} else {
				tokens = append(tokens, jsonToken{text, tokStringValue})
			}
			continue
		}

		if c == '-' || (c >= '0' && c <= '9') {
			start := i
			if c == '-' {
				i++
			}
			for i < n && content[i] >= '0' && content[i] <= '9' {
				i++
			}
			if i < n && content[i] == '.' {
				i++
				for i < n && content[i] >= '0' && content[i] <= '9' {
					i++
				}
			}
			if i < n && (content[i] == 'e' || content[i] == 'E') {
				i++
				if i < n && (content[i] == '+' || content[i] == '-') {
					i++
				}
				for i < n && content[i] >= '0' && content[i] <= '9' {
					i++
				}
			}
			tokens = append(tokens, jsonToken{content[start:i], tokNumber})
			continue
		}

		switch {
		case i+4 <= n && content[i:i+4] == "true":
			tokens = append(tokens, jsonToken{"true", tokBoolean})
			i += 4
		case i+5 <= n && content[i:i+5] == "false":
			tokens = append(tokens, jsonToken{"false", tokBoolean})
			i += 5
		case i+4 <= n && content[i:i+4] == "null":
			tokens = append(tokens, jsonToken{"null", tokNull})
			i += 4
		default:
			i++
		}
	}
	return tokens
}
