package pipeline

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uber/goheadroom/ccr"
	"github.com/uber/goheadroom/transforms/contentdetector"
)

// --- Test helpers ---

type alwaysHalfReformat struct{}

func (a *alwaysHalfReformat) Name() string { return "always_half" }
func (a *alwaysHalfReformat) AppliesTo() []contentdetector.ContentType {
	return []contentdetector.ContentType{contentdetector.PlainText}
}
func (a *alwaysHalfReformat) Apply(content string) (*ReformatOutput, error) {
	half := content[:len(content)/2]
	out := ReformatOutputFromLengths(len(content), half)
	return &out, nil
}

type configTestOffload struct {
	name       string
	score      float32
	confidence float32
	appliesTo  []contentdetector.ContentType
}

func newTestOff(name string, score float32) *configTestOffload {
	return &configTestOffload{
		name:       name,
		score:      score,
		confidence: 0.5,
		appliesTo:  []contentdetector.ContentType{contentdetector.PlainText},
	}
}

func (t *configTestOffload) Name() string                             { return t.name }
func (t *configTestOffload) AppliesTo() []contentdetector.ContentType { return t.appliesTo }
func (t *configTestOffload) EstimateBloat(_ string) float32           { return t.score }
func (t *configTestOffload) Confidence() float32                      { return t.confidence }
func (t *configTestOffload) Apply(content string, _ *CompressionContext, store CcrStore) (*OffloadOutput, error) {
	half := content[:len(content)/2]
	key := "test_" + t.name + "_key"
	store.Put(key, []byte(content))
	out := OffloadOutputFromLengths(len(content), half, key)
	return &out, nil
}

type alwaysInternalErrorOffload struct{}

func (a *alwaysInternalErrorOffload) Name() string { return "always_internal_err" }
func (a *alwaysInternalErrorOffload) AppliesTo() []contentdetector.ContentType {
	return []contentdetector.ContentType{contentdetector.PlainText}
}
func (a *alwaysInternalErrorOffload) EstimateBloat(_ string) float32 { return 0.9 }
func (a *alwaysInternalErrorOffload) Apply(_ string, _ *CompressionContext, _ CcrStore) (*OffloadOutput, error) {
	return nil, Internal("always_internal_err", "by design")
}
func (a *alwaysInternalErrorOffload) Confidence() float32 { return 0.5 }

func testCtx() *CompressionContext {
	return &CompressionContext{}
}

func testStore() *ccr.InMemoryStore {
	return ccr.NewInMemoryStore()
}

// --- Tests ---

func TestEmptyPipelinePassesInputThrough(t *testing.T) {
	p := NewPipelineBuilder().Build()
	s := testStore()
	r := p.Run("hello world", contentdetector.PlainText, testCtx(), s)
	assert.Equal(t, "hello world", r.Output)
	assert.Equal(t, 0, r.BytesSaved)
	assert.Empty(t, r.StepsApplied)
	assert.Empty(t, r.CacheKeys)
	assert.Equal(t, 0, s.Len())
}

func TestEmptyInputReturnsEmptyOutput(t *testing.T) {
	p := NewPipelineBuilder().Build()
	s := testStore()
	r := p.Run("", contentdetector.PlainText, testCtx(), s)
	assert.Empty(t, r.Output)
	assert.Empty(t, r.StepsApplied)
}

func TestReformatRunsWhenApplicable(t *testing.T) {
	p := NewPipelineBuilder().
		WithReformat(&alwaysHalfReformat{}).
		Build()
	s := testStore()
	input := strings.Repeat("x", 100)
	r := p.Run(input, contentdetector.PlainText, testCtx(), s)
	assert.Greater(t, r.BytesSaved, 0)
	assert.Equal(t, []string{"always_half"}, r.StepsApplied)
	assert.Less(t, len(r.Output), len(input))
	assert.Empty(t, r.CacheKeys)
}

func TestReformatSkippedForUnrelatedContentType(t *testing.T) {
	p := NewPipelineBuilder().
		WithReformat(&alwaysHalfReformat{}).
		Build()
	s := testStore()
	// alwaysHalfReformat applies to PlainText, not JsonArray
	r := p.Run("not json", contentdetector.JsonArray, testCtx(), s)
	assert.Equal(t, "not json", r.Output)
	assert.Empty(t, r.StepsApplied)
}

func TestOffloadRunsWhenBloatAboveThreshold(t *testing.T) {
	p := NewPipelineBuilder().
		WithOffload(newTestOff("high_bloat", 0.9)).
		Build()
	s := testStore()
	r := p.Run(strings.Repeat("x", 100), contentdetector.PlainText, testCtx(), s)
	assert.Equal(t, []string{"high_bloat"}, r.StepsApplied)
	assert.Equal(t, 1, len(r.CacheKeys))
	stored, ok := s.Get(r.CacheKeys[0])
	assert.True(t, ok)
	assert.NotEmpty(t, stored)
}

func TestOffloadSkippedWhenScoreZero(t *testing.T) {
	p := NewPipelineBuilder().
		WithOffload(newTestOff("low_bloat", 0.0)).
		Build()
	s := testStore()
	r := p.Run(strings.Repeat("x", 100), contentdetector.PlainText, testCtx(), s)
	assert.Empty(t, r.StepsApplied)
	assert.Equal(t, 0, s.Len())
}

func TestOffloadSkippedWhenReformatSufficientAndScoreBelowThreshold(t *testing.T) {
	p := NewPipelineBuilder().
		WithReformat(&alwaysHalfReformat{}).
		WithOffload(newTestOff("midway", 0.3)).
		Build()
	s := testStore()
	r := p.Run(strings.Repeat("x", 100), contentdetector.PlainText, testCtx(), s)
	assert.Equal(t, []string{"always_half"}, r.StepsApplied)
	assert.Empty(t, r.CacheKeys)
}

func TestOffloadRunsAsFallbackWhenReformatUnderwhelms(t *testing.T) {
	// No reformats, so reformat_ratio = 1.0 (above fallback_ratio=0.85),
	// AND score > 0 -> offload runs as fallback.
	p := NewPipelineBuilder().
		WithOffload(newTestOff("fallback", 0.2)).
		Build()
	s := testStore()
	r := p.Run(strings.Repeat("x", 100), contentdetector.PlainText, testCtx(), s)
	assert.Equal(t, []string{"fallback"}, r.StepsApplied)
}

func TestOffloadAboveThresholdRunsEvenWhenReformatWasGreat(t *testing.T) {
	p := NewPipelineBuilder().
		WithReformat(&alwaysHalfReformat{}).
		WithOffload(newTestOff("forced", 0.9)).
		Build()
	s := testStore()
	r := p.Run(strings.Repeat("x", 100), contentdetector.PlainText, testCtx(), s)
	require.Len(t, r.StepsApplied, 2)
	assert.Equal(t, "always_half", r.StepsApplied[0])
	assert.Equal(t, "forced", r.StepsApplied[1])
	assert.Equal(t, 1, len(r.CacheKeys))
}

func TestParallelBloatEstimationReturnsCorrectScores(t *testing.T) {
	p := NewPipelineBuilder().
		WithOffload(newTestOff("alpha", 0.9)).
		WithOffload(newTestOff("beta", 0.0)).
		Build()
	s := testStore()
	r := p.Run(strings.Repeat("x", 100), contentdetector.PlainText, testCtx(), s)
	// Only "alpha" should run (above threshold). "beta" with 0.0
	// should not run even via fallback (score must be > 0).
	assert.Equal(t, []string{"alpha"}, r.StepsApplied)
}

func TestOffloadInternalErrorDoesNotPanicAndYieldsInput(t *testing.T) {
	p := NewPipelineBuilder().
		WithOffload(&alwaysInternalErrorOffload{}).
		Build()
	s := testStore()
	r := p.Run(strings.Repeat("x", 100), contentdetector.PlainText, testCtx(), s)
	assert.Empty(t, r.StepsApplied)
	assert.Equal(t, 100, len(r.Output))
	assert.Equal(t, 0, s.Len())
}

func TestBuilderDispatchesByAppliesTo(t *testing.T) {
	p := NewPipelineBuilder().
		WithReformat(&alwaysHalfReformat{}).
		WithOffload(newTestOff("plain_offload", 0.9)).
		Build()
	// alwaysHalfReformat -> PlainText, testOffload -> PlainText
	assert.Len(t, p.reformatsByType[contentdetector.PlainText], 1)
	assert.Len(t, p.offloadsByType[contentdetector.PlainText], 1)
	assert.Empty(t, p.reformatsByType[contentdetector.BuildOutput])
	assert.Empty(t, p.offloadsByType[contentdetector.JsonArray])
}

func TestBuilderPreservesRegistrationOrderForOffloads(t *testing.T) {
	p := NewPipelineBuilder().
		WithOffload(newTestOff("first", 0.9)).
		WithOffload(newTestOff("second", 0.9)).
		Build()
	s := testStore()
	r := p.Run(strings.Repeat("x", 100), contentdetector.PlainText, testCtx(), s)
	assert.Equal(t, []string{"first", "second"}, r.StepsApplied)
}

func TestConfigAccessor(t *testing.T) {
	p := NewPipelineBuilder().Build()
	assert.NotNil(t, p.Config())
	assert.Equal(t, 0.5, p.Config().Pipeline.ReformatTargetRatio)
}
