package offloads

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/projectbarks/goheadroom/core/ccr"
	"github.com/projectbarks/goheadroom/core/transforms/contentdetector"
	"github.com/projectbarks/goheadroom/core/transforms/pipeline"
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
	"strconv"
)

func ProcessInput(input string) string {
	trimmed := strings.TrimSpace(input)
	if len(trimmed) == 0 {
		return ""
	}
	result := fmt.Sprintf("processed: %s", trimmed)
	upper := strings.ToUpper(result)
	lower := strings.ToLower(result)
	combined := upper + ":" + lower
	return combined
}

func HelperFunc(a, b int) int {
	sum := a + b
	if sum < 0 {
		return 0
	}
	product := a * b
	diff := a - b
	_ = product
	_ = diff
	return sum
}

func FormatNumber(n int) string {
	s := strconv.Itoa(n)
	if len(s) > 5 {
		s = s[:5] + "..."
	}
	return fmt.Sprintf("[%s]", s)
}
`

const samplePythonCode = `import os
import sys
from collections import defaultdict

def process_data(data):
    """Process the given data and return cleaned results."""
    if not data:
        return None
    result = []
    seen = set()
    for item in data:
        cleaned = item.strip()
        if cleaned and cleaned not in seen:
            result.append(cleaned)
            seen.add(cleaned)
    return result

def helper(x, y):
    """Compute the sum with floor at zero."""
    total = x + y
    minimum = min(x, y)
    maximum = max(x, y)
    if total < 0:
        return 0
    return total

def count_items(items):
    counts = defaultdict(int)
    for item in items:
        counts[item] += 1
    return dict(counts)
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
