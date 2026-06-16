package signals

import "testing"

func TestEscalateThreshold(t *testing.T) {
	if EscalateThreshold != 0.7 {
		t.Errorf("expected 0.7, got %f", EscalateThreshold)
	}
}

// alwaysFiresHigh is a synthetic high-confidence detector for testing
// short-circuit behavior.
type alwaysFiresHigh struct{}

func (a *alwaysFiresHigh) DetectImportance(line string, ctx ImportanceContext) ImportanceSignal {
	return MatchedSignal(ImportanceCategorySecurity, 0.99, 0.95)
}
func (a *alwaysFiresHigh) Name() string { return "always-high" }

// alwaysFiresLow is a synthetic low-confidence detector. Confidence 0.5
// is below the escalate threshold so Tiered MUST fall through.
type alwaysFiresLow struct{}

func (a *alwaysFiresLow) DetectImportance(line string, ctx ImportanceContext) ImportanceSignal {
	return MatchedSignal(ImportanceCategoryImportance, 0.4, 0.5)
}
func (a *alwaysFiresLow) Name() string { return "always-low" }

func TestTieredSingleItem(t *testing.T) {
	tiered := NewTiered().With(NewKeywordDetector())
	s := tiered.Evaluate("ERROR: connection refused", ImportanceContextSearch)
	if !s.HasMatch {
		t.Error("expected match")
	}
	if s.Category != ImportanceCategoryError {
		t.Errorf("expected Error, got %d", s.Category)
	}
}

func TestTieredEscalation(t *testing.T) {
	// High-confidence tier should short-circuit before keyword detector.
	tiered := NewTiered().With(&alwaysFiresHigh{}).With(NewKeywordDetector())
	s := tiered.Evaluate("ERROR: connection refused", ImportanceContextDiff)
	// alwaysFiresHigh asserts Security; if keyword detector ran it
	// would have asserted Error.
	if s.Category != ImportanceCategorySecurity {
		t.Errorf("expected Security (high-confidence tier), got %d", s.Category)
	}
	if s.Confidence != 0.95 {
		t.Errorf("expected 0.95 confidence, got %f", s.Confidence)
	}
}

func TestTieredNoEscalation(t *testing.T) {
	// Low-confidence tier falls through to keyword detector.
	tiered := NewTiered().With(&alwaysFiresLow{}).With(NewKeywordDetector())
	s := tiered.Evaluate("ERROR: connection refused", ImportanceContextDiff)
	if s.Category != ImportanceCategoryError {
		t.Errorf("expected Error (keyword tier), got %d", s.Category)
	}
	if s.Confidence != KeywordConfidence {
		t.Errorf("expected %f confidence, got %f", KeywordConfidence, s.Confidence)
	}
}

func TestTieredEmpty(t *testing.T) {
	tiered := NewTiered()
	s := tiered.Evaluate("anything", ImportanceContextText)
	if s.HasMatch {
		t.Error("empty stack should return neutral")
	}
	if s.Confidence != 0.0 {
		t.Errorf("expected 0.0, got %f", s.Confidence)
	}
}

func TestTieredNoMatchReturnsBestSeen(t *testing.T) {
	// Low-confidence tier fires on any input; keyword detector returns
	// neutral for plain text. Best-seen should be the low-confidence signal.
	tiered := NewTiered().With(&alwaysFiresLow{}).With(NewKeywordDetector())
	s := tiered.Evaluate("the quick brown fox", ImportanceContextText)
	if s.Category != ImportanceCategoryImportance {
		t.Errorf("expected Importance (best-seen from low tier), got %d", s.Category)
	}
	if s.Confidence != 0.5 {
		t.Errorf("expected 0.5 confidence, got %f", s.Confidence)
	}
}
