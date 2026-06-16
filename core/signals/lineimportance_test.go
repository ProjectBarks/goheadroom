package signals

import "testing"

func TestImportanceContextValues(t *testing.T) {
	if ImportanceContextText != 0 {
		t.Errorf("expected 0, got %d", ImportanceContextText)
	}
	if ImportanceContextSearch != 1 {
		t.Errorf("expected 1, got %d", ImportanceContextSearch)
	}
	if ImportanceContextDiff != 2 {
		t.Errorf("expected 2, got %d", ImportanceContextDiff)
	}
	if ImportanceContextLog != 3 {
		t.Errorf("expected 3, got %d", ImportanceContextLog)
	}
}

func TestImportanceCategoryValues(t *testing.T) {
	if ImportanceCategoryError != 0 {
		t.Errorf("expected 0, got %d", ImportanceCategoryError)
	}
	if ImportanceCategoryWarning != 1 {
		t.Errorf("expected 1, got %d", ImportanceCategoryWarning)
	}
	if ImportanceCategorySecurity != 3 {
		t.Errorf("expected 3, got %d", ImportanceCategorySecurity)
	}
	if ImportanceCategoryMarkdown != 4 {
		t.Errorf("expected 4, got %d", ImportanceCategoryMarkdown)
	}
}

func TestImportanceSignalStruct(t *testing.T) {
	s := ImportanceSignal{Category: ImportanceCategoryError, Priority: 0.95, Confidence: 0.7, Matched: "error"}
	if s.Priority != 0.95 {
		t.Errorf("expected 0.95, got %f", s.Priority)
	}
}

func TestNeutralSignal(t *testing.T) {
	s := NeutralSignal()
	if s.HasMatch {
		t.Error("neutral signal should not have match")
	}
	if s.Priority != 0.0 {
		t.Errorf("expected 0.0 priority, got %f", s.Priority)
	}
	if s.Confidence != 0.0 {
		t.Errorf("expected 0.0 confidence, got %f", s.Confidence)
	}
}

func TestMatchedSignal(t *testing.T) {
	s := MatchedSignal(ImportanceCategoryError, 0.95, 0.7)
	if !s.HasMatch {
		t.Error("matched signal should have match")
	}
	if s.Category != ImportanceCategoryError {
		t.Errorf("expected Error category, got %d", s.Category)
	}
	if s.Priority != 0.95 {
		t.Errorf("expected 0.95, got %f", s.Priority)
	}
	if s.Confidence != 0.7 {
		t.Errorf("expected 0.7, got %f", s.Confidence)
	}
}
