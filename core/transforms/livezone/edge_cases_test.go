package livezone

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/projectbarks/goheadroom/core/authmode"
	"github.com/projectbarks/goheadroom/core/compressionpolicy"
)

func TestAnthropicLiveZone_NoToolResults(t *testing.T) {
	messages := []interface{}{
		msg("user", makeCompressibleJSON(100)),
		msg("assistant", "Here is my response about that topic."),
		msg("user", "Tell me more."),
	}
	body := anthropicBody(t, nil, messages, nil, "")
	outcome := CompressAnthropicLiveZone(body, compressionpolicy.Auto, authmode.Payg, "req-edge-1")
	assert.NotEqual(t, OutcomeError, outcome.Kind)
}

func TestAnthropicLiveZone_MultipleToolResults(t *testing.T) {
	messages := []interface{}{
		msg("user", "Read both files"),
		msg("assistant", []interface{}{
			toolUseBlock("tu_1", "read_file", map[string]interface{}{"path": "/a.txt"}),
			toolUseBlock("tu_2", "read_file", map[string]interface{}{"path": "/b.txt"}),
		}),
		msg("user", []interface{}{
			toolResultBlock("tu_1", makeCompressibleJSON(50)),
			toolResultBlock("tu_2", makeCompressibleJSON(50)),
		}),
	}
	body := anthropicBody(t, nil, messages, nil, "")
	outcome := CompressAnthropicLiveZone(body, compressionpolicy.Auto, authmode.Payg, "req-edge-2")

	assert.Equal(t, OutcomeCompressed, outcome.Kind)
	require.NotNil(t, outcome.Manifest)
}

func TestAnthropicLiveZone_SystemAsArray(t *testing.T) {
	systemBlocks := []interface{}{
		textBlock("You are a helpful coding assistant."),
		textBlock("Always be concise."),
	}
	messages := []interface{}{
		msg("user", "Hello"),
		msg("assistant", []interface{}{toolUseBlock("tu_1", "read", map[string]interface{}{})}),
		msg("user", []interface{}{toolResultBlock("tu_1", makeCompressibleJSON(100))}),
	}
	body := anthropicBody(t, systemBlocks, messages, nil, "")
	outcome := CompressAnthropicLiveZone(body, compressionpolicy.Auto, authmode.Payg, "req-edge-3")
	assert.NotEqual(t, OutcomeError, outcome.Kind)
}

func TestOpenAIChatLiveZone_MultipleToolMessages(t *testing.T) {
	messages := []interface{}{
		oaiMsg("user", "Read both files"),
		oaiAssistantWithToolCalls("", []interface{}{
			map[string]interface{}{
				"id": "call_1", "type": "function",
				"function": map[string]interface{}{"name": "read", "arguments": `{"path":"/a"}`},
			},
			map[string]interface{}{
				"id": "call_2", "type": "function",
				"function": map[string]interface{}{"name": "read", "arguments": `{"path":"/b"}`},
			},
		}),
		oaiToolMsg("call_1", makeCompressibleJSON(50)),
		oaiToolMsg("call_2", makeCompressibleJSON(50)),
	}
	body := openaiChatBody(t, messages, "gpt-4")
	outcome := CompressOpenAIChatLiveZone(body, compressionpolicy.Auto, authmode.Payg, "req-edge-4")

	assert.Equal(t, OutcomeCompressed, outcome.Kind)
	require.NotNil(t, outcome.Manifest)
}

func TestAnthropicLiveZone_CompressionPolicyOff(t *testing.T) {
	messages := []interface{}{
		msg("user", "Read"),
		msg("assistant", []interface{}{toolUseBlock("tu_1", "read", map[string]interface{}{})}),
		msg("user", []interface{}{toolResultBlock("tu_1", makeCompressibleJSON(150))}),
	}
	body := anthropicBody(t, nil, messages, nil, "")
	outcome := CompressAnthropicLiveZone(body, compressionpolicy.Off, authmode.Payg, "req-edge-5")
	assert.Equal(t, OutcomeNoCompression, outcome.Kind)
}

func TestOpenAIChatLiveZone_CompressionPolicyOff(t *testing.T) {
	messages := []interface{}{
		oaiMsg("user", "Read"),
		oaiAssistantWithToolCalls("", []interface{}{
			map[string]interface{}{
				"id": "call_1", "type": "function",
				"function": map[string]interface{}{"name": "read", "arguments": "{}"},
			},
		}),
		oaiToolMsg("call_1", makeCompressibleJSON(150)),
	}
	body := openaiChatBody(t, messages, "gpt-4")
	outcome := CompressOpenAIChatLiveZone(body, compressionpolicy.Off, authmode.Payg, "req-edge-6")
	assert.Equal(t, OutcomeNoCompression, outcome.Kind)
}

func TestAnthropicLiveZone_AllFieldsPreserved(t *testing.T) {
	bodyMap := map[string]interface{}{
		"model":       DefaultModel,
		"max_tokens":  4096,
		"temperature": 0.7,
		"top_p":       0.9,
		"stream":      true,
		"messages": []interface{}{
			msg("user", "Read"),
			msg("assistant", []interface{}{toolUseBlock("tu_1", "read", map[string]interface{}{})}),
			msg("user", []interface{}{toolResultBlock("tu_1", makeCompressibleJSON(100))}),
		},
	}
	body, err := json.Marshal(bodyMap)
	require.NoError(t, err)

	outcome := CompressAnthropicLiveZone(body, compressionpolicy.Auto, authmode.Payg, "req-edge-7")

	if outcome.Kind == OutcomeCompressed {
		var result map[string]interface{}
		err := json.Unmarshal(outcome.Body, &result)
		require.NoError(t, err)

		assert.Equal(t, float64(4096), result["max_tokens"])
		assert.Equal(t, 0.7, result["temperature"])
		assert.Equal(t, 0.9, result["top_p"])
		assert.Equal(t, true, result["stream"])
	}
}
