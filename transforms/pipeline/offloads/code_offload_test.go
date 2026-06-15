package offloads

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uber/goheadroom/ccr"
	"github.com/uber/goheadroom/transforms/contentdetector"
	"github.com/uber/goheadroom/transforms/pipeline"
)

func defaultCodeCfg() pipeline.CodeOffloadConfig {
	cfg, _ := pipeline.DefaultPipelineConfig()
	return cfg.Offload.Code
}

func codeOffload() *CodeOffload {
	return NewCodeOffload(defaultCodeCfg())
}

const sampleGoCode = `package example

import (
	"fmt"
	"strings"
)

func ProcessInput(input string) string {
	trimmed := strings.TrimSpace(input)
	if len(trimmed) == 0 {
		return ""
	}
	result := fmt.Sprintf("processed: %s", trimmed)
	// Additional logic here
	return result
}

func HelperFunc(a, b int) int {
	sum := a + b
	if sum < 0 {
		return 0
	}
	return sum
}
`

const samplePythonCode = `import os
import sys

def process_data(data):
    """Process the given data."""
    if not data:
        return None
    result = []
    for item in data:
        result.append(item.strip())
    return result

def helper(x, y):
    total = x + y
    if total < 0:
        return 0
    return total
`

func TestCodeOffloadNameAndAppliesTo(t *testing.T) {
	o := codeOffload()
	assert.Equal(t, "code_offload", o.Name())
	assert.Equal(t, []contentdetector.ContentType{contentdetector.SourceCode}, o.AppliesTo())
}

func TestCodeOffloadEstimateBloatEmptyIsZero(t *testing.T) {
	assert.Equal(t, float32(0.0), codeOffload().EstimateBloat(""))
}

func TestCodeOffloadEstimateBloatNonCodeIsZero(t *testing.T) {
	assert.Equal(t, float32(0.0), codeOffload().EstimateBloat("just plain text here"))
	assert.Equal(t, float32(0.0), codeOffload().EstimateBloat(`{"a":1,"b":2}`))
}

func TestCodeOffloadEstimateBloatSmallInputIsZero(t *testing.T) {
	// Short code (under MinInputLen = 200 trimmed bytes)
	short := "func f() { return 1 }"
	assert.Equal(t, float32(0.0), codeOffload().EstimateBloat(short))
}

func TestCodeOffloadEstimateBloatGoCode(t *testing.T) {
	score := codeOffload().EstimateBloat(sampleGoCode)
	assert.Greater(t, score, float32(0.0))
}

func TestCodeOffloadEstimateBloatPythonCode(t *testing.T) {
	score := codeOffload().EstimateBloat(samplePythonCode)
	assert.Greater(t, score, float32(0.0))
}

func TestCodeOffloadApplyCompressesGoCode(t *testing.T) {
	store := ccr.NewInMemoryStore()
	ctx := &pipeline.CompressionContext{}
	r, err := codeOffload().Apply(sampleGoCode, ctx, store)
	require.NoError(t, err)
	assert.Greater(t, r.BytesSaved, 0)
	assert.NotEmpty(t, r.CacheKey)
	assert.Contains(t, r.Output, "[code_offload CCR: hash=")
	// Signature should be preserved
	assert.Contains(t, r.Output, "func ProcessInput")
	assert.Contains(t, r.Output, "func HelperFunc")
	// Original stored
	stored, ok := store.Get(r.CacheKey)
	assert.True(t, ok)
	assert.Equal(t, sampleGoCode, string(stored))
}

func TestCodeOffloadApplyCompressesPythonCode(t *testing.T) {
	store := ccr.NewInMemoryStore()
	ctx := &pipeline.CompressionContext{}
	r, err := codeOffload().Apply(samplePythonCode, ctx, store)
	require.NoError(t, err)
	assert.Greater(t, r.BytesSaved, 0)
	assert.Contains(t, r.Output, "[code_offload CCR: hash=")
	assert.Contains(t, r.Output, "def process_data")
}

func TestCodeOffloadApplySkipsNonCode(t *testing.T) {
	store := ccr.NewInMemoryStore()
	ctx := &pipeline.CompressionContext{}
	_, err := codeOffload().Apply("this is just plain text with no code patterns at all", ctx, store)
	require.Error(t, err)
	te, ok := err.(pipeline.TransformError)
	require.True(t, ok)
	assert.Equal(t, pipeline.ErrorInvalidInput, te.Kind)
	assert.Equal(t, 0, store.Len())
}

func TestCodeOffloadCacheKeyStable(t *testing.T) {
	storeA := ccr.NewInMemoryStore()
	storeB := ccr.NewInMemoryStore()
	ctx := &pipeline.CompressionContext{}
	rA, errA := codeOffload().Apply(sampleGoCode, ctx, storeA)
	rB, errB := codeOffload().Apply(sampleGoCode, ctx, storeB)
	require.NoError(t, errA)
	require.NoError(t, errB)
	assert.Equal(t, rA.CacheKey, rB.CacheKey)
}

func TestCodeOffloadConfidence(t *testing.T) {
	assert.Equal(t, float32(0.85), codeOffload().Confidence())
}

func TestCodeOffloadApplyPreservesImports(t *testing.T) {
	store := ccr.NewInMemoryStore()
	ctx := &pipeline.CompressionContext{}
	r, err := codeOffload().Apply(sampleGoCode, ctx, store)
	require.NoError(t, err)
	// Import block should be preserved
	assert.True(t, strings.Contains(r.Output, "import") || strings.Contains(r.Output, "fmt"))
}
