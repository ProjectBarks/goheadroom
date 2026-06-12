package integration

import (
	"testing"

	"github.com/uber/goheadroom/relevance"
	"github.com/uber/goheadroom/signals"
)

func TestSignalsKeywordDetectorIntegration(t *testing.T) {
	d := signals.NewKeywordDetector()

	tests := []struct {
		line     string
		ctx      signals.ImportanceContext
		wantCat  signals.ImportanceCategory
		wantMatch bool
	}{
		{"ERROR: connection refused", signals.ImportanceContextSearch, signals.ImportanceCategoryError, true},
		{"warning: deprecated API", signals.ImportanceContextText, signals.ImportanceCategoryWarning, true},
		{"missing auth header", signals.ImportanceContextDiff, signals.ImportanceCategorySecurity, true},
		{"# Section heading", signals.ImportanceContextText, signals.ImportanceCategoryMarkdown, true},
		{"the quick brown fox", signals.ImportanceContextText, 0, false},
	}

	for _, tc := range tests {
		s := d.DetectImportance(tc.line, tc.ctx)
		if s.HasMatch != tc.wantMatch {
			t.Errorf("line %q: hasMatch=%v, want %v", tc.line, s.HasMatch, tc.wantMatch)
		}
		if s.HasMatch && s.Category != tc.wantCat {
			t.Errorf("line %q: category=%d, want %d", tc.line, s.Category, tc.wantCat)
		}
	}
}

func TestSignalsTieredIntegration(t *testing.T) {
	tiered := signals.NewTiered().With(signals.NewKeywordDetector())
	s := tiered.Evaluate("FATAL: timeout connecting upstream", signals.ImportanceContextDiff)
	if !s.HasMatch {
		t.Error("expected match from tiered combinator")
	}
	if s.Category != signals.ImportanceCategoryError {
		t.Errorf("expected Error, got %d", s.Category)
	}
}

func TestRelevanceBM25Integration(t *testing.T) {
	scorer := relevance.NewBM25Scorer()

	r := scorer.Score(`{"id": "550e8400-e29b-41d4-a716-446655440000"}`, "550e8400-e29b-41d4-a716-446655440000")
	if r.Score < 0.3 {
		t.Errorf("UUID match should score >= 0.3, got %f", r.Score)
	}

	r2 := scorer.Score(`{"name": "alice"}`, "completely unrelated")
	if r2.Score != 0.0 {
		t.Errorf("no match should be 0.0, got %f", r2.Score)
	}
}

func TestRelevanceHybridIntegration(t *testing.T) {
	h := relevance.NewHybridScorerBM25Only()
	if !h.IsAvailable() {
		t.Error("hybrid should always be available")
	}

	r := h.Score(`{"name": "alice", "role": "admin"}`, "alice admin")
	if r.Score < 0.3 {
		t.Errorf("multi-match should clear 0.3, got %f", r.Score)
	}

	batch := h.ScoreBatch([]string{`{"name": "alice"}`, `{"name": "bob"}`}, "alice")
	if len(batch) != 2 {
		t.Fatalf("expected 2 scores, got %d", len(batch))
	}
	if batch[0].Score <= batch[1].Score {
		t.Errorf("alice should outrank bob: %f vs %f", batch[0].Score, batch[1].Score)
	}
}

func TestCosineSimilarityIntegration(t *testing.T) {
	v := []float32{1.0, 2.0, 3.0}
	sim := relevance.CosineSimilarity(v, v)
	if sim < 0.99 {
		t.Errorf("identical vectors should have sim ~1.0, got %f", sim)
	}
}
