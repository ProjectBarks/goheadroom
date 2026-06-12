// Package relevance provides relevance scoring for content compression.
//
// Direct port of headroom-core/src/relevance/. Provides BM25 keyword
// matching, cosine-similarity embedding scoring, and a hybrid combinator
// with adaptive alpha tuning.
package relevance

// RelevanceScore holds a relevance score with explainability fields.
// Score is clamped to [0.0, 1.0].
type RelevanceScore struct {
	Score        float64
	Reason       string
	MatchedTerms []string
}

// NewRelevanceScore creates a score, clamping to [0.0, 1.0].
func NewRelevanceScore(score float64, reason string, matchedTerms []string) RelevanceScore {
	if score < 0.0 {
		score = 0.0
	}
	if score > 1.0 {
		score = 1.0
	}
	if matchedTerms == nil {
		matchedTerms = []string{}
	}
	return RelevanceScore{
		Score:        score,
		Reason:       reason,
		MatchedTerms: matchedTerms,
	}
}

// RelevanceScoreZero returns a zero-score with the given reason.
func RelevanceScoreZero(reason string) RelevanceScore {
	return NewRelevanceScore(0.0, reason, nil)
}

// Merge combines two RelevanceScores. Uses max of scores, joins
// reasons with " + ", and deduplicates matched terms.
func (s RelevanceScore) Merge(other RelevanceScore) RelevanceScore {
	score := s.Score
	if other.Score > score {
		score = other.Score
	}

	reason := s.Reason
	if other.Reason != "" {
		if reason != "" {
			reason = reason + " + " + other.Reason
		} else {
			reason = other.Reason
		}
	}

	// Deduplicate terms.
	seen := make(map[string]bool)
	var terms []string
	for _, t := range s.MatchedTerms {
		if !seen[t] {
			seen[t] = true
			terms = append(terms, t)
		}
	}
	for _, t := range other.MatchedTerms {
		if !seen[t] {
			seen[t] = true
			terms = append(terms, t)
		}
	}
	if terms == nil {
		terms = []string{}
	}

	return NewRelevanceScore(score, reason, terms)
}

// RelevanceScorer is the interface that every relevance scorer implements.
type RelevanceScorer interface {
	// Score scores a single item against the context.
	Score(item, context string) RelevanceScore

	// ScoreBatch scores a batch of items. Default implementations
	// should delegate to per-item Score.
	ScoreBatch(items []string, context string) []RelevanceScore

	// IsAvailable returns whether this scorer is available in the
	// current environment.
	IsAvailable() bool
}
