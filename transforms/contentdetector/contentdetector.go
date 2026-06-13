// Package contentdetector provides content type detection
// for multi-format compression routing.
//
// Direct port of headroom-core/src/transforms/content_detector.rs.
// Code language patterns use hand-rolled string scanning instead of regex
// to avoid O(lines * patterns) regexp.MatchString overhead.
package contentdetector

import (
	"math"
	"strings"

	"github.com/uber/goheadroom/internal/textutil"
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

func matchesDiffHeader(line string) bool {
	if strings.HasPrefix(line, "diff --git ") {
		return true
	}
	if strings.HasPrefix(line, "diff --combined ") {
		return true
	}
	if strings.HasPrefix(line, "diff --cc ") {
		return true
	}
	if strings.HasPrefix(line, "--- a/") {
		return true
	}
	if len(line) >= 2 && line[0] == '@' && line[1] == '@' {
		return matchesHunkHeader(line)
	}
	return false
}

func matchesHunkHeader(line string) bool {
	i := 2
	for i < len(line) && (line[i] == '@') {
		i++
	}
	atCount := i

	if i >= len(line) || (line[i] != ' ' && line[i] != '\t') {
		return false
	}
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}

	if atCount == 2 {
		i = skipHunkRange(line, i, '-')
		if i < 0 {
			return false
		}
		i = skipHunkRange(line, i, '+')
		if i < 0 {
			return false
		}
	} else {
		if i >= len(line) || line[i] != '-' {
			return false
		}
		i = skipOptionalRange(line, i)
		if i < 0 {
			return false
		}

		for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
			i++
		}
		for i < len(line) && line[i] == '-' {
			i = skipOptionalRange(line, i)
			if i < 0 {
				return false
			}
			for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
				i++
			}
		}

		if i >= len(line) || line[i] != '+' {
			return false
		}
		i = skipOptionalRange(line, i)
		if i < 0 {
			return false
		}
	}

	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}

	closing := line[i:]
	return strings.HasPrefix(closing, strings.Repeat("@", atCount))
}

func skipHunkRange(line string, i int, prefix byte) int {
	if i >= len(line) || line[i] != prefix {
		return -1
	}
	i++
	if i >= len(line) || line[i] < '0' || line[i] > '9' {
		return -1
	}
	for i < len(line) && line[i] >= '0' && line[i] <= '9' {
		i++
	}
	if i < len(line) && line[i] == ',' {
		i++
		if i >= len(line) || line[i] < '0' || line[i] > '9' {
			return -1
		}
		for i < len(line) && line[i] >= '0' && line[i] <= '9' {
			i++
		}
	}
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	return i
}

func skipOptionalRange(line string, i int) int {
	i++
	if i >= len(line) || line[i] < '0' || line[i] > '9' {
		return -1
	}
	for i < len(line) && line[i] >= '0' && line[i] <= '9' {
		i++
	}
	if i < len(line) && line[i] == ',' {
		i++
		if i >= len(line) || line[i] < '0' || line[i] > '9' {
			return -1
		}
		for i < len(line) && line[i] >= '0' && line[i] <= '9' {
			i++
		}
	}
	return i
}

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
	i := textutil.SkipLineWhitespace(s)
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
	tagLen := len(tag)
	need := tagLen + 2
	for {
		if len(s) < need {
			return false
		}
		idx := strings.IndexByte(s, '<')
		if idx < 0 || len(s)-idx < need {
			return false
		}
		match := true
		for j := 0; j < tagLen; j++ {
			if toLowerASCII(s[idx+1+j]) != tag[j] {
				match = false
				break
			}
		}
		if match {
			next := idx + 1 + tagLen
			if next < len(s) && (s[next] == ' ' || s[next] == '\t' || s[next] == '>' || s[next] == '\n' || s[next] == '/') {
				return true
			}
		}
		s = s[idx+1:]
	}
}

// countHTMLStructuralTags replaces htmlStructuralPattern FindAllStringIndex.
// Counts occurrences of structural HTML tags (case-insensitive).
func countHTMLStructuralTags(s string) uint32 {
	tags := []string{"div", "span", "script", "style", "link", "meta", "nav", "header", "footer", "aside", "article", "section", "main"}
	var count uint32
	for len(s) > 1 {
		idx := strings.IndexByte(s, '<')
		if idx < 0 || idx+1 >= len(s) {
			break
		}
		for _, tag := range tags {
			end := idx + 1 + len(tag)
			if end >= len(s) {
				continue
			}
			match := true
			for j := 0; j < len(tag); j++ {
				if toLowerASCII(s[idx+1+j]) != tag[j] {
					match = false
					break
				}
			}
			if match {
				c := s[end]
				if c == ' ' || c == '\t' || c == '>' || c == '\n' || c == '/' {
					count++
					break
				}
			}
		}
		s = s[idx+1:]
	}
	return count
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
	start := textutil.SkipLineWhitespace(line)
	if start >= len(line) {
		return false
	}
	rest := line[start:]

	// ^\s*(def|class|import|from|async def)\s+\w+
	if textutil.HasKeywordThenWhitespaceWord(line, start, []string{"async def", "def", "class", "import", "from"}) {
		return true
	}
	// ^\s*@\w+
	if rest[0] == '@' && len(rest) > 1 && textutil.IsWordChar(rest[1]) {
		return true
	}
	// ^\s*"""
	if len(rest) >= 3 && rest[0] == '"' && rest[1] == '"' && rest[2] == '"' {
		return true
	}
	// ^\s*if __name__\s*==
	if textutil.HasLiteralPrefix(line, start, "if __name__") {
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
	start := textutil.SkipLineWhitespace(line)
	if start >= len(line) {
		return false
	}

	// ^\s*(function|const|let|var|class|import|export)\s+
	if textutil.HasKeywordThenWhitespace(line, start, []string{"function", "const", "let", "var", "class", "import", "export"}) {
		return true
	}
	// ^\s*async\s+function
	if textutil.HasKeywordThenWhitespace(line, start, []string{"async"}) {
		j := start + 5 // len("async")
		for j < len(line) && (line[j] == ' ' || line[j] == '\t') {
			j++
		}
		if textutil.HasLiteralPrefix(line, j, "function") {
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
	if textutil.HasLiteralPrefix(line, start, "module.exports") {
		return true
	}
	return false
}

// matchTypescript matches:
//   ^\s*(interface|type|enum|namespace)\s+\w+
//   ^:\s*(string|number|boolean|any|void)\b
func matchTypescript(line string) bool {
	start := textutil.SkipLineWhitespace(line)
	if start >= len(line) {
		return false
	}

	// ^\s*(interface|type|enum|namespace)\s+\w+
	if textutil.HasKeywordThenWhitespaceWord(line, start, []string{"interface", "type", "enum", "namespace"}) {
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
				if end == len(line) || !textutil.IsWordChar(line[end]) {
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
	start := textutil.SkipLineWhitespace(line)
	if start >= len(line) {
		return false
	}

	// ^\s*(func|type|package|import)\s+
	if textutil.HasKeywordThenWhitespace(line, start, []string{"func", "type", "package", "import"}) {
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
	start := textutil.SkipLineWhitespace(line)
	if start >= len(line) {
		return false
	}

	if textutil.HasKeywordThenWhitespace(line, start, []string{"fn", "struct", "enum", "impl", "mod", "use", "pub"}) {
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
	start := textutil.SkipLineWhitespace(line)
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
	if rest[0] == '@' && len(rest) > 1 && textutil.IsWordChar(rest[1]) {
		return true
	}
	// ^\s*package\s+[\w.]+;
	if textutil.HasKeywordThenWhitespace(line, start, []string{"package"}) {
		j := start + 7 // len("package")
		for j < len(line) && (line[j] == ' ' || line[j] == '\t') {
			j++
		}
		// scan [\w.]+;
		k := j
		for k < len(line) && (textutil.IsWordChar(line[k]) || line[k] == '.') {
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

var errorKeywords = []string{"error", "fail", "failed", "fatal", "critical"}
var warnKeywords = []string{"warning", "warn"}
var infoKeywords = []string{"info", "debug", "trace"}

func matchLogLine(line string) (matched bool, isError bool) {
	if textutil.ContainsWordCI(line, errorKeywords) {
		return true, true
	}
	if textutil.ContainsWordCI(line, warnKeywords) {
		return true, true
	}
	if textutil.ContainsWordCI(line, infoKeywords) {
		return true, false
	}
	s := textutil.SkipLineWhitespace(line)
	if s+10 <= len(line) &&
		textutil.IsDigit(line[s]) && textutil.IsDigit(line[s+1]) && textutil.IsDigit(line[s+2]) && textutil.IsDigit(line[s+3]) &&
		line[s+4] == '-' && textutil.IsDigit(line[s+5]) && textutil.IsDigit(line[s+6]) &&
		line[s+7] == '-' && textutil.IsDigit(line[s+8]) && textutil.IsDigit(line[s+9]) {
		return true, false
	}
	if s+10 <= len(line) &&
		line[s] == '[' && textutil.IsDigit(line[s+1]) && textutil.IsDigit(line[s+2]) &&
		line[s+3] == ':' && textutil.IsDigit(line[s+4]) && textutil.IsDigit(line[s+5]) &&
		line[s+6] == ':' && textutil.IsDigit(line[s+7]) && textutil.IsDigit(line[s+8]) &&
		line[s+9] == ']' {
		return true, false
	}
	if len(line) >= 3 {
		if line[0] == '=' && line[1] == '=' && line[2] == '=' {
			return true, false
		}
		if line[0] == '-' && line[1] == '-' && line[2] == '-' {
			return true, false
		}
	}
	if textutil.HasLiteralPrefix(line, s, "PASSED") ||
		textutil.HasLiteralPrefix(line, s, "FAILED") ||
		textutil.HasLiteralPrefix(line, s, "SKIPPED") {
		return true, false
	}
	if textutil.HasLiteralPrefix(line, 0, "npm ERR!") ||
		textutil.HasLiteralPrefix(line, 0, "yarn error") ||
		textutil.HasLiteralPrefix(line, 0, "cargo error") {
		return true, false
	}
	if strings.Contains(line, "Traceback (most recent call last)") {
		return true, false
	}
	if textutil.HasLiteralPrefix(line, s, "at") {
		j := s + 2
		if j < len(line) && (line[j] == ' ' || line[j] == '\t') {
			for j < len(line) && (line[j] == ' ' || line[j] == '\t') {
				j++
			}
			k := j
			for k < len(line) && (textutil.IsWordChar(line[k]) || line[k] == '.' || line[k] == '$') {
				k++
			}
			if k > j && k < len(line) && line[k] == '(' {
				return true, false
			}
		}
	}
	return false, false
}


// DetectContentType analyzes text and returns the most likely content type.
// Port of Rust detect_content_type().
func DetectContentType(text string) DetectionResult {
	if len(text) == 0 || textutil.IsAllWhitespace(text) {
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
	remaining := text
	for count := 0; count < 500 && len(remaining) > 0; count++ {
		idx := strings.IndexByte(remaining, '\n')
		var line string
		if idx < 0 {
			line = remaining
			remaining = ""
		} else {
			line = remaining[:idx]
			remaining = remaining[idx+1:]
		}
		if matchesDiffHeader(line) {
			headerMatches++
		}
		if matchesDiffChange(line) {
			changeMatches++
		}
	}

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

	remaining := text
	for count := 0; count < 100 && len(remaining) > 0; count++ {
		idx := strings.IndexByte(remaining, '\n')
		var line string
		if idx < 0 {
			line = remaining
			remaining = ""
		} else {
			line = remaining[:idx]
			remaining = remaining[idx+1:]
		}
		if textutil.IsAllWhitespace(line) {
			continue
		}
		nonEmptyLines++
		if matchesSearchResult(line) {
			matchingLines++
		}
	}

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

	remaining := text
	for count := 0; count < 200 && len(remaining) > 0; count++ {
		idx := strings.IndexByte(remaining, '\n')
		var line string
		if idx < 0 {
			line = remaining
			remaining = ""
		} else {
			line = remaining[:idx]
			remaining = remaining[idx+1:]
		}
		if textutil.IsAllWhitespace(line) {
			continue
		}
		nonEmptyLines++
		if matched, isErr := matchLogLine(line); matched {
			patternMatches++
			if isErr {
				errorMatches++
			}
		}
	}

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
	type langScore struct {
		name  string
		score uint32
	}
	var languageScores []langScore
	var nonEmptyLines uint32

	remaining := text
	for count := 0; count < 100 && len(remaining) > 0; count++ {
		idx := strings.IndexByte(remaining, '\n')
		var line string
		if idx < 0 {
			line = remaining
			remaining = ""
		} else {
			line = remaining[:idx]
			remaining = remaining[idx+1:]
		}
		if textutil.IsAllWhitespace(line) {
			continue
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
	}

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
