// Package pipeline implements the compression pipeline that orchestrates
// reformat and offload transforms with parallel bloat estimation.
//
// Direct port of headroom-core/src/transforms/pipeline/.
package pipeline

import (
	"fmt"

	"github.com/projectbarks/goheadroom/core/transforms/contentdetector"
)

// --- TransformError ---

// ErrorKind distinguishes the three transform error categories.
type ErrorKind int

const (
	// ErrorInvalidInput means the transform could not parse the input.
	ErrorInvalidInput ErrorKind = iota
	// ErrorSkipped means the transform ran cleanly but found nothing to do.
	ErrorSkipped
	// ErrorInternal means an internal failure (serializer, store, logic bug).
	ErrorInternal
)

// TransformError is the error type returned by pipeline transforms.
// All three kinds signal "skip this transform, continue the pipeline."
type TransformError struct {
	Kind      ErrorKind
	Transform string
	Message   string
}

func (e TransformError) Error() string {
	switch e.Kind {
	case ErrorInvalidInput:
		return fmt.Sprintf("invalid input for %s: %s", e.Transform, e.Message)
	case ErrorSkipped:
		return fmt.Sprintf("%s skipped: %s", e.Transform, e.Message)
	case ErrorInternal:
		return fmt.Sprintf("%s internal error: %s", e.Transform, e.Message)
	default:
		return fmt.Sprintf("%s error: %s", e.Transform, e.Message)
	}
}

// InvalidInput creates an ErrorInvalidInput TransformError.
func InvalidInput(transform, message string) TransformError {
	return TransformError{Kind: ErrorInvalidInput, Transform: transform, Message: message}
}

// Skipped creates an ErrorSkipped TransformError.
func Skipped(transform, message string) TransformError {
	return TransformError{Kind: ErrorSkipped, Transform: transform, Message: message}
}

// Internal creates an ErrorInternal TransformError.
func Internal(transform, message string) TransformError {
	return TransformError{Kind: ErrorInternal, Transform: transform, Message: message}
}

// --- Output structs ---

// ReformatOutput is the result of a ReformatTransform. Output bytes are
// semantically equivalent to the input. No CCR needed.
type ReformatOutput struct {
	Output     string
	BytesSaved int
}

// ReformatOutputFromLengths constructs a ReformatOutput computing
// bytes_saved as inputLen - len(output), clamped to zero.
func ReformatOutputFromLengths(inputLen int, output string) ReformatOutput {
	saved := inputLen - len(output)
	if saved < 0 {
		saved = 0
	}
	return ReformatOutput{Output: output, BytesSaved: saved}
}

// OffloadOutput is the result of an OffloadTransform. Output bytes
// are a subset of the input; the original is in the CCR store under CacheKey.
type OffloadOutput struct {
	Output     string
	BytesSaved int
	// CacheKey under which the original payload is stored. Required.
	CacheKey string
}

// OffloadOutputFromLengths constructs an OffloadOutput computing
// bytes_saved as inputLen - len(output), clamped to zero.
func OffloadOutputFromLengths(inputLen int, output, cacheKey string) OffloadOutput {
	saved := inputLen - len(output)
	if saved < 0 {
		saved = 0
	}
	return OffloadOutput{Output: output, BytesSaved: saved, CacheKey: cacheKey}
}

// CompressionContext is per-call context the orchestrator passes to transforms.
type CompressionContext struct {
	// Query for relevance scoring inside offload transforms.
	Query string
	// TokenBudget the orchestrator is targeting. Nil means no budget signal.
	TokenBudget *int
}

// ContextWithQuery creates a CompressionContext with just a query.
func ContextWithQuery(query string) CompressionContext {
	return CompressionContext{Query: query}
}

// ContextWithBudget creates a CompressionContext with just a token budget.
func ContextWithBudget(budget int) CompressionContext {
	return CompressionContext{TokenBudget: &budget}
}

// --- CcrStore interface (re-exported from ccr for convenience) ---

// CcrStore is the interface for a cache of computed responses.
// Transforms use this to stash original payloads for CCR retrieval.
type CcrStore interface {
	Put(key string, value []byte)
	Get(key string) ([]byte, bool)
	Len() int
}

// --- Transform interfaces ---

// ReformatTransform packs input denser without dropping information.
// No CCR involvement. The orchestrator runs reformats first.
type ReformatTransform interface {
	// Name returns a stable telemetry name (lowercase snake_case).
	Name() string
	// AppliesTo returns the content types this transform accepts.
	AppliesTo() []contentdetector.ContentType
	// Apply runs the transform.
	Apply(content string) (*ReformatOutput, error)
}

// OffloadTransform drops bytes from the wire and stashes the original
// via CCR. Carries a cheap domain-specific bloat estimator.
type OffloadTransform interface {
	// Name returns a stable telemetry name (lowercase snake_case).
	Name() string
	// AppliesTo returns the content types this transform accepts.
	AppliesTo() []contentdetector.ContentType
	// EstimateBloat returns a 0.0-1.0 score of how much this transform
	// would benefit the input. Must be cheap (no full compression pass).
	// Must be safe on empty input (returns 0.0).
	EstimateBloat(content string) float32
	// Apply runs the offload. Only called when EstimateBloat >= threshold.
	// On success, CacheKey must resolve in the provided store.
	Apply(content string, ctx *CompressionContext, store CcrStore) (*OffloadOutput, error)
	// Confidence returns a calibrated 0.0-1.0 quality score for telemetry.
	Confidence() float32
}
