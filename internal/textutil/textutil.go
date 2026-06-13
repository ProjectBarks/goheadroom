package textutil

import "strings"

func SkipLineWhitespace(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] != ' ' && s[i] != '\t' {
			return i
		}
	}
	return len(s)
}

func IsWordChar(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_'
}

func HasKeywordThenWhitespace(s string, start int, keywords []string) bool {
	rest := s[start:]
	for _, kw := range keywords {
		if len(rest) > len(kw) && rest[:len(kw)] == kw {
			c := rest[len(kw)]
			if c == ' ' || c == '\t' {
				return true
			}
		}
	}
	return false
}

func HasKeywordThenWhitespaceWord(s string, start int, keywords []string) bool {
	rest := s[start:]
	for _, kw := range keywords {
		kwLen := len(kw)
		if len(rest) > kwLen && rest[:kwLen] == kw {
			c := rest[kwLen]
			if c == ' ' || c == '\t' {
				j := kwLen + 1
				for j < len(rest) && (rest[j] == ' ' || rest[j] == '\t') {
					j++
				}
				if j < len(rest) && IsWordChar(rest[j]) {
					return true
				}
			}
		}
	}
	return false
}

func HasLiteralPrefix(s string, start int, prefix string) bool {
	end := start + len(prefix)
	return end <= len(s) && s[start:end] == prefix
}

func matchWordAt(s string, i int, kw string) bool {
	kwLen := len(kw)
	if i+kwLen > len(s) {
		return false
	}
	for j := 1; j < kwLen; j++ {
		c := s[i+j]
		if c >= 'A' && c <= 'Z' {
			c += 32
		}
		if c != kw[j] {
			return false
		}
	}
	end := i + kwLen
	if end < len(s) && IsWordChar(s[end]) {
		return false
	}
	return true
}

func ContainsWordCI(line string, keywords []string) bool {
	if len(keywords) == 0 || len(line) == 0 {
		return false
	}
	var firstChars [26]bool
	for _, kw := range keywords {
		if len(kw) > 0 && kw[0] >= 'a' && kw[0] <= 'z' {
			firstChars[kw[0]-'a'] = true
		}
	}
	for i := 0; i < len(line); i++ {
		c := line[i]
		if c >= 'A' && c <= 'Z' {
			c += 32
		}
		if c < 'a' || c > 'z' {
			continue
		}
		if !firstChars[c-'a'] {
			continue
		}
		if i > 0 && IsWordChar(line[i-1]) {
			continue
		}
		for _, kw := range keywords {
			if kw[0] != c {
				continue
			}
			if matchWordAt(line, i, kw) {
				return true
			}
		}
	}
	return false
}

func ContainsAnyWordCI(s string, keywords []string) bool {
	if len(keywords) == 0 || len(s) == 0 {
		return false
	}
	var firstChars [26]bool
	for _, kw := range keywords {
		if len(kw) > 0 && kw[0] >= 'a' && kw[0] <= 'z' {
			firstChars[kw[0]-'a'] = true
		}
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 32
		}
		if c < 'a' || c > 'z' {
			continue
		}
		if !firstChars[c-'a'] {
			continue
		}
		if i > 0 {
			b := s[i-1]
			if (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '_' {
				continue
			}
		}
		for _, kw := range keywords {
			if kw[0] != c {
				continue
			}
			if matchWordAt(s, i, kw) {
				return true
			}
		}
	}
	return false
}

func IsDigit(c byte) bool {
	return c >= '0' && c <= '9'
}

func ForEachLine(text string, maxLines int, fn func(line string)) {
	remaining := text
	count := 0
	for count < maxLines && len(remaining) > 0 {
		idx := strings.IndexByte(remaining, '\n')
		var line string
		if idx < 0 {
			line = remaining
			remaining = ""
		} else {
			line = remaining[:idx]
			remaining = remaining[idx+1:]
		}
		fn(line)
		count++
	}
}

func IsAllWhitespace(s string) bool {
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case ' ', '\t', '\n', '\r':
			continue
		default:
			return false
		}
	}
	return true
}
