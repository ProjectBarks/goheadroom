package relevance

import (
	"math"
	"testing"
)

func TestCosineSimilarityIdentical(t *testing.T) {
	v := []float32{1.0, 2.0, 3.0}
	sim := CosineSimilarity(v, v)
	if math.Abs(sim-1.0) > 1e-9 {
		t.Errorf("expected ~1.0, got %f", sim)
	}
}

func TestCosineSimilarityOrthogonal(t *testing.T) {
	a := []float32{1.0, 0.0, 0.0, 0.0}
	b := []float32{0.0, 1.0, 0.0, 0.0}
	sim := CosineSimilarity(a, b)
	if sim != 0.0 {
		t.Errorf("expected 0.0, got %f", sim)
	}
}

func TestCosineSimilarityOpposite(t *testing.T) {
	a := []float32{1.0, 1.0}
	b := []float32{-1.0, -1.0}
	// Raw cosine = -1.0; clamped to 0.0.
	sim := CosineSimilarity(a, b)
	if sim != 0.0 {
		t.Errorf("expected 0.0 (clamped), got %f", sim)
	}
}

func TestCosineSimilarityZeroVector(t *testing.T) {
	zero := []float32{0.0, 0.0, 0.0, 0.0}
	v := []float32{1.0, 2.0, 3.0, 4.0}
	if CosineSimilarity(zero, v) != 0.0 {
		t.Error("expected 0.0 for zero vector (first)")
	}
	if CosineSimilarity(v, zero) != 0.0 {
		t.Error("expected 0.0 for zero vector (second)")
	}
}

func TestCosineSimilarityDifferentLengths(t *testing.T) {
	a := []float32{1.0, 2.0}
	b := []float32{1.0, 2.0, 3.0}
	sim := CosineSimilarity(a, b)
	if sim != 0.0 {
		t.Errorf("expected 0.0 for mismatched dims, got %f", sim)
	}
}

func TestEmbeddingConstants(t *testing.T) {
	if EmbeddingDimension != 384 {
		t.Errorf("expected 384, got %d", EmbeddingDimension)
	}
	if EmbeddingModelName != "BAAI/bge-small-en-v1.5" {
		t.Errorf("expected BAAI/bge-small-en-v1.5, got %q", EmbeddingModelName)
	}
}

func TestEmbeddingScorerStubNotAvailable(t *testing.T) {
	s := NewEmbeddingScorerStub()
	if s.IsAvailable() {
		t.Error("stub scorer should not be available")
	}
	r := s.Score("item", "query")
	if r.Score != 0.0 {
		t.Errorf("expected 0.0, got %f", r.Score)
	}
}

func TestEmbeddingScorerStubBatch(t *testing.T) {
	s := NewEmbeddingScorerStub()
	scores := s.ScoreBatch([]string{"a", "b", "c"}, "query")
	if len(scores) != 3 {
		t.Fatalf("expected 3 scores, got %d", len(scores))
	}
	for _, sc := range scores {
		if sc.Score != 0.0 {
			t.Errorf("expected 0.0, got %f", sc.Score)
		}
	}
}

func TestNewEmbeddingScorerReturnsError(t *testing.T) {
	_, err := NewEmbeddingScorer("/nonexistent/model.onnx")
	if err == nil {
		t.Error("expected error for unavailable ONNX runtime")
	}
}
