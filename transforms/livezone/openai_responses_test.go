package livezone

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uber/goheadroom/authmode"
	"github.com/uber/goheadroom/compressionpolicy"
)

func openaiResponsesBody(t *testing.T, input interface{}, model string) []byte {
	t.Helper()
	body := map[string]interface{}{
		"input": input,
		"model": model,
	}
	data, err := json.Marshal(body)
	require.NoError(t, err)
	return data
}

func TestCompressOpenAIResponsesLiveZone_EmptyInput(t *testing.T) {
	body := openaiResponsesBody(t, []interface{}{}, "gpt-4")
	outcome := CompressOpenAIResponsesLiveZone(body, compressionpolicy.Auto, authmode.Payg, "req-resp-1")
	assert.Equal(t, OutcomeNoCompression, outcome.Kind)
}

func TestCompressOpenAIResponsesLiveZone_StringInput(t *testing.T) {
	bodyMap := map[string]interface{}{
		"input": "Hello, summarize this.",
		"model": "gpt-4",
	}
	body, _ := json.Marshal(bodyMap)
	outcome := CompressOpenAIResponsesLiveZone(body, compressionpolicy.Auto, authmode.Payg, "req-resp-2")
	assert.Equal(t, OutcomeNoCompression, outcome.Kind)
}

func TestCompressOpenAIResponsesLiveZone_LargeInputCompressed(t *testing.T) {
	longText := makeCompressibleJSON(150)
	input := []interface{}{
		map[string]interface{}{
			"type": "message",
			"role": "user",
			"content": []interface{}{
				map[string]interface{}{
					"type": "input_text",
					"text": longText,
				},
			},
		},
	}
	body := openaiResponsesBody(t, input, "gpt-4")
	outcome := CompressOpenAIResponsesLiveZone(body, compressionpolicy.Auto, authmode.Payg, "req-resp-3")

	assert.Equal(t, OutcomeCompressed, outcome.Kind)
	require.NotNil(t, outcome.Manifest)
	assert.Greater(t, outcome.Manifest.TotalOriginalTokens, outcome.Manifest.TotalCompressedTokens)
}

func TestCompressOpenAIResponsesLiveZone_FunctionOutputCompressed(t *testing.T) {
	longOutput := makeCompressibleJSON(100)
	input := []interface{}{
		map[string]interface{}{
			"type": "message",
			"role": "user",
			"content": []interface{}{
				map[string]interface{}{
					"type": "input_text",
					"text": "Process the data",
				},
			},
		},
		map[string]interface{}{
			"type":    "function_call_output",
			"call_id": "fc_1",
			"output":  longOutput,
		},
	}
	body := openaiResponsesBody(t, input, "gpt-4")
	outcome := CompressOpenAIResponsesLiveZone(body, compressionpolicy.Auto, authmode.Payg, "req-resp-4")

	assert.Equal(t, OutcomeCompressed, outcome.Kind)
	require.NotNil(t, outcome.Manifest)
}

func TestCompressOpenAIResponsesLiveZone_ManifestTracking(t *testing.T) {
	longText := makeCompressibleJSON(150)
	input := []interface{}{
		map[string]interface{}{
			"type": "message",
			"role": "user",
			"content": []interface{}{
				map[string]interface{}{"type": "input_text", "text": longText},
			},
		},
	}
	body := openaiResponsesBody(t, input, "gpt-4")
	outcome := CompressOpenAIResponsesLiveZone(body, compressionpolicy.Auto, authmode.Payg, "req-resp-5")

	if outcome.Kind == OutcomeCompressed {
		require.NotNil(t, outcome.Manifest)
		sumOrig := 0
		sumComp := 0
		for _, b := range outcome.Manifest.Blocks {
			sumOrig += b.OriginalTokens
			sumComp += b.CompressedTokens
		}
		assert.Equal(t, outcome.Manifest.TotalOriginalTokens, sumOrig)
		assert.Equal(t, outcome.Manifest.TotalCompressedTokens, sumComp)
	}
}

func TestCompressOpenAIResponsesLiveZone_InvalidJSON(t *testing.T) {
	outcome := CompressOpenAIResponsesLiveZone([]byte(`{bad`), compressionpolicy.Auto, authmode.Payg, "req-resp-6")
	assert.Equal(t, OutcomeError, outcome.Kind)
}

func TestSummarizeOpenAIResponsesNoChangeReason(t *testing.T) {
	manifest := &CompressionManifest{
		TotalOriginalTokens:   100,
		TotalCompressedTokens: 100,
	}
	reason := SummarizeOpenAIResponsesNoChangeReason(manifest)
	assert.NotEmpty(t, reason)
}

func TestSummarizeOpenAIResponsesNoChangeReason_NilManifest(t *testing.T) {
	reason := SummarizeOpenAIResponsesNoChangeReason(nil)
	assert.NotEmpty(t, reason)
}
