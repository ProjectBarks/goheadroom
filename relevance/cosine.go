package relevance

import "math"

// EmbeddingDimension is the default embedding vector dimension for
// BAAI/bge-small-en-v1.5.
const EmbeddingDimension = 384

// EmbeddingModelName is the default model used for embedding scoring.
const EmbeddingModelName = "BAAI/bge-small-en-v1.5"

// CosineSimilarity computes cosine similarity between two vectors.
// Returns 0.0 for empty, zero, or mismatched-length vectors.
// Clamped to [0, 1] since we only care about positive similarity
// for relevance scoring.
func CosineSimilarity(a, b []float32) float64 {
	if len(a) == 0 || len(b) == 0 || len(a) != len(b) {
		return 0.0
	}

	var dot, normA, normB float64
	for i := range a {
		av := float64(a[i])
		bv := float64(b[i])
		dot += av * bv
		normA += av * av
		normB += bv * bv
	}

	if normA == 0.0 || normB == 0.0 {
		return 0.0
	}

	sim := dot / (math.Sqrt(normA) * math.Sqrt(normB))
	if sim < 0.0 {
		return 0.0
	}
	if sim > 1.0 {
		return 1.0
	}
	return sim
}
