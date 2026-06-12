//go:build !onnx

package relevance

import "fmt"

// EmbeddingScorer wraps an ONNX runtime for semantic relevance scoring.
// This is the stub implementation that returns errors when ONNX is not
// available (built without the "onnx" tag).
type EmbeddingScorer struct {
	ModelName string
	available bool
}

// NewEmbeddingScorer returns an error because ONNX runtime is not
// available in this build.
func NewEmbeddingScorer(modelPath string) (*EmbeddingScorer, error) {
	return nil, fmt.Errorf("embedding scorer requires ONNX runtime (build with -tags onnx)")
}

// NewEmbeddingScorerStub creates a stub scorer for testing the
// unavailable path. IsAvailable() returns false.
func NewEmbeddingScorerStub() *EmbeddingScorer {
	return &EmbeddingScorer{
		ModelName: EmbeddingModelName,
		available: false,
	}
}

// Score implements RelevanceScorer.
func (e *EmbeddingScorer) Score(item, context string) RelevanceScore {
	return RelevanceScoreZero("Embedding: model not available")
}

// ScoreBatch implements RelevanceScorer.
func (e *EmbeddingScorer) ScoreBatch(items []string, context string) []RelevanceScore {
	results := make([]RelevanceScore, len(items))
	for i := range items {
		results[i] = RelevanceScoreZero("Embedding: model not available")
	}
	return results
}

// IsAvailable implements RelevanceScorer.
func (e *EmbeddingScorer) IsAvailable() bool {
	return e.available
}
