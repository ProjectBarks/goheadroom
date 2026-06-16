package livezone

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/projectbarks/goheadroom/core/authmode"
	"github.com/projectbarks/goheadroom/core/ccr"
	"github.com/projectbarks/goheadroom/core/compressionpolicy"
)

func TestCompressAnthropicLiveZoneWithCCR_InjectsMarkers(t *testing.T) {
	longToolResult := makeCompressibleJSON(100)
	messages := []interface{}{
		msg("user", "Read the file"),
		msg("assistant", []interface{}{toolUseBlock("tu_1", "read_file", map[string]interface{}{"path": "/tmp/big.txt"})}),
		msg("user", []interface{}{toolResultBlock("tu_1", longToolResult)}),
	}
	body := anthropicBody(t, nil, messages, nil, "")

	store := ccr.NewInMemoryStore()
	config := &CCRConfig{}

	outcome := CompressAnthropicLiveZoneWithCCR(body, compressionpolicy.Auto, authmode.Payg, "req-ccr-1", store, config)

	assert.Equal(t, OutcomeCompressed, outcome.Kind)
	require.NotNil(t, outcome.Body)

	var result map[string]interface{}
	err := json.Unmarshal(outcome.Body, &result)
	require.NoError(t, err)

	resultMessages, ok := result["messages"].([]interface{})
	require.True(t, ok)
	assert.NotEmpty(t, resultMessages)
}

func TestCompressAnthropicLiveZoneWithCCR_FrozenMessageCount(t *testing.T) {
	longToolResult := makeCompressibleJSON(100)
	messages := []interface{}{
		msg("user", "First question"),
		msg("assistant", "First answer"),
		msg("user", "Second question"),
		msg("assistant", []interface{}{toolUseBlock("tu_1", "read", map[string]interface{}{})}),
		msg("user", []interface{}{toolResultBlock("tu_1", longToolResult)}),
	}
	body := anthropicBody(t, nil, messages, nil, "")

	store := ccr.NewInMemoryStore()
	config := &CCRConfig{}

	outcome := CompressAnthropicLiveZoneWithCCR(body, compressionpolicy.Auto, authmode.Payg, "req-ccr-2", store, config)

	if outcome.Kind == OutcomeCompressed {
		require.NotNil(t, outcome.Manifest)
		frozenCount := 0
		for _, block := range outcome.Manifest.Blocks {
			if block.Action == BlockActionExcluded && block.ExclusionReason != nil {
				if *block.ExclusionReason == ExclusionReasonFrozenByPolicy ||
					*block.ExclusionReason == ExclusionReasonCacheControl {
					frozenCount++
				}
			}
		}
		assert.Greater(t, frozenCount, 0, "CCR should freeze some messages")
	}
}

func TestCompressAnthropicLiveZoneWithCCR_NilStoresFallsBack(t *testing.T) {
	longToolResult := makeCompressibleJSON(100)
	messages := []interface{}{
		msg("user", "Read"),
		msg("assistant", []interface{}{toolUseBlock("tu_1", "read", map[string]interface{}{})}),
		msg("user", []interface{}{toolResultBlock("tu_1", longToolResult)}),
	}
	body := anthropicBody(t, nil, messages, nil, "")

	// nil store and config should fall back to non-CCR behavior
	outcome := CompressAnthropicLiveZoneWithCCR(body, compressionpolicy.Auto, authmode.Payg, "req-ccr-3", nil, nil)
	// Should still work, just without CCR
	assert.NotEqual(t, OutcomeError, outcome.Kind)
}
