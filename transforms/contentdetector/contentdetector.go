// Package contentdetector provides regex-based content type detection
// for multi-format compression routing.
//
// Direct port of headroom-core/src/transforms/content_detector.rs.
package contentdetector

import (
	"encoding/json"
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
	return DetectionResult{ContentType: PlainText, Confidence: confidence, Metadata: map[string]interface{}{}}
}

// Regex patterns (compiled once).
var (
	searchResultPattern = regexp.MustCompile(`^[^\s:]+:\d+:`)

	diffHeaderPattern = regexp.MustCompile(`^(diff --git|diff --combined |diff --cc |--- a/|@@\s+-\d+,\d+\s+\+\d+,\d+\s+@@|@@@+\s+-\d+(?:,\d+)?\s+(?:-\d+(?:,\d+)?\s+)+\+\d+(?:,\d+)?\s+@@@+)`)

	diffChangePattern = regexp.MustCompile(`^[+-][^+-]`)

	// Code patterns by language.
	pythonPatterns = []*regexp.Regexp{
		regexp.MustCompile(`^\s*(def|class|import|from|async def)\s+\w+`),
		regexp.MustCompile(`^\s*@\w+`),
		regexp.MustCompile(`^\s*"""`),
		regexp.MustCompile(`^\s*if __name__\s*==`),
	}
	javascriptPatterns = []*regexp.Regexp{
		regexp.MustCompile(`^\s*(function|const|let|var|class|import|export)\s+`),
		regexp.MustCompile(`^\s*(async\s+function|=>\s*\{)`),
		regexp.MustCompile(`^\s*module\.exports`),
	}
	typescriptPatterns = []*regexp.Regexp{
		regexp.MustCompile(`^\s*(interface|type|enum|namespace)\s+\w+`),
		regexp.MustCompile(`^:\s*(string|number|boolean|any|void)\b`),
	}
	goPatterns = []*regexp.Regexp{
		regexp.MustCompile(`^\s*(func|type|package|import)\s+`),
		regexp.MustCompile(`^\s*func\s+\([^)]+\)\s+\w+`),
	}
	rustPatterns = []*regexp.Regexp{
		regexp.MustCompile(`^\s*(fn|struct|enum|impl|mod|use|pub)\s+`),
		regexp.MustCompile(`^\s*#\[`),
	}
	javaPatterns = []*regexp.Regexp{
		regexp.MustCompile(`^\s*(public|private|protected)\s+(class|interface|enum)`),
		regexp.MustCompile(`^\s*@\w+`),
		regexp.MustCompile(`^\s*package\s+[\w.]+;`),
	}

	// Log / build output patterns. Indices 0-1 are error patterns.
	logPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b(ERROR|FAIL|FAILED|FATAL|CRITICAL)\b`),
		regexp.MustCompile(`(?i)\b(WARN|WARNING)\b`),
		regexp.MustCompile(`(?i)\b(INFO|DEBUG|TRACE)\b`),
		regexp.MustCompile(`^\s*\d{4}-\d{2}-\d{2}`),
		regexp.MustCompile(`^\s*\[\d{2}:\d{2}:\d{2}\]`),
		regexp.MustCompile(`^={3,}|^-{3,}`),
		regexp.MustCompile(`^\s*PASSED|^\s*FAILED|^\s*SKIPPED`),
		regexp.MustCompile(`^npm ERR!|^yarn error|^cargo error`),
		regexp.MustCompile(`Traceback \(most recent call last\)`),
		regexp.MustCompile(`^\s*at\s+[\w.$]+\(`),
	}

	// HTML patterns.
	htmlDoctypePattern    = regexp.MustCompile(`(?i)^\s*<!doctype\s+html`)
	htmlTagPattern        = regexp.MustCompile(`(?i)<html[\s>]`)
	htmlHeadPattern       = regexp.MustCompile(`(?i)<head[\s>]`)
	htmlBodyPattern       = regexp.MustCompile(`(?i)<body[\s>]`)
	htmlStructuralPattern = regexp.MustCompile(`(?i)<(div|span|script|style|link|meta|nav|header|footer|aside|article|section|main)[\s>]`)
)

type codeLanguage struct {
	name     string
	patterns []*regexp.Regexp
}

var codeLanguages = []codeLanguage{
	{"python", pythonPatterns},
	{"javascript", javascriptPatterns},
	{"typescript", typescriptPatterns},
	{"go", goPatterns},
	{"rust", rustPatterns},
	{"java", javaPatterns},
}

// DetectContentType analyzes text and returns the most likely content type.
// Port of Rust detect_content_type().
func DetectContentType(text string) DetectionResult {
	if len(text) == 0 || len(strings.TrimSpace(text)) == 0 {
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

func tryDetectJSON(text string) (DetectionResult, bool) {
	trimmed := strings.TrimSpace(text)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return DetectionResult{}, false
	}

	var arr []json.RawMessage
	if err := json.Unmarshal([]byte(trimmed), &arr); err != nil {
		return DetectionResult{}, false
	}

	itemCount := len(arr)
	isDictArray := false
	if itemCount > 0 {
		isDictArray = true
		for _, raw := range arr {
			var obj map[string]interface{}
			if err := json.Unmarshal(raw, &obj); err != nil {
				isDictArray = false
				break
			}
		}
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

func tryDetectDiff(text string) (DetectionResult, bool) {
	lines := strings.Split(text, "\n")
	limit := len(lines)
	if limit > 500 {
		limit = 500
	}

	var headerMatches, changeMatches uint32
	for _, line := range lines[:limit] {
		if diffHeaderPattern.MatchString(line) {
			headerMatches++
		}
		if diffChangePattern.MatchString(line) {
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

	hasDoctype := htmlDoctypePattern.MatchString(sample)
	hasHTMLTag := htmlTagPattern.MatchString(sample)
	hasHead := htmlHeadPattern.MatchString(sample)
	hasBody := htmlBodyPattern.MatchString(sample)
	structuralMatches := uint32(len(htmlStructuralPattern.FindAllStringIndex(sample, -1)))

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
	lines := strings.Split(text, "\n")
	limit := len(lines)
	if limit > 100 {
		limit = 100
	}
	lines = lines[:limit]

	if len(lines) == 0 {
		return DetectionResult{}, false
	}

	var matchingLines uint32
	var nonEmptyLines uint32
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		nonEmptyLines++
		if searchResultPattern.MatchString(line) {
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
	lines := strings.Split(text, "\n")
	limit := len(lines)
	if limit > 200 {
		limit = 200
	}
	lines = lines[:limit]

	if len(lines) == 0 {
		return DetectionResult{}, false
	}

	var patternMatches, errorMatches uint32
	var nonEmptyLines uint32
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		nonEmptyLines++
		for i, pattern := range logPatterns {
			if pattern.MatchString(line) {
				patternMatches++
				if i < 2 {
					errorMatches++
				}
				break
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
	lines := strings.Split(text, "\n")
	limit := len(lines)
	if limit > 100 {
		limit = 100
	}
	lines = lines[:limit]

	if len(lines) == 0 {
		return DetectionResult{}, false
	}

	// Track scores in first-match insertion order (matching Python dict semantics).
	type langScore struct {
		name  string
		score uint32
	}
	var languageScores []langScore

	for _, line := range lines {
		for _, cl := range codeLanguages {
			matched := false
			for _, pattern := range cl.patterns {
				if pattern.MatchString(line) {
					matched = true
					break
				}
			}
			if matched {
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
				break
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

	var nonEmptyLines uint32
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			nonEmptyLines++
		}
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
