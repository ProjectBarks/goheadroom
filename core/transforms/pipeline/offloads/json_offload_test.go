package offloads

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/projectbarks/goheadroom/core/ccr"
	"github.com/projectbarks/goheadroom/core/transforms/contentdetector"
	"github.com/projectbarks/goheadroom/core/transforms/pipeline"
)

func defaultJsonCfg() pipeline.JsonOffloadConfig {
	cfg, _ := pipeline.DefaultPipelineConfig()
	return cfg.Offload.Json
}

func jsonOffload() *JsonOffload {
	return NewJsonOffload(defaultJsonCfg())
}

func buildTabularArray(n int) string {
	var s strings.Builder
	s.WriteByte('[')
	for i := 0; i < n; i++ {
		if i > 0 {
			s.WriteByte(',')
		}
		s.WriteString(fmt.Sprintf(`{"id":%d,"name":"item-%d","value":%d}`, i, i, i*100))
	}
	s.WriteByte(']')
	return s.String()
}

func TestJsonOffloadNameAndAppliesTo(t *testing.T) {
	o := jsonOffload()
	assert.Equal(t, "json_offload", o.Name())
	assert.Equal(t, []contentdetector.ContentType{contentdetector.JsonArray}, o.AppliesTo())
}

func TestJsonOffloadEstimateBloatEmptyZero(t *testing.T) {
	assert.Equal(t, float32(0.0), jsonOffload().EstimateBloat(""))
}

func TestJsonOffloadEstimateBloatNonArrayZero(t *testing.T) {
	assert.Equal(t, float32(0.0), jsonOffload().EstimateBloat(`{"a":1,"b":2}`))
	assert.Equal(t, float32(0.0), jsonOffload().EstimateBloat("just words here"))
	assert.Equal(t, float32(0.0), jsonOffload().EstimateBloat("42"))
}

func TestJsonOffloadEstimateBloatBelowMinRowsZero(t *testing.T) {
	arr := buildTabularArray(3)
	assert.Equal(t, float32(0.0), jsonOffload().EstimateBloat(arr))
}

func TestJsonOffloadEstimateBloatAtSaturationIsOne(t *testing.T) {
	arr := buildTabularArray(100)
	score := jsonOffload().EstimateBloat(arr)
	assert.GreaterOrEqual(t, score, float32(0.99))
}

func TestJsonOffloadEstimateBloatScalesLinearly(t *testing.T) {
	arr := buildTabularArray(25)
	score := jsonOffload().EstimateBloat(arr)
	assert.Greater(t, score, float32(0.3))
	assert.Less(t, score, float32(0.7))
}

func TestJsonOffloadEstimateBloatPrettyPrinted(t *testing.T) {
	var s strings.Builder
	s.WriteString("[\n")
	for i := 0; i < 30; i++ {
		if i > 0 {
			s.WriteString(",\n")
		}
		s.WriteString(fmt.Sprintf(`  {"id":%d,"name":"item-%d","value":%d}`, i, i, i*100))
	}
	s.WriteString("\n]")
	score := jsonOffload().EstimateBloat(s.String())
	assert.Greater(t, score, float32(0.4))
}

func buildRepetitiveArray(n int) string {
	var s strings.Builder
	s.WriteByte('[')
	for i := 0; i < n; i++ {
		if i > 0 {
			s.WriteByte(',')
		}
		// Use many identical rows so SmartCrusher detects low uniqueness
		// and compresses. Only the id varies; all other fields are identical.
		s.WriteString(fmt.Sprintf(`{"id":%d,"status":"ok","category":"type-a","message":"heartbeat received"}`, i))
	}
	s.WriteByte(']')
	return s.String()
}

func TestJsonOffloadApplyCompressesLargeArray(t *testing.T) {
	// Build a large repetitive array where SmartCrusher will produce real savings
	arr := buildRepetitiveArray(2000)
	store := ccr.NewInMemoryStore()
	ctx := &pipeline.CompressionContext{}
	r, err := jsonOffload().Apply(arr, ctx, store)
	require.NoError(t, err)
	assert.Greater(t, r.BytesSaved, 0)
	assert.NotEmpty(t, r.CacheKey)
	assert.Contains(t, r.Output, "[json_offload CCR: hash=")
	stored, ok := store.Get(r.CacheKey)
	assert.True(t, ok)
	assert.Equal(t, arr, string(stored))
}

func TestJsonOffloadApplySkippedForSmallArray(t *testing.T) {
	arr := buildTabularArray(2)
	store := ccr.NewInMemoryStore()
	ctx := &pipeline.CompressionContext{}
	_, err := jsonOffload().Apply(arr, ctx, store)
	require.Error(t, err)
	te, ok := err.(pipeline.TransformError)
	require.True(t, ok)
	assert.Equal(t, pipeline.ErrorSkipped, te.Kind)
	assert.Equal(t, 0, store.Len())
}

func TestJsonOffloadApplySkippedForNonJson(t *testing.T) {
	store := ccr.NewInMemoryStore()
	ctx := &pipeline.CompressionContext{}
	_, err := jsonOffload().Apply("not json at all", ctx, store)
	require.Error(t, err)
}

func TestJsonOffloadCacheKeyStable(t *testing.T) {
	arr := buildRepetitiveArray(2000)
	storeA := ccr.NewInMemoryStore()
	storeB := ccr.NewInMemoryStore()
	ctx := &pipeline.CompressionContext{}
	rA, errA := jsonOffload().Apply(arr, ctx, storeA)
	rB, errB := jsonOffload().Apply(arr, ctx, storeB)
	require.NoError(t, errA)
	require.NoError(t, errB)
	assert.Equal(t, rA.CacheKey, rB.CacheKey)
}

func TestJsonOffloadConfidence(t *testing.T) {
	assert.Equal(t, float32(0.85), jsonOffload().Confidence())
}

func TestCountRowSeparators(t *testing.T) {
	assert.Equal(t, 0, countRowSeparators(""))
	assert.Equal(t, 0, countRowSeparators("[]"))
	assert.Equal(t, 0, countRowSeparators(`[{"a":1}]`))
	assert.Equal(t, 1, countRowSeparators(`[{"a":1},{"a":2}]`))
	assert.Equal(t, 2, countRowSeparators(`[{"a":1}, {"a":2}, {"a":3}]`))
}
