package relevance

import "testing"

func TestRelevanceScoreCreation(t *testing.T) {
	s := NewRelevanceScore(0.5, "test", []string{"term"})
	if s.Score != 0.5 {
		t.Errorf("expected 0.5, got %f", s.Score)
	}
	if s.Reason != "test" {
		t.Errorf("expected 'test', got %q", s.Reason)
	}
	if len(s.MatchedTerms) != 1 || s.MatchedTerms[0] != "term" {
		t.Errorf("unexpected matched terms: %v", s.MatchedTerms)
	}
}

func TestRelevanceScoreZero(t *testing.T) {
	s := RelevanceScoreZero("no match")
	if s.Score != 0.0 {
		t.Errorf("expected 0.0, got %f", s.Score)
	}
	if s.Reason != "no match" {
		t.Errorf("expected 'no match', got %q", s.Reason)
	}
	if len(s.MatchedTerms) != 0 {
		t.Errorf("expected empty terms, got %v", s.MatchedTerms)
	}
}

func TestRelevanceScoreClampHigh(t *testing.T) {
	s := NewRelevanceScore(1.5, "", nil)
	if s.Score != 1.0 {
		t.Errorf("expected clamped to 1.0, got %f", s.Score)
	}
}

func TestRelevanceScoreClampLow(t *testing.T) {
	s := NewRelevanceScore(-0.5, "", nil)
	if s.Score != 0.0 {
		t.Errorf("expected clamped to 0.0, got %f", s.Score)
	}
}

func TestRelevanceScoreMerge(t *testing.T) {
	a := NewRelevanceScore(0.3, "BM25", []string{"alice", "bob"})
	b := NewRelevanceScore(0.7, "Embedding", []string{"bob", "charlie"})
	merged := a.Merge(b)

	// Max of scores.
	if merged.Score != 0.7 {
		t.Errorf("expected 0.7, got %f", merged.Score)
	}
	// Joined reasons.
	if merged.Reason != "BM25 + Embedding" {
		t.Errorf("expected joined reason, got %q", merged.Reason)
	}
	// Deduplicated terms.
	if len(merged.MatchedTerms) != 3 {
		t.Errorf("expected 3 deduplicated terms, got %v", merged.MatchedTerms)
	}
	// Check order: alice, bob, charlie.
	expected := []string{"alice", "bob", "charlie"}
	for i, term := range expected {
		if i >= len(merged.MatchedTerms) || merged.MatchedTerms[i] != term {
			t.Errorf("expected term %q at index %d, got %v", term, i, merged.MatchedTerms)
		}
	}
}

func TestRelevanceScorerInterface(t *testing.T) {
	// Verify the interface is usable with a stub.
	stub := &stubScorer{}
	var _ RelevanceScorer = stub

	s := stub.Score("item", "context")
	if s.Score != 0.42 {
		t.Errorf("expected 0.42, got %f", s.Score)
	}

	batch := stub.ScoreBatch([]string{"a", "b"}, "ctx")
	if len(batch) != 2 {
		t.Errorf("expected 2 scores, got %d", len(batch))
	}

	if !stub.IsAvailable() {
		t.Error("expected available")
	}
}

type stubScorer struct{}

func (s *stubScorer) Score(item, context string) RelevanceScore {
	return NewRelevanceScore(0.42, "stub", nil)
}

func (s *stubScorer) ScoreBatch(items []string, context string) []RelevanceScore {
	results := make([]RelevanceScore, len(items))
	for i := range items {
		results[i] = s.Score(items[i], context)
	}
	return results
}

func (s *stubScorer) IsAvailable() bool { return true }
