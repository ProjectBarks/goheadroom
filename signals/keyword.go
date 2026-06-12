package signals

import (
	"strings"

	ahocorasick "github.com/petar-dambovaliev/aho-corasick"
)

// Priority constants for each importance category.
const (
	ErrorPriority      = 0.95
	WarningPriority    = 0.75
	SecurityPriority   = 0.85
	ImportancePriority = 0.6
	MarkdownPriority   = 0.45
	KeywordConfidence  = 0.7
)

// KeywordRegistry holds the keyword lists for each importance category.
type KeywordRegistry struct {
	entries map[ImportanceCategory][]string
}

// NewKeywordRegistry creates an empty registry.
func NewKeywordRegistry() *KeywordRegistry {
	return &KeywordRegistry{
		entries: make(map[ImportanceCategory][]string),
	}
}

// AddKeyword adds a keyword to the given category.
func (r *KeywordRegistry) AddKeyword(category ImportanceCategory, keyword string) {
	r.entries[category] = append(r.entries[category], keyword)
}

// KeywordsForCategory returns the keywords for the given category.
func (r *KeywordRegistry) KeywordsForCategory(category ImportanceCategory) []string {
	return r.entries[category]
}

// Categories returns all categories that have keywords.
func (r *KeywordRegistry) Categories() []ImportanceCategory {
	cats := make([]ImportanceCategory, 0, len(r.entries))
	for c := range r.entries {
		cats = append(cats, c)
	}
	return cats
}

// NewDefaultKeywordRegistry returns the default Headroom keyword set,
// ported from the Rust KeywordRegistry::default_set(). Includes the
// four error keywords Python's regex was missing (abort, timeout,
// denied, rejected) and excludes "token" from the security set.
func NewDefaultKeywordRegistry() *KeywordRegistry {
	r := NewKeywordRegistry()

	// Error keywords
	for _, kw := range []string{
		"error", "exception", "fail", "failed", "failure",
		"fatal", "critical", "crash", "panic",
		"abort", "timeout", "denied", "rejected",
	} {
		r.AddKeyword(ImportanceCategoryError, kw)
	}

	// Warning keywords
	for _, kw := range []string{"warn", "warning"} {
		r.AddKeyword(ImportanceCategoryWarning, kw)
	}

	// Importance keywords
	for _, kw := range []string{
		"important", "note", "todo", "fixme",
		"hack", "xxx", "bug", "fix",
	} {
		r.AddKeyword(ImportanceCategoryImportance, kw)
	}

	// Security keywords (no "token" -- it false-positives on LLM token counts)
	for _, kw := range []string{"security", "auth", "password", "secret"} {
		r.AddKeyword(ImportanceCategorySecurity, kw)
	}

	// Markdown prefixes stored as ImportanceCategoryMarkdown
	for _, kw := range []string{"# ", "## ", "### ", "#### ", "**", "> "} {
		r.AddKeyword(ImportanceCategoryMarkdown, kw)
	}

	return r
}

// priorityFor returns the priority constant for the given category.
func priorityFor(category ImportanceCategory) float64 {
	switch category {
	case ImportanceCategoryError:
		return ErrorPriority
	case ImportanceCategoryWarning:
		return WarningPriority
	case ImportanceCategorySecurity:
		return SecurityPriority
	case ImportanceCategoryImportance:
		return ImportancePriority
	case ImportanceCategoryMarkdown:
		return MarkdownPriority
	default:
		return 0.0
	}
}

// categoryAutomaton wraps an aho-corasick automaton with a parallel
// category lookup table. Built case-insensitively with word-boundary
// matching via MatchOnlyWholeWords.
type categoryAutomaton struct {
	automaton  ahocorasick.AhoCorasick
	categories []ImportanceCategory
}

// buildAutomaton builds a category automaton from category-keyword pairs.
// The automaton uses case-insensitive matching and whole-word boundaries.
func buildAutomaton(entries []struct {
	cat      ImportanceCategory
	keywords []string
}) categoryAutomaton {
	var patterns []string
	var categories []ImportanceCategory

	for _, entry := range entries {
		for _, kw := range entry.keywords {
			patterns = append(patterns, strings.ToLower(kw))
			categories = append(categories, entry.cat)
		}
	}

	builder := ahocorasick.NewAhoCorasickBuilder(ahocorasick.Opts{
		AsciiCaseInsensitive: true,
		MatchOnlyWholeWords:  true,
		MatchKind:            ahocorasick.LeftMostLongestMatch,
		DFA:                  false,
	})
	ac := builder.Build(patterns)

	return categoryAutomaton{
		automaton:  ac,
		categories: categories,
	}
}

// firstWordMatch returns the first matching category via whole-word match.
func (ca *categoryAutomaton) firstWordMatch(line string) (ImportanceCategory, bool) {
	lower := strings.ToLower(line)
	iter := ca.automaton.Iter(lower)
	m := iter.Next()
	if m == nil {
		return 0, false
	}
	return ca.categories[m.Pattern()], true
}

// KeywordDetector is a pattern-based LineImportanceDetector backed by
// aho-corasick. Replaces the Python error_detection.py regex registry.
type KeywordDetector struct {
	registry *KeywordRegistry
	// Categories that fire across all contexts (error/importance).
	universal categoryAutomaton
	// Warning fires in Search/Log/Text contexts but is omitted in Diff.
	warning categoryAutomaton
	// Security fires in Diff context only.
	security categoryAutomaton
}

// NewKeywordDetector creates a KeywordDetector with the default keyword set.
func NewKeywordDetector() *KeywordDetector {
	return NewKeywordDetectorWithRegistry(NewDefaultKeywordRegistry())
}

// NewKeywordDetectorWithRegistry creates a KeywordDetector with a custom registry.
func NewKeywordDetectorWithRegistry(registry *KeywordRegistry) *KeywordDetector {
	errorKw := registry.KeywordsForCategory(ImportanceCategoryError)
	importanceKw := registry.KeywordsForCategory(ImportanceCategoryImportance)
	warningKw := registry.KeywordsForCategory(ImportanceCategoryWarning)
	securityKw := registry.KeywordsForCategory(ImportanceCategorySecurity)

	universal := buildAutomaton([]struct {
		cat      ImportanceCategory
		keywords []string
	}{
		{ImportanceCategoryError, errorKw},
		{ImportanceCategoryImportance, importanceKw},
	})

	warning := buildAutomaton([]struct {
		cat      ImportanceCategory
		keywords []string
	}{
		{ImportanceCategoryWarning, warningKw},
	})

	sec := buildAutomaton([]struct {
		cat      ImportanceCategory
		keywords []string
	}{
		{ImportanceCategorySecurity, securityKw},
	})

	return &KeywordDetector{
		registry: registry,
		universal: universal,
		warning:   warning,
		security:  sec,
	}
}

// DetectImportance implements LineImportanceDetector.
func (kd *KeywordDetector) DetectImportance(line string, ctx ImportanceContext) ImportanceSignal {
	// Check universal patterns first (error + importance).
	if cat, ok := kd.universal.firstWordMatch(line); ok {
		return MatchedSignal(cat, priorityFor(cat), KeywordConfidence)
	}

	// Context-specific patterns.
	switch ctx {
	case ImportanceContextDiff:
		// Security fires in Diff context only.
		if cat, ok := kd.security.firstWordMatch(line); ok {
			return MatchedSignal(cat, priorityFor(cat), KeywordConfidence)
		}
	case ImportanceContextText, ImportanceContextSearch, ImportanceContextLog:
		// Warning fires in non-Diff contexts.
		if cat, ok := kd.warning.firstWordMatch(line); ok {
			return MatchedSignal(cat, priorityFor(cat), KeywordConfidence)
		}
	}

	// Markdown structural prefixes only count in Text context.
	if ctx == ImportanceContextText {
		mdPrefixes := kd.registry.KeywordsForCategory(ImportanceCategoryMarkdown)
		for _, prefix := range mdPrefixes {
			if strings.HasPrefix(line, prefix) {
				return MatchedSignal(ImportanceCategoryMarkdown, MarkdownPriority, KeywordConfidence)
			}
		}
	}

	return NeutralSignal()
}

// Name implements LineImportanceDetector.
func (kd *KeywordDetector) Name() string {
	return "keyword"
}
