package pipeline

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/uber/goheadroom/transforms/contentdetector"
)

// --- TransformError tests ---

func TestTransformErrorInvalidInputMessage(t *testing.T) {
	err := TransformError{
		Kind:      ErrorInvalidInput,
		Transform: "json_minifier",
		Message:   "bad token at line 3",
	}
	msg := err.Error()
	assert.Contains(t, msg, "json_minifier")
	assert.Contains(t, msg, "bad token at line 3")
}

func TestTransformErrorSkippedMessage(t *testing.T) {
	err := TransformError{
		Kind:      ErrorSkipped,
		Transform: "log_template",
		Message:   "empty input",
	}
	msg := err.Error()
	assert.Contains(t, msg, "log_template")
	assert.Contains(t, msg, "skipped")
}

func TestTransformErrorInternalMessage(t *testing.T) {
	err := TransformError{
		Kind:      ErrorInternal,
		Transform: "diff_offload",
		Message:   "serializer crash",
	}
	msg := err.Error()
	assert.Contains(t, msg, "diff_offload")
	assert.Contains(t, msg, "internal error")
}

func TestTransformErrorConstructors(t *testing.T) {
	e1 := InvalidInput("json_minifier", "bad token")
	assert.Equal(t, ErrorInvalidInput, e1.Kind)
	assert.Equal(t, "json_minifier", e1.Transform)

	e2 := Skipped("log_template", "empty input")
	assert.Equal(t, ErrorSkipped, e2.Kind)

	e3 := Internal("diff_offload", "crash")
	assert.Equal(t, ErrorInternal, e3.Kind)
}

// --- ReformatOutput tests ---

func TestReformatOutputFromLengths(t *testing.T) {
	ro := ReformatOutputFromLengths(20, "shorter text")
	assert.Equal(t, "shorter text", ro.Output)
	assert.Equal(t, 20-len("shorter text"), ro.BytesSaved)
}

func TestReformatOutputFromLengthsClampsNegative(t *testing.T) {
	ro := ReformatOutputFromLengths(10, "this is much longer than 10 bytes")
	assert.Equal(t, 0, ro.BytesSaved)
}

// --- OffloadOutput tests ---

func TestOffloadOutputFromLengths(t *testing.T) {
	oo := OffloadOutputFromLengths(50, "short", "key123")
	assert.Equal(t, "short", oo.Output)
	assert.Equal(t, 50-len("short"), oo.BytesSaved)
	assert.Equal(t, "key123", oo.CacheKey)
}

func TestOffloadOutputFromLengthsClampsNegative(t *testing.T) {
	oo := OffloadOutputFromLengths(5, "this is much longer", "k")
	assert.Equal(t, 0, oo.BytesSaved)
}

// --- CompressionContext tests ---

func TestCompressionContextWithQuery(t *testing.T) {
	ctx := ContextWithQuery("find errors")
	assert.Equal(t, "find errors", ctx.Query)
	assert.Nil(t, ctx.TokenBudget)
}

func TestCompressionContextWithBudget(t *testing.T) {
	ctx := ContextWithBudget(2048)
	assert.Empty(t, ctx.Query)
	assert.NotNil(t, ctx.TokenBudget)
	assert.Equal(t, 2048, *ctx.TokenBudget)
}

// --- Interface conformance tests ---

func TestReformatInterfaceConformance(t *testing.T) {
	mock := &testReformat{}
	var _ ReformatTransform = mock
	assert.Equal(t, "test_reformat", mock.Name())
	assert.Equal(t, []contentdetector.ContentType{contentdetector.PlainText}, mock.AppliesTo())
	r, err := mock.Apply("hello")
	assert.NoError(t, err)
	assert.Equal(t, "hello", r.Output)
	assert.Equal(t, 0, r.BytesSaved)
}

func TestOffloadInterfaceConformance(t *testing.T) {
	mock := &testOffload{bloat: 0.9}
	var _ OffloadTransform = mock
	assert.Equal(t, "test_offload", mock.Name())
	assert.InDelta(t, 0.5, mock.Confidence(), 0.001)
	assert.Equal(t, float32(0.0), mock.EstimateBloat(""))
}

// --- Test helpers ---

type testReformat struct{}

func (t *testReformat) Name() string                                { return "test_reformat" }
func (t *testReformat) AppliesTo() []contentdetector.ContentType    { return []contentdetector.ContentType{contentdetector.PlainText} }
func (t *testReformat) Apply(content string) (*ReformatOutput, error) {
	return &ReformatOutput{Output: content, BytesSaved: 0}, nil
}

type testOffload struct {
	bloat float32
}

func (t *testOffload) Name() string                             { return "test_offload" }
func (t *testOffload) AppliesTo() []contentdetector.ContentType { return []contentdetector.ContentType{contentdetector.PlainText} }
func (t *testOffload) EstimateBloat(content string) float32 {
	if content == "" {
		return 0.0
	}
	return t.bloat
}
func (t *testOffload) Apply(content string, ctx *CompressionContext, store CcrStore) (*OffloadOutput, error) {
	key := "test_key"
	store.Put(key, []byte(content))
	return &OffloadOutput{Output: content, BytesSaved: 0, CacheKey: key}, nil
}
func (t *testOffload) Confidence() float32 { return 0.5 }
