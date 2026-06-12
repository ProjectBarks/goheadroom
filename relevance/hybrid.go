package relevance

import (
	"fmt"
	"math"
	"regexp"
	"strings"
)

// Alpha tuning constants.
const (
	AlphaMin = 0.3
	AlphaMax = 0.9
)

// Regex patterns for identifier detection in queries.
var (
	uuidPattern    = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)
	numericIDPat   = regexp.MustCompile(`\b\d{4,}\b`)
	hostnamePat    = regexp.MustCompile(`\b[a-zA-Z0-9][-a-zA-Z0-9]*\.[a-zA-Z0-9][-a-zA-Z0-9]*(?:\.[a-zA-Z]{2,})?\b`)
	emailPat       = regexp.MustCompile(`\b[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Z|a-z]{2,}\b`)
)

// IsUUID returns true if the text contains a UUID pattern.
func IsUUID(text string) bool {
	return uuidPattern.MatchString(text)
}

// IsHostname returns true if the text contains a hostname-like pattern.
func IsHostname(text string) bool {
	return hostnamePat.MatchString(strings.ToLower(text))
}

// IsEmail returns true if the text contains an email-like pattern.
func IsEmail(text string) bool {
	return emailPat.MatchString(strings.ToLower(text))
}

// ClampAlpha clamps alpha to [AlphaMin, AlphaMax].
func ClampAlpha(alpha float64) float64 {
	if alpha < AlphaMin {
		return AlphaMin
	}
	if alpha > AlphaMax {
		return AlphaMax
	}
	return alpha
}

// ComputeAdaptiveAlpha computes the per-query alpha based on content
// analysis. Queries with UUIDs, numeric IDs, hostnames, or emails
// get a higher BM25 weight (exact match matters more than semantic
// match for those cases). Returns clamped to [AlphaMin, AlphaMax].
func ComputeAdaptiveAlpha(baseAlpha float64, context string) float64 {
	contextLower := strings.ToLower(context)

	uuidCount := len(uuidPattern.FindAllString(context, -1))
	idCount := len(numericIDPat.FindAllString(context, -1))
	hostnameCount := len(hostnamePat.FindAllString(contextLower, -1))
	emailCount := len(emailPat.FindAllString(contextLower, -1))

	alpha := baseAlpha
	if uuidCount > 0 {
		alpha = math.Max(alpha, 0.85)
	} else if idCount >= 2 {
		alpha = math.Max(alpha, 0.75)
	} else if idCount == 1 {
		alpha = math.Max(alpha, 0.65)
	} else if hostnameCount > 0 || emailCount > 0 {
		alpha = math.Max(alpha, 0.6)
	}

	return ClampAlpha(alpha)
}

// HybridScorer combines BM25 + optional embedding scoring with
// adaptive alpha tuning.
type HybridScorer struct {
	BaseAlpha          float64
	Adaptive           bool
	BM25               *BM25Scorer
	Embedding          *EmbeddingScorer
	embeddingAvailable bool
}

// NewHybridScorer creates a HybridScorer with both BM25 and embedding.
func NewHybridScorer(bm25 *BM25Scorer, embedding *EmbeddingScorer) *HybridScorer {
	avail := false
	if embedding != nil {
		avail = embedding.IsAvailable()
	}
	return &HybridScorer{
		BaseAlpha:          0.5,
		Adaptive:           true,
		BM25:               bm25,
		Embedding:          embedding,
		embeddingAvailable: avail,
	}
}

// NewHybridScorerBM25Only creates a HybridScorer using only BM25 (no
// embedding). The embedding scorer is stubbed and unavailable.
func NewHybridScorerBM25Only() *HybridScorer {
	return &HybridScorer{
		BaseAlpha:          0.5,
		Adaptive:           true,
		BM25:               NewBM25Scorer(),
		Embedding:          NewEmbeddingScorerStub(),
		embeddingAvailable: false,
	}
}

// HasEmbeddingSupport returns true if the embedding scorer is available.
func (h *HybridScorer) HasEmbeddingSupport() bool {
	return h.embeddingAvailable
}

// computeAlpha computes the per-query alpha.
func (h *HybridScorer) computeAlpha(context string) float64 {
	if !h.Adaptive {
		return h.BaseAlpha
	}
	return ComputeAdaptiveAlpha(h.BaseAlpha, context)
}

// boostBM25Only applies the BM25-only fallback boost. Items with any
// matched term get score >= 0.3. Items with two or more matched terms
// get +0.2, capped at 1.0.
func (h *HybridScorer) boostBM25Only(bm25Result RelevanceScore) RelevanceScore {
	boosted := bm25Result.Score
	if len(bm25Result.MatchedTerms) > 0 {
		boosted = math.Max(boosted, 0.3)
		if len(bm25Result.MatchedTerms) >= 2 {
			boosted = math.Min(boosted+0.2, 1.0)
		}
	}
	return NewRelevanceScore(
		boosted,
		fmt.Sprintf("Hybrid (BM25 only, boosted): %s", bm25Result.Reason),
		bm25Result.MatchedTerms,
	)
}

// Score implements RelevanceScorer.
func (h *HybridScorer) Score(item, context string) RelevanceScore {
	bm25Result := h.BM25.Score(item, context)

	if !h.embeddingAvailable {
		return h.boostBM25Only(bm25Result)
	}

	embResult := h.Embedding.Score(item, context)
	alpha := h.computeAlpha(context)
	combined := alpha*bm25Result.Score + (1.0-alpha)*embResult.Score

	return NewRelevanceScore(
		combined,
		fmt.Sprintf("Hybrid (alpha=%.2f): BM25=%.2f, Semantic=%.2f", alpha, bm25Result.Score, embResult.Score),
		bm25Result.MatchedTerms,
	)
}

// ScoreBatch implements RelevanceScorer.
func (h *HybridScorer) ScoreBatch(items []string, context string) []RelevanceScore {
	if len(items) == 0 {
		return nil
	}

	bm25Results := h.BM25.ScoreBatch(items, context)

	if !h.embeddingAvailable {
		results := make([]RelevanceScore, len(bm25Results))
		for i, r := range bm25Results {
			results[i] = h.boostBM25Only(r)
		}
		return results
	}

	embResults := h.Embedding.ScoreBatch(items, context)
	alpha := h.computeAlpha(context)

	results := make([]RelevanceScore, len(items))
	for i := range items {
		combined := alpha*bm25Results[i].Score + (1.0-alpha)*embResults[i].Score
		results[i] = NewRelevanceScore(
			combined,
			fmt.Sprintf("Hybrid (alpha=%.2f): BM25=%.2f, Emb=%.2f", alpha, bm25Results[i].Score, embResults[i].Score),
			bm25Results[i].MatchedTerms,
		)
	}
	return results
}

// IsAvailable implements RelevanceScorer. Hybrid is always available
// because it falls back to BM25.
func (h *HybridScorer) IsAvailable() bool {
	return true
}
