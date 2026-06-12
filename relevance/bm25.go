package relevance

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
)

// tokenPattern matches tokens in order: UUID first (so hex-string IDs
// aren't broken), then numeric IDs (4+ digits with word boundaries),
// then alphanumeric fallback.
var tokenPattern = regexp.MustCompile(
	`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}|\b\d{4,}\b|[a-zA-Z0-9_]+`,
)

// Tokenize splits text into tokens, lowercased, preserving UUIDs and
// numeric IDs as single tokens.
func Tokenize(text string) []string {
	if text == "" {
		return nil
	}
	lower := strings.ToLower(text)
	matches := tokenPattern.FindAllString(lower, -1)
	return matches
}

// BM25Scorer implements BM25 keyword relevance scoring.
// Zero ML dependencies: pure regex tokenization + arithmetic.
type BM25Scorer struct {
	K1             float64
	B              float64
	NormalizeScore bool
	MaxScore       float64
}

// NewBM25Scorer creates a BM25Scorer with default parameters.
// Defaults match the Rust source: k1=1.5, b=0.75.
func NewBM25Scorer() *BM25Scorer {
	return &BM25Scorer{
		K1:             1.5,
		B:              0.75,
		NormalizeScore: true,
		MaxScore:       10.0,
	}
}

// NewBM25ScorerCustom creates a BM25Scorer with custom parameters.
func NewBM25ScorerCustom(k1, b float64) *BM25Scorer {
	return &BM25Scorer{
		K1:             k1,
		B:              b,
		NormalizeScore: true,
		MaxScore:       10.0,
	}
}

// bm25Score computes the raw BM25 score for a single (doc, query) pair.
// Returns (raw_score, matched_terms).
func (s *BM25Scorer) bm25Score(docTokens []string, queryFreq map[string]int, avgDocLen float64) (float64, []string) {
	if len(docTokens) == 0 || len(queryFreq) == 0 {
		return 0.0, nil
	}

	docLen := float64(len(docTokens))
	avgdl := avgDocLen
	if avgdl <= 0.0 {
		if docLen > 0.0 {
			avgdl = docLen
		} else {
			avgdl = 1.0
		}
	}

	// Doc-side term frequency.
	docFreq := make(map[string]int)
	for _, t := range docTokens {
		docFreq[t]++
	}

	var score float64
	var matched []string
	idf := math.Log(2.0)

	// Sort query keys for deterministic output.
	keys := make([]string, 0, len(queryFreq))
	for k := range queryFreq {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, term := range keys {
		qf := queryFreq[term]
		f, ok := docFreq[term]
		if !ok {
			continue
		}
		matched = append(matched, term)

		ff := float64(f)
		numerator := ff * (s.K1 + 1.0)
		denominator := ff + s.K1*(1.0-s.B+s.B*docLen/avgdl)
		termScore := idf * numerator / denominator
		score += termScore * float64(qf)
	}

	return score, matched
}

// finalizeScore normalizes and applies the long-token bonus.
func (s *BM25Scorer) finalizeScore(raw float64, matched []string) float64 {
	normalized := raw
	if s.NormalizeScore {
		normalized = math.Min(raw/s.MaxScore, 1.0)
	}
	for _, t := range matched {
		if len(t) >= 8 {
			normalized = math.Min(normalized+0.3, 1.0)
			break
		}
	}
	return normalized
}

// Score implements RelevanceScorer for a single item.
func (s *BM25Scorer) Score(item, context string) RelevanceScore {
	itemTokens := Tokenize(item)
	contextTokens := Tokenize(context)

	queryFreq := make(map[string]int)
	for _, t := range contextTokens {
		queryFreq[t]++
	}

	raw, matched := s.bm25Score(itemTokens, queryFreq, 0.0)
	normalized := s.finalizeScore(raw, matched)

	var reason string
	switch len(matched) {
	case 0:
		reason = "BM25: no term matches"
	case 1:
		reason = fmt.Sprintf("BM25: matched '%s'", matched[0])
	default:
		preview := matched
		if len(preview) > 3 {
			preview = preview[:3]
		}
		suffix := ""
		if len(matched) > 3 {
			suffix = "..."
		}
		reason = fmt.Sprintf("BM25: matched %d terms (%s%s)", len(matched), strings.Join(preview, ", "), suffix)
	}

	// Cap matched_terms for readability.
	matchedCapped := matched
	if len(matchedCapped) > 10 {
		matchedCapped = matchedCapped[:10]
	}

	return NewRelevanceScore(normalized, reason, matchedCapped)
}

// ScoreBatch implements RelevanceScorer for a batch of items.
func (s *BM25Scorer) ScoreBatch(items []string, context string) []RelevanceScore {
	contextTokens := Tokenize(context)

	if len(contextTokens) == 0 {
		results := make([]RelevanceScore, len(items))
		for i := range items {
			results[i] = RelevanceScoreZero("BM25: empty context")
		}
		return results
	}

	queryFreq := make(map[string]int)
	for _, t := range contextTokens {
		queryFreq[t]++
	}

	// Pre-tokenize all items.
	allTokens := make([][]string, len(items))
	totalLen := 0
	for i, item := range items {
		allTokens[i] = Tokenize(item)
		totalLen += len(allTokens[i])
	}

	avgLen := float64(totalLen) / math.Max(float64(len(items)), 1.0)

	results := make([]RelevanceScore, len(items))
	for i, itemTokens := range allTokens {
		raw, matched := s.bm25Score(itemTokens, queryFreq, avgLen)
		normalized := s.finalizeScore(raw, matched)

		var reason string
		switch len(matched) {
		case 0:
			reason = "BM25: no matches"
		default:
			reason = fmt.Sprintf("BM25: %d terms", len(matched))
		}

		matchedCapped := matched
		if len(matchedCapped) > 5 {
			matchedCapped = matchedCapped[:5]
		}

		results[i] = NewRelevanceScore(normalized, reason, matchedCapped)
	}
	return results
}

// IsAvailable implements RelevanceScorer.
func (s *BM25Scorer) IsAvailable() bool {
	return true
}
