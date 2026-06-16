package signals

import (
	"testing"
)

func TestKeywordConstants(t *testing.T) {
	if ErrorPriority != 0.95 {
		t.Errorf("expected 0.95, got %f", ErrorPriority)
	}
	if WarningPriority != 0.75 {
		t.Errorf("expected 0.75, got %f", WarningPriority)
	}
	if SecurityPriority != 0.85 {
		t.Errorf("expected 0.85, got %f", SecurityPriority)
	}
	if ImportancePriority != 0.6 {
		t.Errorf("expected 0.6, got %f", ImportancePriority)
	}
	if MarkdownPriority != 0.45 {
		t.Errorf("expected 0.45, got %f", MarkdownPriority)
	}
	if KeywordConfidence != 0.7 {
		t.Errorf("expected 0.7, got %f", KeywordConfidence)
	}
}

func TestDefaultRegistryHasCategories(t *testing.T) {
	r := NewDefaultKeywordRegistry()
	cats := r.Categories()
	if len(cats) < 4 {
		t.Errorf("expected at least 4 categories, got %d", len(cats))
	}
}

func TestRegistryKeywordsForCategory(t *testing.T) {
	r := NewDefaultKeywordRegistry()
	errorKws := r.KeywordsForCategory(ImportanceCategoryError)
	if len(errorKws) == 0 {
		t.Error("expected error keywords")
	}
	// Check the four keywords Python was missing.
	found := map[string]bool{}
	for _, kw := range errorKws {
		found[kw] = true
	}
	for _, expected := range []string{"abort", "timeout", "denied", "rejected"} {
		if !found[expected] {
			t.Errorf("expected error keyword %q", expected)
		}
	}
	// Check "token" is NOT in security set.
	secKws := r.KeywordsForCategory(ImportanceCategorySecurity)
	for _, kw := range secKws {
		if kw == "token" {
			t.Error("token should not be in security keywords")
		}
	}
}

func TestCustomRegistry(t *testing.T) {
	r := NewKeywordRegistry()
	r.AddKeyword(ImportanceCategoryError, "oops")
	kws := r.KeywordsForCategory(ImportanceCategoryError)
	if len(kws) != 1 || kws[0] != "oops" {
		t.Errorf("unexpected keywords: %v", kws)
	}
}

func TestDetectsErrorKeywords(t *testing.T) {
	d := NewKeywordDetector()
	s := d.DetectImportance("ERROR: connection refused", ImportanceContextSearch)
	if !s.HasMatch {
		t.Error("expected match")
	}
	if s.Category != ImportanceCategoryError {
		t.Errorf("expected Error, got %d", s.Category)
	}
	if s.Priority != ErrorPriority {
		t.Errorf("expected %f, got %f", ErrorPriority, s.Priority)
	}
}

func TestDetectsWarningKeywords(t *testing.T) {
	d := NewKeywordDetector()
	s := d.DetectImportance("warning: deprecated API", ImportanceContextSearch)
	if !s.HasMatch {
		t.Error("expected match")
	}
	if s.Category != ImportanceCategoryWarning {
		t.Errorf("expected Warning, got %d", s.Category)
	}
	// Warning should NOT fire in Diff context.
	s2 := d.DetectImportance("warning: deprecated API alone with no errors", ImportanceContextDiff)
	if s2.HasMatch && s2.Category == ImportanceCategoryWarning {
		t.Error("warning should not fire in Diff context")
	}
}

func TestDetectsSecurityKeywords(t *testing.T) {
	d := NewKeywordDetector()
	// Security fires in Diff context.
	s := d.DetectImportance("missing auth header", ImportanceContextDiff)
	if !s.HasMatch {
		t.Error("expected match")
	}
	if s.Category != ImportanceCategorySecurity {
		t.Errorf("expected Security, got %d", s.Category)
	}
}

func TestDetectsMarkdownHeadings(t *testing.T) {
	d := NewKeywordDetector()
	// Markdown prefix fires only in Text context.
	s := d.DetectImportance("# Section", ImportanceContextText)
	if !s.HasMatch {
		t.Error("expected match for markdown heading")
	}
	if s.Category != ImportanceCategoryMarkdown {
		t.Errorf("expected Markdown, got %d", s.Category)
	}
	// Should NOT fire in Diff context.
	s2 := d.DetectImportance("# Section", ImportanceContextDiff)
	if s2.HasMatch {
		t.Error("markdown should not fire in Diff context")
	}
}

func TestNoSignalsForPlainText(t *testing.T) {
	d := NewKeywordDetector()
	s := d.DetectImportance("the quick brown fox", ImportanceContextText)
	if s.HasMatch {
		t.Error("expected no match for plain text")
	}
	if s.Priority != 0.0 {
		t.Errorf("expected 0.0 priority, got %f", s.Priority)
	}
}

func TestCaseInsensitiveDetection(t *testing.T) {
	d := NewKeywordDetector()
	for _, line := range []string{
		"ERROR: something broke",
		"error: something broke",
		"Error: something broke",
	} {
		s := d.DetectImportance(line, ImportanceContextSearch)
		if !s.HasMatch {
			t.Errorf("expected match for %q", line)
		}
		if s.Category != ImportanceCategoryError {
			t.Errorf("expected Error for %q, got %d", line, s.Category)
		}
	}
}

func TestMultipleSignalsInOneLine(t *testing.T) {
	d := NewKeywordDetector()
	// Line with both error and importance keywords -- error should win
	// because universal automaton checks error first.
	s := d.DetectImportance("error: important note here", ImportanceContextText)
	if !s.HasMatch {
		t.Error("expected match")
	}
	// The first match in the universal automaton wins.
	if s.Category != ImportanceCategoryError {
		t.Errorf("expected Error (first match), got %d", s.Category)
	}
}

func TestDetectorName(t *testing.T) {
	d := NewKeywordDetector()
	if d.Name() != "keyword" {
		t.Errorf("expected 'keyword', got %q", d.Name())
	}
}

func TestDetectorImplementsInterface(t *testing.T) {
	var _ LineImportanceDetector = (*KeywordDetector)(nil)
}

func TestCustomRegistryWithDetector(t *testing.T) {
	r := NewKeywordRegistry()
	r.AddKeyword(ImportanceCategoryError, "oops")
	d := NewKeywordDetectorWithRegistry(r)
	s := d.DetectImportance("something oops happened", ImportanceContextSearch)
	if !s.HasMatch {
		t.Error("expected match for custom keyword")
	}
	if s.Category != ImportanceCategoryError {
		t.Errorf("expected Error, got %d", s.Category)
	}
}

func TestAllSignalsHaveConfidence(t *testing.T) {
	d := NewKeywordDetector()
	tests := []struct {
		line string
		ctx  ImportanceContext
	}{
		{"ERROR: something", ImportanceContextSearch},
		{"warning: deprecated", ImportanceContextText},
		{"auth failure", ImportanceContextDiff},
		{"# Heading", ImportanceContextText},
		{"important note", ImportanceContextSearch},
	}
	for _, tc := range tests {
		s := d.DetectImportance(tc.line, tc.ctx)
		if s.HasMatch && s.Confidence != KeywordConfidence {
			t.Errorf("line %q: expected confidence %f, got %f", tc.line, KeywordConfidence, s.Confidence)
		}
	}
}
