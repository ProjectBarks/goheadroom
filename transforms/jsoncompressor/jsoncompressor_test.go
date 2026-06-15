package jsoncompressor

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPreservesKeys(t *testing.T) {
	result := Compress(`{"name": "Alice", "age": 30}`, DefaultConfig())
	assert.Contains(t, result.Compressed, `"name"`)
	assert.Contains(t, result.Compressed, `"age"`)
	assert.Equal(t, 2, result.KeyCount)
}

func TestPreservesBooleansAndNull(t *testing.T) {
	result := Compress(`{"active": true, "del": false, "v": null}`, DefaultConfig())
	assert.Contains(t, result.Compressed, "true")
	assert.Contains(t, result.Compressed, "false")
	assert.Contains(t, result.Compressed, "null")
}

func TestPreservesShortStrings(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ShortValueThreshold = 10
	result := Compress(`{"status": "ok"}`, cfg)
	assert.Contains(t, result.Compressed, `"ok"`)
}

func TestElongatedStringReplaced(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ShortValueThreshold = 5
	result := Compress(`{"description": "This is a very long description that goes on and on"}`, cfg)
	assert.NotContains(t, result.Compressed, "very long description")
	assert.Contains(t, result.Compressed, `"description"`)
}

func TestHighEntropyPreserved(t *testing.T) {
	result := Compress(`{"id": "550e8400-e29b-41d4-a716-446655440000"}`, DefaultConfig())
	assert.Contains(t, result.Compressed, "550e8400-e29b-41d4-a716-446655440000")
}

func TestLowEntropyLongStringReplaced(t *testing.T) {
	result := Compress(`{"data": "`+strings.Repeat("a", 80)+`"}`, DefaultConfig())
	assert.NotContains(t, result.Compressed, strings.Repeat("a", 80))
	assert.Contains(t, result.Compressed, `"data"`)
}

func TestArrayItemsAfterMaxFullReplaced(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxArrayItemsFull = 2
	result := Compress(`["short", "also short", "third item here now", "fourth item"]`, cfg)
	assert.Contains(t, result.Compressed, `"short"`)
	assert.Contains(t, result.Compressed, `"also short"`)
	assert.NotContains(t, result.Compressed, "third item")
}

func TestEmptyObject(t *testing.T) {
	result := Compress("{}", DefaultConfig())
	assert.Equal(t, "{}", result.Compressed)
	assert.Equal(t, 0, result.KeyCount)
}

func TestNestedObject(t *testing.T) {
	result := Compress(`{"user": {"name": "Bob", "bio": "`+strings.Repeat("x", 100)+`"}}`, DefaultConfig())
	assert.Contains(t, result.Compressed, `"user"`)
	assert.Contains(t, result.Compressed, `"name"`)
	assert.Contains(t, result.Compressed, `"Bob"`)
	assert.NotContains(t, result.Compressed, strings.Repeat("x", 100))
}

func TestShortValueThresholdAppliedToPayload(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ShortValueThreshold = 4
	result := Compress(`{"a": "abcd", "b": "abcde"}`, cfg)
	assert.Contains(t, result.Compressed, `"abcd"`)
	assert.NotContains(t, result.Compressed, `"abcde"`)
}

func TestNormalizedEntropy(t *testing.T) {
	assert.InDelta(t, 0.0, normalizedEntropy("aaaa"), 0.001)
	assert.Greater(t, normalizedEntropy("550e8400-e29b"), 0.8)
}
