package livezone

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/projectbarks/goheadroom/core/authmode"
)

func TestBlockAction_String(t *testing.T) {
	assert.Equal(t, "Compressed", BlockActionCompressed.String())
	assert.Equal(t, "Excluded", BlockActionExcluded.String())
	assert.Equal(t, "Passthrough", BlockActionPassthrough.String())
}

func TestExclusionReason_String(t *testing.T) {
	assert.Equal(t, "SystemMessage", ExclusionReasonSystemMessage.String())
	assert.Equal(t, "ToolDefinition", ExclusionReasonToolDefinition.String())
	assert.Equal(t, "FrozenByPolicy", ExclusionReasonFrozenByPolicy.String())
	assert.Equal(t, "CacheControl", ExclusionReasonCacheControl.String())
	assert.Equal(t, "TooSmall", ExclusionReasonTooSmall.String())
	assert.Equal(t, "LatestAssistant", ExclusionReasonLatestAssistant.String())
	assert.Equal(t, "LatestUser", ExclusionReasonLatestUser.String())
}

func TestBlockOutcome_Fields(t *testing.T) {
	bo := BlockOutcome{
		Index:            3,
		Action:           BlockActionCompressed,
		ExclusionReason:  nil,
		Strategy:         "smart_crush",
		OriginalTokens:   500,
		CompressedTokens: 200,
	}
	assert.Equal(t, 3, bo.Index)
	assert.Equal(t, BlockActionCompressed, bo.Action)
	assert.Nil(t, bo.ExclusionReason)
	assert.Equal(t, "smart_crush", bo.Strategy)
	assert.Equal(t, 500, bo.OriginalTokens)
	assert.Equal(t, 200, bo.CompressedTokens)
}

func TestBlockOutcome_WithExclusionReason(t *testing.T) {
	reason := ExclusionReasonCacheControl
	bo := BlockOutcome{
		Index:           1,
		Action:          BlockActionExcluded,
		ExclusionReason: &reason,
		Strategy:        "",
		OriginalTokens:  100,
	}
	require.NotNil(t, bo.ExclusionReason)
	assert.Equal(t, ExclusionReasonCacheControl, *bo.ExclusionReason)
}

func TestCompressionManifest_Fields(t *testing.T) {
	m := CompressionManifest{
		Blocks:                []BlockOutcome{},
		TotalOriginalTokens:   1000,
		TotalCompressedTokens: 400,
		Model:                 "claude-3-5-sonnet-20241022",
		AuthMode:              authmode.Payg,
	}
	assert.Empty(t, m.Blocks)
	assert.Equal(t, 1000, m.TotalOriginalTokens)
	assert.Equal(t, 400, m.TotalCompressedTokens)
	assert.Equal(t, "claude-3-5-sonnet-20241022", m.Model)
	assert.Equal(t, authmode.Payg, m.AuthMode)
}

func TestCompressionManifest_JSON(t *testing.T) {
	reason := ExclusionReasonSystemMessage
	m := CompressionManifest{
		Blocks: []BlockOutcome{
			{Index: 0, Action: BlockActionExcluded, ExclusionReason: &reason, OriginalTokens: 50},
			{Index: 1, Action: BlockActionCompressed, Strategy: "smart_crush", OriginalTokens: 500, CompressedTokens: 200},
		},
		TotalOriginalTokens:   550,
		TotalCompressedTokens: 200,
		Model:                 "test-model",
		AuthMode:              authmode.OAuth,
	}
	data, err := json.Marshal(m)
	require.NoError(t, err)

	var decoded CompressionManifest
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)
	assert.Equal(t, 2, len(decoded.Blocks))
	assert.Equal(t, 550, decoded.TotalOriginalTokens)
}

func TestLiveZoneOutcome_Compressed(t *testing.T) {
	manifest := CompressionManifest{TotalOriginalTokens: 100, TotalCompressedTokens: 50}
	outcome := LiveZoneOutcome{
		Kind:     OutcomeCompressed,
		Body:     []byte(`{"messages":[]}`),
		Manifest: &manifest,
	}
	assert.Equal(t, OutcomeCompressed, outcome.Kind)
	assert.NotNil(t, outcome.Body)
	assert.NotNil(t, outcome.Manifest)
	assert.Empty(t, outcome.Reason)
	assert.Nil(t, outcome.Err)
}

func TestLiveZoneOutcome_NoCompression(t *testing.T) {
	outcome := LiveZoneOutcome{
		Kind:   OutcomeNoCompression,
		Reason: "below_threshold",
	}
	assert.Equal(t, OutcomeNoCompression, outcome.Kind)
	assert.Nil(t, outcome.Body)
	assert.Nil(t, outcome.Manifest)
	assert.Equal(t, "below_threshold", outcome.Reason)
}

func TestLiveZoneOutcome_Error(t *testing.T) {
	outcome := LiveZoneOutcome{
		Kind: OutcomeError,
		Err:  ErrInvalidBody,
	}
	assert.Equal(t, OutcomeError, outcome.Kind)
	assert.ErrorIs(t, outcome.Err, ErrInvalidBody)
}

func TestLiveZoneErrors(t *testing.T) {
	assert.Error(t, ErrInvalidBody)
	assert.Error(t, ErrNoMessages)
	assert.Error(t, ErrCompressionFailed)
	assert.Error(t, ErrTokenizerFailed)
	assert.Error(t, ErrUnsupportedFormat)
}

func TestDefaultModel(t *testing.T) {
	assert.Equal(t, "claude-3-5-sonnet-20241022", DefaultModel)
}
