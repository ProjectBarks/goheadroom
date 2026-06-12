//go:build onnx

package relevance

import (
	"fmt"
	"sync"

	ort "github.com/yalue/onnxruntime_go"
)

// EmbeddingScorer wraps an ONNX runtime for semantic relevance scoring.
// This is the real implementation that requires the "onnx" build tag.
type EmbeddingScorer struct {
	ModelName string
	modelPath string
	session   *ort.Session
	mu        sync.Mutex
	available bool
}

// NewEmbeddingScorer creates an EmbeddingScorer with the given ONNX model path.
func NewEmbeddingScorer(modelPath string) (*EmbeddingScorer, error) {
	// Initialize ONNX runtime if needed.
	if err := ort.InitializeEnvironment(); err != nil {
		return nil, fmt.Errorf("ONNX runtime init failed: %w", err)
	}
	return &EmbeddingScorer{
		ModelName: EmbeddingModelName,
		modelPath: modelPath,
		available: true,
	}, nil
}

// NewEmbeddingScorerStub creates a stub scorer for testing.
func NewEmbeddingScorerStub() *EmbeddingScorer {
	return &EmbeddingScorer{
		ModelName: EmbeddingModelName,
		available: false,
	}
}

// Score implements RelevanceScorer.
func (e *EmbeddingScorer) Score(item, context string) RelevanceScore {
	if !e.available {
		return RelevanceScoreZero("Embedding: model not available")
	}
	// Full ONNX inference would go here.
	return RelevanceScoreZero("Embedding: inference not implemented")
}

// ScoreBatch implements RelevanceScorer.
func (e *EmbeddingScorer) ScoreBatch(items []string, context string) []RelevanceScore {
	results := make([]RelevanceScore, len(items))
	for i := range items {
		results[i] = e.Score(items[i], context)
	}
	return results
}

// IsAvailable implements RelevanceScorer.
func (e *EmbeddingScorer) IsAvailable() bool {
	return e.available
}
