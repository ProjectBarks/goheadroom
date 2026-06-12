package relevance

import (
	"strings"
	"testing"
)

func TestTokenizeBasic(t *testing.T) {
	toks := Tokenize("hello world")
	if len(toks) != 2 {
		t.Fatalf("expected 2 tokens, got %d: %v", len(toks), toks)
	}
	if toks[0] != "hello" || toks[1] != "world" {
		t.Errorf("unexpected tokens: %v", toks)
	}
}

func TestTokenizeLowercases(t *testing.T) {
	toks := Tokenize("Hello WORLD")
	if len(toks) != 2 || toks[0] != "hello" || toks[1] != "world" {
		t.Errorf("expected lowercased: %v", toks)
	}
}

func TestTokenizePreservesUUIDs(t *testing.T) {
	toks := Tokenize("find 550e8400-e29b-41d4-a716-446655440000 fast")
	found := false
	for _, tok := range toks {
		if tok == "550e8400-e29b-41d4-a716-446655440000" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("UUID not preserved as single token: %v", toks)
	}
}

func TestTokenizePreservesNumericIDs(t *testing.T) {
	toks := Tokenize("user 12345 logged in 99 times")
	found := false
	for _, tok := range toks {
		if tok == "12345" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("numeric ID not found: %v", toks)
	}
}

func TestTokenizeHandlesPunctuation(t *testing.T) {
	toks := Tokenize("hello, world!")
	if len(toks) != 2 || toks[0] != "hello" || toks[1] != "world" {
		t.Errorf("punctuation not stripped: %v", toks)
	}
}

func TestTokenizeEmpty(t *testing.T) {
	toks := Tokenize("")
	if len(toks) != 0 {
		t.Errorf("expected empty, got %v", toks)
	}
}

func TestBM25NewDefault(t *testing.T) {
	s := NewBM25Scorer()
	if s.K1 != 1.5 {
		t.Errorf("expected k1=1.5, got %f", s.K1)
	}
	if s.B != 0.75 {
		t.Errorf("expected b=0.75, got %f", s.B)
	}
}

func TestBM25ExactMatch(t *testing.T) {
	s := NewBM25Scorer()
	r := s.Score("alice bob", "alice")
	if r.Score <= 0.0 {
		t.Errorf("expected positive score for exact match, got %f", r.Score)
	}
	if !strings.Contains(r.Reason, "alice") {
		t.Errorf("reason should mention matched term: %q", r.Reason)
	}
}

func TestBM25NoMatch(t *testing.T) {
	s := NewBM25Scorer()
	r := s.Score(`{"id": 1, "name": "alice"}`, "completely unrelated query")
	if r.Score != 0.0 {
		t.Errorf("expected 0.0 for no match, got %f", r.Score)
	}
	if len(r.MatchedTerms) != 0 {
		t.Errorf("expected no matched terms, got %v", r.MatchedTerms)
	}
}

func TestBM25MultipleTerms(t *testing.T) {
	s := NewBM25Scorer()
	r := s.Score("alice bob charlie", "alice bob")
	if r.Score <= 0.0 {
		t.Errorf("expected positive score, got %f", r.Score)
	}
	if len(r.MatchedTerms) < 2 {
		t.Errorf("expected at least 2 matched terms, got %v", r.MatchedTerms)
	}
}

func TestBM25HigherScoreForMoreMatches(t *testing.T) {
	s := NewBM25Scorer()
	single := s.Score(`{"name": "alice"}`, "alice")
	triple := s.Score(`{"a": "alice", "b": "alice", "c": "alice"}`, "alice")
	if triple.Score < single.Score {
		t.Errorf("more matches should not decrease score: triple=%f single=%f", triple.Score, single.Score)
	}
}

func TestBM25LongTokenBonus(t *testing.T) {
	s := NewBM25Scorer()
	short := s.Score(`{"x": "ab"}`, "ab")
	long := s.Score(`{"x": "abcdefgh"}`, "abcdefgh")
	if long.Score < short.Score {
		t.Errorf("long token should score at least as high: long=%f short=%f", long.Score, short.Score)
	}
	// Long match should get the +0.3 bonus.
	if long.Score < 0.3 {
		t.Errorf("long match should clear 0.3 with bonus: got %f", long.Score)
	}
}

func TestBM25CaseInsensitive(t *testing.T) {
	s := NewBM25Scorer()
	r := s.Score("ALICE BOB", "alice bob")
	if r.Score <= 0.0 {
		t.Errorf("expected case-insensitive match, got %f", r.Score)
	}
}

func TestBM25BatchScoring(t *testing.T) {
	s := NewBM25Scorer()
	items := []string{
		`{"name": "alice logged in"}`,
		`{"name": "system started"}`,
		`{"name": "bob logged out"}`,
	}
	scores := s.ScoreBatch(items, "alice login")
	if len(scores) != 3 {
		t.Fatalf("expected 3 scores, got %d", len(scores))
	}
	// Item 0 (alice + logged) should outrank item 1 (no match).
	if scores[0].Score <= scores[1].Score {
		t.Errorf("alice match should outrank: %f vs %f", scores[0].Score, scores[1].Score)
	}
}

func TestBM25CustomParams(t *testing.T) {
	s := NewBM25ScorerCustom(1.2, 0.5)
	if s.K1 != 1.2 {
		t.Errorf("expected k1=1.2, got %f", s.K1)
	}
	if s.B != 0.5 {
		t.Errorf("expected b=0.5, got %f", s.B)
	}
	r := s.Score("hello world", "hello")
	if r.Score <= 0.0 {
		t.Errorf("expected positive score with custom params, got %f", r.Score)
	}
}

func TestBM25UUIDMatching(t *testing.T) {
	s := NewBM25Scorer()
	item := `{"id": "550e8400-e29b-41d4-a716-446655440000", "name": "Alice"}`
	r := s.Score(item, "find record 550e8400-e29b-41d4-a716-446655440000")
	if r.Score < 0.3 {
		t.Errorf("UUID match should clear 0.3 with long-token bonus: got %f", r.Score)
	}
	uuidFound := false
	for _, term := range r.MatchedTerms {
		if strings.Contains(term, "550e8400") {
			uuidFound = true
			break
		}
	}
	if !uuidFound {
		t.Errorf("matched terms should include UUID: %v", r.MatchedTerms)
	}
}

func TestBM25BatchEmptyContext(t *testing.T) {
	s := NewBM25Scorer()
	scores := s.ScoreBatch([]string{"foo", "bar"}, "")
	if len(scores) != 2 {
		t.Fatalf("expected 2 scores, got %d", len(scores))
	}
	for _, sc := range scores {
		if sc.Score != 0.0 {
			t.Errorf("expected 0.0 for empty context, got %f", sc.Score)
		}
	}
}

func TestBM25IsAvailable(t *testing.T) {
	s := NewBM25Scorer()
	if !s.IsAvailable() {
		t.Error("BM25 should always be available")
	}
}
