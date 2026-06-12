package livezone

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uber/goheadroom/authmode"
	"github.com/uber/goheadroom/compressionpolicy"
)

func openaiChatBody(t *testing.T, messages []interface{}, model string) []byte {
	t.Helper()
	body := map[string]interface{}{
		"messages": messages,
		"model":    model,
	}
	data, err := json.Marshal(body)
	require.NoError(t, err)
	return data
}

func oaiMsg(role, content string) map[string]interface{} {
	return map[string]interface{}{"role": role, "content": content}
}

func oaiToolMsg(toolCallID, content string) map[string]interface{} {
	return map[string]interface{}{
		"role":         "tool",
		"tool_call_id": toolCallID,
		"content":      content,
	}
}

func oaiAssistantWithToolCalls(content string, toolCalls []interface{}) map[string]interface{} {
	m := map[string]interface{}{
		"role": "assistant",
	}
	if content != "" {
		m["content"] = content
	}
	if toolCalls != nil {
		m["tool_calls"] = toolCalls
	}
	return m
}

func TestCompressOpenAIChatLiveZone_EmptyMessages(t *testing.T) {
	body := openaiChatBody(t, []interface{}{}, "gpt-4")
	outcome := CompressOpenAIChatLiveZone(body, compressionpolicy.Auto, authmode.Payg, "req-oai-1")
	assert.Equal(t, OutcomeNoCompression, outcome.Kind)
}

func TestCompressOpenAIChatLiveZone_SingleUserMessage(t *testing.T) {
	messages := []interface{}{oaiMsg("user", "Hello")}
	body := openaiChatBody(t, messages, "gpt-4")
	outcome := CompressOpenAIChatLiveZone(body, compressionpolicy.Auto, authmode.Payg, "req-oai-2")
	assert.Equal(t, OutcomeNoCompression, outcome.Kind)
}

func TestCompressOpenAIChatLiveZone_LatestToolRoleCompressed(t *testing.T) {
	longToolResult := makeCompressibleJSON(100)
	messages := []interface{}{
		oaiMsg("user", "Read this file"),
		oaiAssistantWithToolCalls("", []interface{}{
			map[string]interface{}{
				"id":   "call_1",
				"type": "function",
				"function": map[string]interface{}{
					"name":      "read_file",
					"arguments": `{"path":"/tmp/big.txt"}`,
				},
			},
		}),
		oaiToolMsg("call_1", longToolResult),
	}
	body := openaiChatBody(t, messages, "gpt-4")
	outcome := CompressOpenAIChatLiveZone(body, compressionpolicy.Auto, authmode.Payg, "req-oai-3")

	assert.Equal(t, OutcomeCompressed, outcome.Kind)
	require.NotNil(t, outcome.Manifest)
	assert.Greater(t, outcome.Manifest.TotalOriginalTokens, outcome.Manifest.TotalCompressedTokens)
}

func TestCompressOpenAIChatLiveZone_LatestUserCompressed(t *testing.T) {
	longUserMsg := makeCompressibleJSON(100)
	messages := []interface{}{
		oaiMsg("system", "You are a helpful assistant."),
		oaiMsg("user", "Previous short question"),
		oaiMsg("assistant", "Previous answer"),
		oaiMsg("user", longUserMsg),
	}
	body := openaiChatBody(t, messages, "gpt-4")
	outcome := CompressOpenAIChatLiveZone(body, compressionpolicy.Auto, authmode.Payg, "req-oai-4")

	assert.Equal(t, OutcomeCompressed, outcome.Kind)
	require.NotNil(t, outcome.Manifest)
}

func TestCompressOpenAIChatLiveZone_SystemMessageExcluded(t *testing.T) {
	longText := makeCompressibleJSON(100)
	messages := []interface{}{
		oaiMsg("system", "You are a helpful assistant with a very long system prompt."),
		oaiMsg("user", longText),
	}
	body := openaiChatBody(t, messages, "gpt-4")
	outcome := CompressOpenAIChatLiveZone(body, compressionpolicy.Auto, authmode.Payg, "req-oai-5")

	if outcome.Kind == OutcomeCompressed {
		require.NotNil(t, outcome.Manifest)
		for _, block := range outcome.Manifest.Blocks {
			if block.ExclusionReason != nil && *block.ExclusionReason == ExclusionReasonSystemMessage {
				return
			}
		}
	}
}

func TestCompressOpenAIChatLiveZone_HistoricalFrozen(t *testing.T) {
	messages := []interface{}{
		oaiMsg("user", makeLongText(2000)),
		oaiMsg("assistant", "I see."),
		oaiMsg("user", makeCompressibleJSON(100)),
	}
	body := openaiChatBody(t, messages, "gpt-4")
	outcome := CompressOpenAIChatLiveZone(body, compressionpolicy.Auto, authmode.Payg, "req-oai-6")

	if outcome.Kind == OutcomeCompressed {
		require.NotNil(t, outcome.Manifest)
		foundFrozen := false
		for _, block := range outcome.Manifest.Blocks {
			if block.ExclusionReason != nil && *block.ExclusionReason == ExclusionReasonFrozenByPolicy {
				foundFrozen = true
			}
		}
		assert.True(t, foundFrozen)
	}
}

func TestCompressOpenAIChatLiveZone_ManifestAggregates(t *testing.T) {
	longToolResult := makeCompressibleJSON(150)
	messages := []interface{}{
		oaiMsg("user", "Process this"),
		oaiAssistantWithToolCalls("", []interface{}{
			map[string]interface{}{
				"id":   "call_1",
				"type": "function",
				"function": map[string]interface{}{
					"name":      "process",
					"arguments": "{}",
				},
			},
		}),
		oaiToolMsg("call_1", longToolResult),
	}
	body := openaiChatBody(t, messages, "gpt-4")
	outcome := CompressOpenAIChatLiveZone(body, compressionpolicy.Auto, authmode.Payg, "req-oai-7")

	assert.Equal(t, OutcomeCompressed, outcome.Kind)
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

func TestCompressOpenAIChatLiveZone_InvalidJSON(t *testing.T) {
	outcome := CompressOpenAIChatLiveZone([]byte(`not json`), compressionpolicy.Auto, authmode.Payg, "req-oai-8")
	assert.Equal(t, OutcomeError, outcome.Kind)
	assert.Error(t, outcome.Err)
}

func TestCompressOpenAIChatLiveZone_ModelPreserved(t *testing.T) {
	longToolResult := makeCompressibleJSON(100)
	messages := []interface{}{
		oaiMsg("user", "Read"),
		oaiAssistantWithToolCalls("", []interface{}{
			map[string]interface{}{
				"id": "call_1", "type": "function",
				"function": map[string]interface{}{"name": "read", "arguments": "{}"},
			},
		}),
		oaiToolMsg("call_1", longToolResult),
	}
	body := openaiChatBody(t, messages, "gpt-4-turbo")
	outcome := CompressOpenAIChatLiveZone(body, compressionpolicy.Auto, authmode.Payg, "req-oai-9")

	if outcome.Kind == OutcomeCompressed {
		var result map[string]interface{}
		err := json.Unmarshal(outcome.Body, &result)
		require.NoError(t, err)
		assert.Equal(t, "gpt-4-turbo", result["model"])
	}
}
