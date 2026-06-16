package core

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/projectbarks/goheadroom/core/authmode"
	"github.com/projectbarks/goheadroom/core/compressionpolicy"
	"github.com/projectbarks/goheadroom/core/transforms/livezone"
)

// makeCompressibleJSON generates a large JSON array of duplicate/similar objects
// that SmartCrusher can compress.
func makeCompressibleJSONIntegration(items int) string {
	var entries []map[string]interface{}
	for i := 0; i < items; i++ {
		entries = append(entries, map[string]interface{}{
			"event":  "heartbeat",
			"ok":     true,
			"seq":    i,
			"status": "active",
		})
	}
	data, _ := json.Marshal(entries)
	return string(data)
}

func anthropicRequestBody(t *testing.T, messages []interface{}) []byte {
	t.Helper()
	body := map[string]interface{}{
		"model":    "claude-3-5-sonnet-20241022",
		"messages": messages,
	}
	data, err := json.Marshal(body)
	require.NoError(t, err)
	return data
}

func openaiChatRequestBody(t *testing.T, messages []interface{}) []byte {
	t.Helper()
	body := map[string]interface{}{
		"model":    "gpt-4",
		"messages": messages,
	}
	data, err := json.Marshal(body)
	require.NoError(t, err)
	return data
}

func TestCompressLiveZone_Anthropic(t *testing.T) {
	longToolResult := makeCompressibleJSONIntegration(100)
	messages := []interface{}{
		map[string]interface{}{"role": "user", "content": "Read the file"},
		map[string]interface{}{"role": "assistant", "content": []interface{}{
			map[string]interface{}{
				"type": "tool_use", "id": "tu_1", "name": "read_file",
				"input": map[string]interface{}{"path": "/tmp/big.txt"},
			},
		}},
		map[string]interface{}{"role": "user", "content": []interface{}{
			map[string]interface{}{
				"type": "tool_result", "tool_use_id": "tu_1", "content": longToolResult,
			},
		}},
	}
	body := anthropicRequestBody(t, messages)

	req := CompressRequest{
		Body:      body,
		Format:    FormatAnthropic,
		Mode:      compressionpolicy.Auto,
		AuthMode:  authmode.Payg,
		RequestID: "integration-1",
	}

	outcome := CompressLiveZone(req)
	assert.Equal(t, livezone.OutcomeCompressed, outcome.Kind)
	require.NotNil(t, outcome.Manifest)
	assert.Greater(t, outcome.Manifest.TotalOriginalTokens, outcome.Manifest.TotalCompressedTokens)
}

func TestCompressLiveZone_OpenAIChat(t *testing.T) {
	longToolResult := makeCompressibleJSONIntegration(100)
	messages := []interface{}{
		map[string]interface{}{"role": "user", "content": "Read the file"},
		map[string]interface{}{
			"role": "assistant",
			"tool_calls": []interface{}{
				map[string]interface{}{
					"id": "call_1", "type": "function",
					"function": map[string]interface{}{"name": "read_file", "arguments": `{"path":"/tmp/big.txt"}`},
				},
			},
		},
		map[string]interface{}{"role": "tool", "tool_call_id": "call_1", "content": longToolResult},
	}
	body := openaiChatRequestBody(t, messages)

	req := CompressRequest{
		Body:      body,
		Format:    FormatOpenAIChat,
		Mode:      compressionpolicy.Auto,
		AuthMode:  authmode.Payg,
		RequestID: "integration-2",
	}

	outcome := CompressLiveZone(req)
	assert.Equal(t, livezone.OutcomeCompressed, outcome.Kind)
	require.NotNil(t, outcome.Manifest)
}

func TestCompressLiveZone_OpenAIResponses(t *testing.T) {
	longText := makeCompressibleJSONIntegration(150)
	bodyMap := map[string]interface{}{
		"model": "gpt-4",
		"input": []interface{}{
			map[string]interface{}{
				"type": "message",
				"role": "user",
				"content": []interface{}{
					map[string]interface{}{"type": "input_text", "text": longText},
				},
			},
		},
	}
	body, err := json.Marshal(bodyMap)
	require.NoError(t, err)

	req := CompressRequest{
		Body:      body,
		Format:    FormatOpenAIResponses,
		Mode:      compressionpolicy.Auto,
		AuthMode:  authmode.Payg,
		RequestID: "integration-3",
	}

	outcome := CompressLiveZone(req)
	assert.Equal(t, livezone.OutcomeCompressed, outcome.Kind)
}

func TestCompressLiveZone_UnknownFormat(t *testing.T) {
	req := CompressRequest{
		Body:      []byte(`{}`),
		Format:    RequestFormat("unknown"),
		Mode:      compressionpolicy.Auto,
		AuthMode:  authmode.Payg,
		RequestID: "integration-4",
	}

	outcome := CompressLiveZone(req)
	assert.Equal(t, livezone.OutcomeError, outcome.Kind)
	assert.ErrorIs(t, outcome.Err, livezone.ErrUnsupportedFormat)
}

func TestCompressLiveZone_PolicyOff(t *testing.T) {
	longToolResult := makeCompressibleJSONIntegration(100)
	messages := []interface{}{
		map[string]interface{}{"role": "user", "content": "Read"},
		map[string]interface{}{"role": "assistant", "content": []interface{}{
			map[string]interface{}{"type": "tool_use", "id": "tu_1", "name": "r", "input": map[string]interface{}{}},
		}},
		map[string]interface{}{"role": "user", "content": []interface{}{
			map[string]interface{}{"type": "tool_result", "tool_use_id": "tu_1", "content": longToolResult},
		}},
	}
	body := anthropicRequestBody(t, messages)

	req := CompressRequest{
		Body:      body,
		Format:    FormatAnthropic,
		Mode:      compressionpolicy.Off,
		AuthMode:  authmode.Payg,
		RequestID: "integration-5",
	}

	outcome := CompressLiveZone(req)
	assert.Equal(t, livezone.OutcomeNoCompression, outcome.Kind)
}

func TestCompressLiveZone_WithCCR(t *testing.T) {
	longToolResult := makeCompressibleJSONIntegration(100)
	messages := []interface{}{
		map[string]interface{}{"role": "user", "content": "Read"},
		map[string]interface{}{"role": "assistant", "content": []interface{}{
			map[string]interface{}{"type": "tool_use", "id": "tu_1", "name": "r", "input": map[string]interface{}{}},
		}},
		map[string]interface{}{"role": "user", "content": []interface{}{
			map[string]interface{}{"type": "tool_result", "tool_use_id": "tu_1", "content": longToolResult},
		}},
	}
	body := anthropicRequestBody(t, messages)

	req := CompressRequest{
		Body:      body,
		Format:    FormatAnthropic,
		Mode:      compressionpolicy.Auto,
		AuthMode:  authmode.Payg,
		RequestID: "integration-6",
		EnableCCR: true,
	}

	outcome := CompressLiveZone(req)
	assert.Equal(t, livezone.OutcomeCompressed, outcome.Kind)
}

func TestCompressRequest_Defaults(t *testing.T) {
	req := CompressRequest{
		Body:   []byte(`{}`),
		Format: FormatAnthropic,
	}
	assert.Equal(t, FormatAnthropic, req.Format)
	assert.False(t, req.EnableCCR)
	assert.Empty(t, req.RequestID)
}
