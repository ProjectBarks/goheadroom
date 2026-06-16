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

func defaultJsonStructureCfg() pipeline.JsonStructureOffloadConfig {
	cfg, _ := pipeline.DefaultPipelineConfig()
	return cfg.Offload.JsonStructure
}

func jsonStructureOffload() *JsonStructureOffload {
	return NewJsonStructureOffload(defaultJsonStructureCfg())
}

func buildJsonObject(n int) string {
	var s strings.Builder
	s.WriteByte('{')
	for i := 0; i < n; i++ {
		if i > 0 {
			s.WriteByte(',')
		}
		s.WriteString(fmt.Sprintf(`"field_%d":"this is a long string value that will be elided by the compressor because it exceeds the threshold limit set in config"`, i))
	}
	s.WriteByte('}')
	return s.String()
}

func buildJsonArray(n int) string {
	var s strings.Builder
	s.WriteByte('[')
	for i := 0; i < n; i++ {
		if i > 0 {
			s.WriteByte(',')
		}
		s.WriteString(fmt.Sprintf(`{"id":%d,"description":"this is a long string value that will be elided by the compressor because it exceeds the short value threshold"}`, i))
	}
	s.WriteByte(']')
	return s.String()
}

func TestJsonStructureOffloadNameAndAppliesTo(t *testing.T) {
	o := jsonStructureOffload()
	assert.Equal(t, "json_structure_offload", o.Name())
	assert.Equal(t, []contentdetector.ContentType{contentdetector.PlainText, contentdetector.JsonArray}, o.AppliesTo())
}

func TestJsonStructureOffloadEstimateBloatEmptyIsZero(t *testing.T) {
	assert.Equal(t, float32(0.0), jsonStructureOffload().EstimateBloat(""))
}

func TestJsonStructureOffloadEstimateBloatNonJsonIsZero(t *testing.T) {
	assert.Equal(t, float32(0.0), jsonStructureOffload().EstimateBloat("just plain text here"))
	assert.Equal(t, float32(0.0), jsonStructureOffload().EstimateBloat("42"))
	assert.Equal(t, float32(0.0), jsonStructureOffload().EstimateBloat("some log output line"))
}

func TestJsonStructureOffloadEstimateBloatSmallInputIsZero(t *testing.T) {
	// Less than MinInputLen (100) bytes
	assert.Equal(t, float32(0.0), jsonStructureOffload().EstimateBloat(`{"a":"b"}`))
}

func TestJsonStructureOffloadEstimateBloatObjectWithLongValues(t *testing.T) {
	obj := buildJsonObject(5)
	score := jsonStructureOffload().EstimateBloat(obj)
	assert.Greater(t, score, float32(0.0))
}

func TestJsonStructureOffloadEstimateBloatArrayWithLongValues(t *testing.T) {
	arr := buildJsonArray(5)
	score := jsonStructureOffload().EstimateBloat(arr)
	assert.Greater(t, score, float32(0.0))
}

func TestJsonStructureOffloadApplyCompressesObject(t *testing.T) {
	obj := buildJsonObject(10)
	store := ccr.NewInMemoryStore()
	ctx := &pipeline.CompressionContext{}
	r, err := jsonStructureOffload().Apply(obj, ctx, store)
	require.NoError(t, err)
	assert.Greater(t, r.BytesSaved, 0)
	assert.NotEmpty(t, r.CacheKey)
	assert.Contains(t, r.Output, "[json_structure_offload CCR: hash=")
	stored, ok := store.Get(r.CacheKey)
	assert.True(t, ok)
	assert.Equal(t, obj, string(stored))
}

func TestJsonStructureOffloadApplyCompressesArray(t *testing.T) {
	arr := buildJsonArray(10)
	store := ccr.NewInMemoryStore()
	ctx := &pipeline.CompressionContext{}
	r, err := jsonStructureOffload().Apply(arr, ctx, store)
	require.NoError(t, err)
	assert.Greater(t, r.BytesSaved, 0)
	assert.Contains(t, r.Output, "[json_structure_offload CCR: hash=")
}

func TestJsonStructureOffloadApplySkipsNonJson(t *testing.T) {
	store := ccr.NewInMemoryStore()
	ctx := &pipeline.CompressionContext{}
	_, err := jsonStructureOffload().Apply("not json at all", ctx, store)
	require.Error(t, err)
	te, ok := err.(pipeline.TransformError)
	require.True(t, ok)
	assert.Equal(t, pipeline.ErrorInvalidInput, te.Kind)
	assert.Equal(t, 0, store.Len())
}

func TestJsonStructureOffloadCacheKeyStable(t *testing.T) {
	obj := buildJsonObject(10)
	storeA := ccr.NewInMemoryStore()
	storeB := ccr.NewInMemoryStore()
	ctx := &pipeline.CompressionContext{}
	rA, errA := jsonStructureOffload().Apply(obj, ctx, storeA)
	rB, errB := jsonStructureOffload().Apply(obj, ctx, storeB)
	require.NoError(t, errA)
	require.NoError(t, errB)
	assert.Equal(t, rA.CacheKey, rB.CacheKey)
}

func TestJsonStructureOffloadConfidence(t *testing.T) {
	assert.Equal(t, float32(0.80), jsonStructureOffload().Confidence())
}
