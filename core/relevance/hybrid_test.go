package relevance

import "testing"

func TestIsUUID(t *testing.T) {
	if !IsUUID("find 550e8400-e29b-41d4-a716-446655440000") {
		t.Error("expected UUID match")
	}
	if IsUUID("no uuid here") {
		t.Error("expected no UUID match")
	}
}

func TestIsHostname(t *testing.T) {
	if !IsHostname("status of api.example.com") {
		t.Error("expected hostname match")
	}
	if IsHostname("no hostname") {
		t.Error("expected no hostname match")
	}
}

func TestIsEmail(t *testing.T) {
	if !IsEmail("contact user@example.com") {
		t.Error("expected email match")
	}
	if IsEmail("no email here") {
		t.Error("expected no email match")
	}
}

func TestAlphaClampMin(t *testing.T) {
	c := ClampAlpha(0.1)
	if c != AlphaMin {
		t.Errorf("expected %f, got %f", AlphaMin, c)
	}
}

func TestAlphaClampMax(t *testing.T) {
	c := ClampAlpha(1.0)
	if c != AlphaMax {
		t.Errorf("expected %f, got %f", AlphaMax, c)
	}
}

func TestAlphaConstants(t *testing.T) {
	if AlphaMin != 0.3 {
		t.Errorf("expected 0.3, got %f", AlphaMin)
	}
	if AlphaMax != 0.9 {
		t.Errorf("expected 0.9, got %f", AlphaMax)
	}
}

func TestComputeAlphaForUUIDQuery(t *testing.T) {
	alpha := ComputeAdaptiveAlpha(0.5, "find 550e8400-e29b-41d4-a716-446655440000")
	if alpha < 0.85 {
		t.Errorf("UUID query should push alpha >= 0.85: got %f", alpha)
	}
}

func TestComputeAlphaForNaturalLanguage(t *testing.T) {
	alpha := ComputeAdaptiveAlpha(0.5, "show me failed requests")
	if alpha != 0.5 {
		t.Errorf("natural language query should keep base alpha: got %f", alpha)
	}
}

func TestComputeAlphaForEmailQuery(t *testing.T) {
	alpha := ComputeAdaptiveAlpha(0.5, "messages from user@example.com")
	if alpha < 0.6 {
		t.Errorf("email query should push alpha >= 0.6: got %f", alpha)
	}
}

func TestHybridScorerCreation(t *testing.T) {
	h := NewHybridScorer(NewBM25Scorer(), NewEmbeddingScorerStub())
	if h.BaseAlpha != 0.5 {
		t.Errorf("expected base alpha 0.5, got %f", h.BaseAlpha)
	}
	if !h.Adaptive {
		t.Error("expected adaptive=true")
	}
}

func TestHybridScorerBM25Only(t *testing.T) {
	h := NewHybridScorerBM25Only()
	if h.HasEmbeddingSupport() {
		t.Error("BM25-only should not have embedding support")
	}

	// Single match should get boosted to >= 0.3.
	r := h.Score(`{"name": "alice"}`, "alice")
	if r.Score < 0.3 {
		t.Errorf("single match should clear 0.3 in fallback: got %f", r.Score)
	}

	// No match should stay at 0.
	r2 := h.Score(`{"id": 1}`, "completely unrelated query")
	if r2.Score != 0.0 {
		t.Errorf("no match should be 0.0: got %f", r2.Score)
	}
}

func TestHybridScorerNoMatchBM25Only(t *testing.T) {
	h := NewHybridScorerBM25Only()
	r := h.Score(`{"id": 1}`, "completely unrelated query")
	if r.Score != 0.0 {
		t.Errorf("expected 0.0, got %f", r.Score)
	}
}

func TestHybridScorerBatch(t *testing.T) {
	h := NewHybridScorerBM25Only()
	items := []string{
		`{"name": "alice"}`,
		`{"name": "bob"}`,
	}
	scores := h.ScoreBatch(items, "alice")
	if len(scores) != 2 {
		t.Fatalf("expected 2 scores, got %d", len(scores))
	}
	// First item should score higher (has "alice").
	if scores[0].Score <= scores[1].Score {
		t.Errorf("alice should outrank bob: %f vs %f", scores[0].Score, scores[1].Score)
	}

	// Empty batch.
	empty := h.ScoreBatch(nil, "anything")
	if len(empty) != 0 {
		t.Errorf("expected empty, got %d", len(empty))
	}
}

func TestHybridScorerIsAvailable(t *testing.T) {
	h := NewHybridScorerBM25Only()
	if !h.IsAvailable() {
		t.Error("hybrid should always be available")
	}
}

func TestHybridScorerMultiMatchBoost(t *testing.T) {
	h := NewHybridScorerBM25Only()
	r := h.Score(
		`{"name": "alice", "role": "admin", "team": "engineering"}`,
		"alice admin engineering",
	)
	if r.Score < 0.5 {
		t.Errorf("multi-match should clear 0.5 in fallback: got %f", r.Score)
	}
}

func TestHybridScorerImplementsInterface(t *testing.T) {
	var _ RelevanceScorer = (*HybridScorer)(nil)
}
