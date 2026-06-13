// Package contentdetector provides content type detection
// for multi-format compression routing.
//
// Direct port of headroom-core/src/transforms/content_detector.rs.
// Code language patterns use hand-rolled string scanning instead of regex
// to avoid O(lines * patterns) regexp.MatchString overhead.
package contentdetector

import (
	"math"
	"regexp"
	"strings"
)

// ContentType enumerates detected content types.
type ContentType int

const (
	PlainText ContentType = iota
	JsonArray
	SourceCode
	SearchResults
	BuildOutput
	GitDiff
	Html
)

// String returns the stable string tag matching Python's ContentType values.
func (ct ContentType) String() string {
	switch ct {
	case JsonArray:
		return "json_array"
	case SourceCode:
		return "source_code"
	case SearchResults:
		return "search"
	case BuildOutput:
		return "build"
	case GitDiff:
		return "diff"
	case Html:
		return "html"
	case PlainText:
		return "text"
	default:
		return "text"
	}
}

// DetectionResult holds the detected type and confidence score.
type DetectionResult struct {
	ContentType ContentType
	Confidence  float64
	Metadata    map[string]interface{}
}

func plainTextResult(confidence float64) DetectionResult {
	return DetectionResult{ContentType: PlainText, Confidence: confidence}
}

// Regex patterns (compiled once) - kept only for complex patterns not worth hand-rolling.
var (
	diffHeaderPattern = regexp.MustCompile(`^(diff --git|diff --combined |diff --cc |--- a/|@@\s+-\d+,\d+\s+\+\d+,\d+\s+@@|@@@+\s+-\d+(?:,\d+)?\s+(?:-\d+(?:,\d+)?\s+)+\+\d+(?:,\d+)?\s+@@@+)`)
)

// matchesSearchResult replaces searchResultPattern `^[^\s:]+:\d+:`
// Scans for non-space non-colon chars, then ':', then digits, then ':'.
func matchesSearchResult(line string) bool {
	i := 0
	for i < len(line) && line[i] != ':' && line[i] != ' ' && line[i] != '\t' && line[i] != '\n' && line[i] != '\r' {
		i++
	}
	if i == 0 || i >= len(line) || line[i] != ':' {
		return false
	}
	i++ // skip ':'
	if i >= len(line) || line[i] < '0' || line[i] > '9' {
		return false
	}
	for i < len(line) && line[i] >= '0' && line[i] <= '9' {
		i++
	}
	return i < len(line) && line[i] == ':'
}

// matchesDiffChange replaces diffChangePattern `^[+-][^+-]`
func matchesDiffChange(line string) bool {
	return len(line) >= 2 && (line[0] == '+' || line[0] == '-') && line[1] != '+' && line[1] != '-'
}

// ---------------------------------------------------------------------------
// HTML detection helpers (replace regex for small-sample HTML detection)
// ---------------------------------------------------------------------------

// toLowerASCII returns the ASCII-lowercase of c.
func toLowerASCII(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + 32
	}
	return c
}

// matchesHTMLDoctype replaces htmlDoctypePattern `(?i)^\s*<!doctype\s+html`
func matchesHTMLDoctype(s string) bool {
	i := skipLineWhitespace(s)
	const prefix = "<!doctype"
	if i+len(prefix) > len(s) {
		return false
	}
	for j := 0; j < len(prefix); j++ {
		if toLowerASCII(s[i+j]) != prefix[j] {
			return false
		}
	}
	k := i + len(prefix)
	if k >= len(s) || (s[k] != ' ' && s[k] != '\t') {
		return false
	}
	// skip whitespace then check "html"
	for k < len(s) && (s[k] == ' ' || s[k] == '\t') {
		k++
	}
	return k+4 <= len(s) &&
		toLowerASCII(s[k]) == 'h' &&
		toLowerASCII(s[k+1]) == 't' &&
		toLowerASCII(s[k+2]) == 'm' &&
		toLowerASCII(s[k+3]) == 'l'
}

// containsHTMLTagCI searches for `<tag` followed by whitespace or '>' (case-insensitive).
func containsHTMLTagCI(s string, tag string) bool {
	// tag is pre-lowered
	limit := len(s) - len(tag) - 1 // need at least '<' + tag
	for i := 0; i <= limit; i++ {
		if s[i] != '<' {
			continue
		}
		match := true
		for j := 0; j < len(tag); j++ {
			if toLowerASCII(s[i+1+j]) != tag[j] {
				match = false
				break
			}
		}
		if match {
			next := i + 1 + len(tag)
			if next < len(s) && (s[next] == ' ' || s[next] == '\t' || s[next] == '>' || s[next] == '\n' || s[next] == '/') {
				return true
			}
		}
	}
	return false
}

// countHTMLStructuralTags replaces htmlStructuralPattern FindAllStringIndex.
// Counts occurrences of structural HTML tags (case-insensitive).
func countHTMLStructuralTags(s string) uint32 {
	tags := []string{"div", "span", "script", "style", "link", "meta", "nav", "header", "footer", "aside", "article", "section", "main"}
	var count uint32
	for i := 0; i < len(s)-1; i++ {
		if s[i] != '<' {
			continue
		}
		for _, tag := range tags {
			end := i + 1 + len(tag)
			if end >= len(s) {
				continue
			}
			match := true
			for j := 0; j < len(tag); j++ {
				if toLowerASCII(s[i+1+j]) != tag[j] {
					match = false
					break
				}
			}
			if match {
				c := s[end]
				if c == ' ' || c == '\t' || c == '>' || c == '\n' || c == '/' {
					count++
					break // only count one tag per '<' position
				}
			}
		}
	}
	return count
}

// ---------------------------------------------------------------------------
// String-scanning helpers (replace regex for hot-path code detection)
// ---------------------------------------------------------------------------

// skipLineWhitespace returns the index of the first non-space/tab byte.
func skipLineWhitespace(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] != ' ' && s[i] != '\t' {
			return i
		}
	}
	return len(s)
}

// isWordChar returns true for [a-zA-Z0-9_].
func isWordChar(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_'
}

// hasKeywordThenWhitespace checks if s[start:] begins with any keyword
// followed by at least one space/tab. Matches ^\s*(kw1|kw2)\s+
func hasKeywordThenWhitespace(s string, start int, keywords []string) bool {
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

// hasKeywordThenWhitespaceWord checks if s[start:] begins with any keyword
// followed by whitespace then a word char. Matches ^\s*(kw1|kw2)\s+\w+
func hasKeywordThenWhitespaceWord(s string, start int, keywords []string) bool {
	rest := s[start:]
	for _, kw := range keywords {
		kwLen := len(kw)
		if len(rest) > kwLen && rest[:kwLen] == kw {
			c := rest[kwLen]
			if c == ' ' || c == '\t' {
				// skip remaining whitespace, check for word char
				j := kwLen + 1
				for j < len(rest) && (rest[j] == ' ' || rest[j] == '\t') {
					j++
				}
				if j < len(rest) && isWordChar(rest[j]) {
					return true
				}
			}
		}
	}
	return false
}

// hasPrefix checks if s[start:] begins with prefix.
func hasLiteralPrefix(s string, start int, prefix string) bool {
	end := start + len(prefix)
	return end <= len(s) && s[start:end] == prefix
}

// containsWordCI checks if line contains any of the keywords as whole words
// (case-insensitive). Matches (?i)\b(KW1|KW2)\b
// Zero-allocation: uses inline ASCII case-folding instead of strings.ToLower.
// Uses a first-char dispatch to skip positions that can't match any keyword.
func containsWordCI(line string, keywords []string) bool {
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
		if i > 0 && isWordChar(line[i-1]) {
			continue
		}
		for _, kw := range keywords {
			if kw[0] != c {
				continue
			}
			if matchWordAtCD(line, i, kw) {
				return true
			}
		}
	}
	return false
}

func matchWordAtCD(s string, i int, kw string) bool {
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
	if end < len(s) && isWordChar(s[end]) {
		return false
	}
	return true
}

// indexWordFoldASCII finds kw in s case-insensitively with word-boundary checks.
// kw must be pre-lowercased ASCII. Returns index of match or -1.
// Zero-allocation: folds case inline via byte arithmetic.
func indexWordFoldASCII(s, kw string) int {
	kwLen := len(kw)
	if kwLen == 0 || len(s) < kwLen {
		return -1
	}
	kw0 := kw[0]
	limit := len(s) - kwLen
	for i := 0; i <= limit; i++ {
		// Quick check: first char with inline case fold
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 32
		}
		if c != kw0 {
			continue
		}

		// Match remaining chars
		match := true
		for j := 1; j < kwLen; j++ {
			c := s[i+j]
			if c >= 'A' && c <= 'Z' {
				c += 32
			}
			if c != kw[j] {
				match = false
				break
			}
		}
		if !match {
			continue
		}

		// Word boundary checks
		if i > 0 && isWordChar(s[i-1]) {
			continue
		}
		end := i + kwLen
		if end < len(s) && isWordChar(s[end]) {
			continue
		}
		return i
	}
	return -1
}

// isDigit returns true for ASCII digits.
func isDigit(c byte) bool {
	return c >= '0' && c <= '9'
}

// ---------------------------------------------------------------------------
// Language matchers (replace []*regexp.Regexp with func(line string) bool)
// ---------------------------------------------------------------------------

type langMatcher struct {
	name  string
	match func(line string) bool
}

// matchPython matches the original Python regex patterns:
//   ^\s*(def|class|import|from|async def)\s+\w+
//   ^\s*@\w+
//   ^\s*"""
//   ^\s*if __name__\s*==
func matchPython(line string) bool {
	start := skipLineWhitespace(line)
	if start >= len(line) {
		return false
	}
	rest := line[start:]

	// ^\s*(def|class|import|from|async def)\s+\w+
	if hasKeywordThenWhitespaceWord(line, start, []string{"async def", "def", "class", "import", "from"}) {
		return true
	}
	// ^\s*@\w+
	if rest[0] == '@' && len(rest) > 1 && isWordChar(rest[1]) {
		return true
	}
	// ^\s*"""
	if len(rest) >= 3 && rest[0] == '"' && rest[1] == '"' && rest[2] == '"' {
		return true
	}
	// ^\s*if __name__\s*==
	if hasLiteralPrefix(line, start, "if __name__") {
		j := start + len("if __name__")
		for j < len(line) && (line[j] == ' ' || line[j] == '\t') {
			j++
		}
		if j+1 < len(line) && line[j] == '=' && line[j+1] == '=' {
			return true
		}
	}
	return false
}

// matchJavascript matches:
//   ^\s*(function|const|let|var|class|import|export)\s+
//   ^\s*(async\s+function|=>\s*\{)
//   ^\s*module\.exports
func matchJavascript(line string) bool {
	start := skipLineWhitespace(line)
	if start >= len(line) {
		return false
	}

	// ^\s*(function|const|let|var|class|import|export)\s+
	if hasKeywordThenWhitespace(line, start, []string{"function", "const", "let", "var", "class", "import", "export"}) {
		return true
	}
	// ^\s*async\s+function
	if hasKeywordThenWhitespace(line, start, []string{"async"}) {
		j := start + 5 // len("async")
		for j < len(line) && (line[j] == ' ' || line[j] == '\t') {
			j++
		}
		if hasLiteralPrefix(line, j, "function") {
			return true
		}
	}
	// ^\s*=>\s*\{
	rest := line[start:]
	if len(rest) >= 2 && rest[0] == '=' && rest[1] == '>' {
		j := 2
		for j < len(rest) && (rest[j] == ' ' || rest[j] == '\t') {
			j++
		}
		if j < len(rest) && rest[j] == '{' {
			return true
		}
	}
	// ^\s*module\.exports
	if hasLiteralPrefix(line, start, "module.exports") {
		return true
	}
	return false
}

// matchTypescript matches:
//   ^\s*(interface|type|enum|namespace)\s+\w+
//   ^:\s*(string|number|boolean|any|void)\b
func matchTypescript(line string) bool {
	start := skipLineWhitespace(line)
	if start >= len(line) {
		return false
	}

	// ^\s*(interface|type|enum|namespace)\s+\w+
	if hasKeywordThenWhitespaceWord(line, start, []string{"interface", "type", "enum", "namespace"}) {
		return true
	}
	// ^:\s*(string|number|boolean|any|void)\b
	if len(line) > 0 && line[0] == ':' {
		j := 1
		for j < len(line) && (line[j] == ' ' || line[j] == '\t') {
			j++
		}
		for _, kw := range []string{"string", "number", "boolean", "any", "void"} {
			end := j + len(kw)
			if end <= len(line) && line[j:end] == kw {
				if end == len(line) || !isWordChar(line[end]) {
					return true
				}
			}
		}
	}
	return false
}

// matchGo matches:
//   ^\s*(func|type|package|import)\s+
//   ^\s*func\s+\([^)]+\)\s+\w+
func matchGo(line string) bool {
	start := skipLineWhitespace(line)
	if start >= len(line) {
		return false
	}

	// ^\s*(func|type|package|import)\s+
	if hasKeywordThenWhitespace(line, start, []string{"func", "type", "package", "import"}) {
		// Also check the second pattern: ^\s*func\s+\([^)]+\)\s+\w+
		// (this is a subset of func\s+ matches so both are covered)
		return true
	}
	return false
}

// matchRust matches:
//   ^\s*(fn|struct|enum|impl|mod|use|pub)\s+
//   ^\s*#\[
func matchRust(line string) bool {
	start := skipLineWhitespace(line)
	if start >= len(line) {
		return false
	}

	if hasKeywordThenWhitespace(line, start, []string{"fn", "struct", "enum", "impl", "mod", "use", "pub"}) {
		return true
	}
	// ^\s*#\[
	rest := line[start:]
	if len(rest) >= 2 && rest[0] == '#' && rest[1] == '[' {
		return true
	}
	return false
}

// matchJava matches:
//   ^\s*(public|private|protected)\s+(class|interface|enum)
//   ^\s*@\w+
//   ^\s*package\s+[\w.]+;
func matchJava(line string) bool {
	start := skipLineWhitespace(line)
	if start >= len(line) {
		return false
	}
	rest := line[start:]

	// ^\s*(public|private|protected)\s+(class|interface|enum)
	for _, access := range []string{"public", "private", "protected"} {
		if len(rest) > len(access) && rest[:len(access)] == access {
			c := rest[len(access)]
			if c == ' ' || c == '\t' {
				j := len(access) + 1
				for j < len(rest) && (rest[j] == ' ' || rest[j] == '\t') {
					j++
				}
				sub := rest[j:]
				for _, typekw := range []string{"class", "interface", "enum"} {
					if len(sub) >= len(typekw) && sub[:len(typekw)] == typekw {
						return true
					}
				}
			}
		}
	}
	// ^\s*@\w+
	if rest[0] == '@' && len(rest) > 1 && isWordChar(rest[1]) {
		return true
	}
	// ^\s*package\s+[\w.]+;
	if hasKeywordThenWhitespace(line, start, []string{"package"}) {
		j := start + 7 // len("package")
		for j < len(line) && (line[j] == ' ' || line[j] == '\t') {
			j++
		}
		// scan [\w.]+;
		k := j
		for k < len(line) && (isWordChar(line[k]) || line[k] == '.') {
			k++
		}
		if k > j && k < len(line) && line[k] == ';' {
			return true
		}
	}
	return false
}

var codeLanguages = []langMatcher{
	{"python", matchPython},
	{"javascript", matchJavascript},
	{"typescript", matchTypescript},
	{"go", matchGo},
	{"rust", matchRust},
	{"java", matchJava},
}

// ---------------------------------------------------------------------------
// Log/build output matchers (replace logPatterns []*regexp.Regexp)
// ---------------------------------------------------------------------------

// logMatcher is a single log-detection pattern. isError marks patterns at
// indices 0-1 in the original logPatterns slice.
type logMatcher struct {
	match   func(line string) bool
	isError bool
}

var logMatchers = []logMatcher{
	// 0: (?i)\b(ERROR|FAIL|FAILED|FATAL|CRITICAL)\b
	{func(line string) bool {
		return containsWordCI(line, []string{"error", "fail", "failed", "fatal", "critical"})
	}, true},
	// 1: (?i)\b(WARN|WARNING)\b
	{func(line string) bool {
		return containsWordCI(line, []string{"warning", "warn"})
	}, true},
	// 2: (?i)\b(INFO|DEBUG|TRACE)\b
	{func(line string) bool {
		return containsWordCI(line, []string{"info", "debug", "trace"})
	}, false},
	// 3: ^\s*\d{4}-\d{2}-\d{2}
	{func(line string) bool {
		s := skipLineWhitespace(line)
		// need at least 10 chars: YYYY-MM-DD
		if s+10 > len(line) {
			return false
		}
		return isDigit(line[s]) && isDigit(line[s+1]) && isDigit(line[s+2]) && isDigit(line[s+3]) &&
			line[s+4] == '-' && isDigit(line[s+5]) && isDigit(line[s+6]) &&
			line[s+7] == '-' && isDigit(line[s+8]) && isDigit(line[s+9])
	}, false},
	// 4: ^\s*\[\d{2}:\d{2}:\d{2}\]
	{func(line string) bool {
		s := skipLineWhitespace(line)
		// need [HH:MM:SS] = 10 chars
		if s+10 > len(line) {
			return false
		}
		return line[s] == '[' && isDigit(line[s+1]) && isDigit(line[s+2]) &&
			line[s+3] == ':' && isDigit(line[s+4]) && isDigit(line[s+5]) &&
			line[s+6] == ':' && isDigit(line[s+7]) && isDigit(line[s+8]) &&
			line[s+9] == ']'
	}, false},
	// 5: ^={3,}|^-{3,}
	{func(line string) bool {
		if len(line) < 3 {
			return false
		}
		if line[0] == '=' && line[1] == '=' && line[2] == '=' {
			return true
		}
		if line[0] == '-' && line[1] == '-' && line[2] == '-' {
			return true
		}
		return false
	}, false},
	// 6: ^\s*PASSED|^\s*FAILED|^\s*SKIPPED
	{func(line string) bool {
		s := skipLineWhitespace(line)
		return hasLiteralPrefix(line, s, "PASSED") ||
			hasLiteralPrefix(line, s, "FAILED") ||
			hasLiteralPrefix(line, s, "SKIPPED")
	}, false},
	// 7: ^npm ERR!|^yarn error|^cargo error
	{func(line string) bool {
		return hasLiteralPrefix(line, 0, "npm ERR!") ||
			hasLiteralPrefix(line, 0, "yarn error") ||
			hasLiteralPrefix(line, 0, "cargo error")
	}, false},
	// 8: Traceback \(most recent call last\) - substring match
	{func(line string) bool {
		return strings.Contains(line, "Traceback (most recent call last)")
	}, false},
	// 9: ^\s*at\s+[\w.$]+\(
	{func(line string) bool {
		s := skipLineWhitespace(line)
		if !hasLiteralPrefix(line, s, "at") {
			return false
		}
		j := s + 2
		if j >= len(line) || (line[j] != ' ' && line[j] != '\t') {
			return false
		}
		for j < len(line) && (line[j] == ' ' || line[j] == '\t') {
			j++
		}
		// scan [\w.$]+
		k := j
		for k < len(line) && (isWordChar(line[k]) || line[k] == '.' || line[k] == '$') {
			k++
		}
		return k > j && k < len(line) && line[k] == '('
	}, false},
}

// forEachLine calls fn for each line in text, up to maxLines lines.
// It avoids allocating a []string slice via strings.Split.
func forEachLine(text string, maxLines int, fn func(line string)) {
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

// isAllWhitespace checks if a string is empty or only whitespace
// without allocating (unlike strings.TrimSpace).
func isAllWhitespace(s string) bool {
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

// DetectContentType analyzes text and returns the most likely content type.
// Port of Rust detect_content_type().
func DetectContentType(text string) DetectionResult {
	if len(text) == 0 || isAllWhitespace(text) {
		return plainTextResult(0.0)
	}

	// 1. JSON array (highest priority)
	if r, ok := tryDetectJSON(text); ok {
		return r
	}

	// 2. Git diff (>= 0.7 confidence)
	if r, ok := tryDetectDiff(text); ok && r.Confidence >= 0.7 {
		return r
	}

	// 3. HTML (>= 0.7 confidence)
	if r, ok := tryDetectHTML(text); ok && r.Confidence >= 0.7 {
		return r
	}

	// 4. Search results (>= 0.6 confidence)
	if r, ok := tryDetectSearch(text); ok && r.Confidence >= 0.6 {
		return r
	}

	// 5. Build/log output (>= 0.5 confidence)
	if r, ok := tryDetectLog(text); ok && r.Confidence >= 0.5 {
		return r
	}

	// 6. Source code (>= 0.5 confidence)
	if r, ok := tryDetectCode(text); ok && r.Confidence >= 0.5 {
		return r
	}

	// 7. Fallback
	return plainTextResult(0.5)
}

// IsJsonArrayOfDicts returns true if text is a JSON array where every element is an object.
func IsJsonArrayOfDicts(text string) bool {
	result := DetectContentType(text)
	if result.ContentType != JsonArray {
		return false
	}
	if isDictArray, ok := result.Metadata["is_dict_array"].(bool); ok {
		return isDictArray
	}
	return false
}

// tryDetectJSON checks if text is a JSON array without full parsing.
// Uses manual scanning instead of json.Unmarshal to avoid allocations.
func tryDetectJSON(text string) (DetectionResult, bool) {
	trimmed := strings.TrimSpace(text)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return DetectionResult{}, false
	}

	// Validate JSON structure and count items using a manual scanner.
	// This replaces json.Unmarshal([]byte(trimmed), &arr) which allocated heavily.
	itemCount, isDictArray, valid := scanJSONArray(trimmed)
	if !valid {
		return DetectionResult{}, false
	}

	confidence := 0.8
	if isDictArray {
		confidence = 1.0
	}

	return DetectionResult{
		ContentType: JsonArray,
		Confidence:  confidence,
		Metadata: map[string]interface{}{
			"item_count":    itemCount,
			"is_dict_array": isDictArray,
		},
	}, true
}

// scanJSONArray validates that s is a JSON array, counts its elements,
// and checks whether every element is a JSON object (dict).
// Returns (itemCount, isDictArray, valid).
func scanJSONArray(s string) (int, bool, bool) {
	// s[0] == '[' is guaranteed by caller.
	i := 1
	i = skipWhitespace(s, i)
	if i >= len(s) {
		return 0, false, false
	}

	// Empty array
	if s[i] == ']' {
		return 0, false, true
	}

	itemCount := 0
	isDictArray := true

	for {
		i = skipWhitespace(s, i)
		if i >= len(s) {
			return 0, false, false
		}

		// Check if this element starts with '{'
		if s[i] != '{' {
			isDictArray = false
		}

		// Skip one JSON value
		var ok bool
		i, ok = skipJSONValue(s, i)
		if !ok {
			return 0, false, false
		}
		itemCount++

		i = skipWhitespace(s, i)
		if i >= len(s) {
			return 0, false, false
		}

		if s[i] == ']' {
			// Check no trailing content
			j := skipWhitespace(s, i+1)
			if j < len(s) {
				return 0, false, false
			}
			return itemCount, isDictArray && itemCount > 0, true
		}
		if s[i] == ',' {
			i++
			continue
		}
		return 0, false, false
	}
}

func skipWhitespace(s string, i int) int {
	for i < len(s) {
		switch s[i] {
		case ' ', '\t', '\n', '\r':
			i++
		default:
			return i
		}
	}
	return i
}

// skipJSONValue advances past one complete JSON value starting at s[i].
// Returns the new index and whether parsing succeeded.
func skipJSONValue(s string, i int) (int, bool) {
	if i >= len(s) {
		return i, false
	}

	switch s[i] {
	case '"':
		return skipJSONString(s, i)
	case '{':
		return skipJSONContainer(s, i, '{', '}')
	case '[':
		return skipJSONContainer(s, i, '[', ']')
	case 't': // true
		if i+4 <= len(s) && s[i:i+4] == "true" {
			return i + 4, true
		}
		return i, false
	case 'f': // false
		if i+5 <= len(s) && s[i:i+5] == "false" {
			return i + 5, true
		}
		return i, false
	case 'n': // null
		if i+4 <= len(s) && s[i:i+4] == "null" {
			return i + 4, true
		}
		return i, false
	default:
		// number: optional minus, digits, optional fraction, optional exponent
		if s[i] == '-' || (s[i] >= '0' && s[i] <= '9') {
			return skipJSONNumber(s, i)
		}
		return i, false
	}
}

func skipJSONString(s string, i int) (int, bool) {
	// s[i] == '"'
	i++
	for i < len(s) {
		if s[i] == '\\' {
			i += 2 // skip escaped char
			continue
		}
		if s[i] == '"' {
			return i + 1, true
		}
		i++
	}
	return i, false
}

// skipJSONContainer skips a balanced { } or [ ] block including nested content.
func skipJSONContainer(s string, i int, open, close byte) (int, bool) {
	depth := 1
	i++ // skip opening bracket/brace
	for i < len(s) {
		switch s[i] {
		case '"':
			var ok bool
			i, ok = skipJSONString(s, i)
			if !ok {
				return i, false
			}
			continue
		case open:
			depth++
		case close:
			depth--
			if depth == 0 {
				return i + 1, true
			}
		}
		i++
	}
	return i, false
}

func skipJSONNumber(s string, i int) (int, bool) {
	start := i
	if i < len(s) && s[i] == '-' {
		i++
	}
	if i >= len(s) || s[i] < '0' || s[i] > '9' {
		return start, false
	}
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i < len(s) && s[i] == '.' {
		i++
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
	}
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		i++
		if i < len(s) && (s[i] == '+' || s[i] == '-') {
			i++
		}
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
	}
	return i, true
}

func tryDetectDiff(text string) (DetectionResult, bool) {
	var headerMatches, changeMatches uint32
	forEachLine(text, 500, func(line string) {
		if diffHeaderPattern.MatchString(line) {
			headerMatches++
		}
		if matchesDiffChange(line) {
			changeMatches++
		}
	})

	if headerMatches == 0 {
		return DetectionResult{}, false
	}

	confidence := math.Min(0.5+float64(headerMatches)*0.2+float64(changeMatches)*0.05, 1.0)
	return DetectionResult{
		ContentType: GitDiff,
		Confidence:  confidence,
		Metadata: map[string]interface{}{
			"header_matches": headerMatches,
			"change_lines":   changeMatches,
		},
	}, true
}

func tryDetectHTML(text string) (DetectionResult, bool) {
	sample := text
	if len(sample) > 3000 {
		sample = sample[:3000]
	}

	hasDoctype := matchesHTMLDoctype(sample)
	hasHTMLTag := containsHTMLTagCI(sample, "html")
	hasHead := containsHTMLTagCI(sample, "head")
	hasBody := containsHTMLTagCI(sample, "body")
	structuralMatches := countHTMLStructuralTags(sample)

	if !hasDoctype && !hasHTMLTag && structuralMatches < 3 {
		return DetectionResult{}, false
	}

	confidence := 0.0
	if hasDoctype {
		confidence += 0.5
	}
	if hasHTMLTag {
		confidence += 0.3
	}
	if hasHead {
		confidence += 0.1
	}
	if hasBody {
		confidence += 0.1
	}
	confidence += math.Min(float64(structuralMatches)*0.03, 0.3)
	confidence = math.Min(confidence, 1.0)

	if confidence < 0.5 {
		return DetectionResult{}, false
	}

	return DetectionResult{
		ContentType: Html,
		Confidence:  confidence,
		Metadata: map[string]interface{}{
			"has_doctype":     hasDoctype,
			"has_html_tag":    hasHTMLTag,
			"structural_tags": structuralMatches,
		},
	}, true
}

func tryDetectSearch(text string) (DetectionResult, bool) {
	var matchingLines uint32
	var nonEmptyLines uint32

	forEachLine(text, 100, func(line string) {
		if isAllWhitespace(line) {
			return
		}
		nonEmptyLines++
		if matchesSearchResult(line) {
			matchingLines++
		}
	})

	if matchingLines == 0 || nonEmptyLines == 0 {
		return DetectionResult{}, false
	}

	ratio := float64(matchingLines) / float64(nonEmptyLines)
	if ratio < 0.3 {
		return DetectionResult{}, false
	}

	confidence := math.Min(0.4+ratio*0.6, 1.0)
	return DetectionResult{
		ContentType: SearchResults,
		Confidence:  confidence,
		Metadata: map[string]interface{}{
			"matching_lines": matchingLines,
			"total_lines":    nonEmptyLines,
		},
	}, true
}

func tryDetectLog(text string) (DetectionResult, bool) {
	var patternMatches, errorMatches uint32
	var nonEmptyLines uint32

	forEachLine(text, 200, func(line string) {
		if isAllWhitespace(line) {
			return
		}
		nonEmptyLines++
		for _, lm := range logMatchers {
			if lm.match(line) {
				patternMatches++
				if lm.isError {
					errorMatches++
				}
				break
			}
		}
	})

	if patternMatches == 0 || nonEmptyLines == 0 {
		return DetectionResult{}, false
	}

	ratio := float64(patternMatches) / float64(nonEmptyLines)
	if ratio < 0.1 {
		return DetectionResult{}, false
	}

	confidence := math.Min(0.3+ratio*0.5+float64(errorMatches)*0.05, 1.0)
	return DetectionResult{
		ContentType: BuildOutput,
		Confidence:  confidence,
		Metadata: map[string]interface{}{
			"pattern_matches": patternMatches,
			"error_matches":   errorMatches,
			"total_lines":     nonEmptyLines,
		},
	}, true
}

func tryDetectCode(text string) (DetectionResult, bool) {
	// Track scores in first-match insertion order (matching Python dict semantics).
	type langScore struct {
		name  string
		score uint32
	}
	var languageScores []langScore
	var nonEmptyLines uint32

	forEachLine(text, 100, func(line string) {
		if isAllWhitespace(line) {
			return // skip but still count below
		}
		nonEmptyLines++

		for _, cl := range codeLanguages {
			if cl.match(line) {
				found := false
				for i := range languageScores {
					if languageScores[i].name == cl.name {
						languageScores[i].score++
						found = true
						break
					}
				}
				if !found {
					languageScores = append(languageScores, langScore{cl.name, 1})
				}
			}
		}
	})

	if len(languageScores) == 0 {
		return DetectionResult{}, false
	}

	// Find max score, first-on-tie (matching Python max() behavior).
	var bestLang string
	var bestScore uint32
	for _, ls := range languageScores {
		if ls.score > bestScore {
			bestScore = ls.score
			bestLang = ls.name
		}
	}

	if bestScore < 3 {
		return DetectionResult{}, false
	}

	if nonEmptyLines == 0 {
		nonEmptyLines = 1
	}

	ratio := float64(bestScore) / float64(nonEmptyLines)
	confidence := math.Min(0.4+ratio*0.4+float64(bestScore)*0.02, 1.0)

	return DetectionResult{
		ContentType: SourceCode,
		Confidence:  confidence,
		Metadata: map[string]interface{}{
			"language":        bestLang,
			"pattern_matches": bestScore,
		},
	}, true
}
