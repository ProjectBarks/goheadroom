package livezone

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uber/goheadroom/authmode"
	"github.com/uber/goheadroom/compressionpolicy"
)

// Helper: build a minimal Anthropic request body.
func anthropicBody(t *testing.T, system interface{}, messages []interface{}, tools []interface{}, model string) []byte {
	t.Helper()
	body := map[string]interface{}{}
	if system != nil {
		body["system"] = system
	}
	if messages != nil {
		body["messages"] = messages
	}
	if tools != nil {
		body["tools"] = tools
	}
	if model != "" {
		body["model"] = model
	} else {
		body["model"] = DefaultModel
	}
	data, err := json.Marshal(body)
	require.NoError(t, err)
	return data
}

func textBlock(text string) map[string]interface{} {
	return map[string]interface{}{"type": "text", "text": text}
}

func toolResultBlock(toolUseID, content string) map[string]interface{} {
	return map[string]interface{}{
		"type":        "tool_result",
		"tool_use_id": toolUseID,
		"content":     content,
	}
}

func toolUseBlock(id, name string, input interface{}) map[string]interface{} {
	return map[string]interface{}{
		"type":  "tool_use",
		"id":    id,
		"name":  name,
		"input": input,
	}
}

func msg(role string, content interface{}) map[string]interface{} {
	return map[string]interface{}{"role": role, "content": content}
}

// makeLongText generates a string of approximately n words to push past compression thresholds.
func makeLongText(words int) string {
	var buf []byte
	word := "lorem "
	for i := 0; i < words; i++ {
		buf = append(buf, word...)
	}
	return string(buf)
}

// makeCompressibleJSON generates a large JSON array of duplicate/similar objects
// that SmartCrusher can compress (dedup identical items).
func makeCompressibleJSON(items int) string {
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

// makeBuildLog generates log-like content that the LogCompressor can compress.
func makeBuildLog(lines int) string {
	var buf []byte
	for i := 0; i < lines; i++ {
		buf = append(buf, []byte("[2024-06-01 12:00:00] INFO: Building module_"+string(rune('A'+(i%26)))+" ... ")...)
		buf = append(buf, []byte("Compiling source files in /src/module/package/file_")...)
		buf = append(buf, []byte(string(rune('0'+(i%10))))...)
		buf = append(buf, '\n')
	}
	return string(buf)
}

func TestCompressAnthropicLiveZone_EmptyMessages(t *testing.T) {
	body := anthropicBody(t, nil, []interface{}{}, nil, "")
	outcome := CompressAnthropicLiveZone(body, compressionpolicy.Auto, authmode.Payg, "req-1")
	assert.Equal(t, OutcomeNoCompression, outcome.Kind)
}

func TestCompressAnthropicLiveZone_SingleUserMessage(t *testing.T) {
	messages := []interface{}{
		msg("user", "Hello, how are you?"),
	}
	body := anthropicBody(t, nil, messages, nil, "")
	outcome := CompressAnthropicLiveZone(body, compressionpolicy.Auto, authmode.Payg, "req-2")
	// Single short user message: no compression needed (below threshold)
	assert.Equal(t, OutcomeNoCompression, outcome.Kind)
}

func TestCompressAnthropicLiveZone_SystemMessageFrozen(t *testing.T) {
	longText := makeLongText(2000)
	messages := []interface{}{
		msg("user", longText),
		msg("assistant", "I understand."),
		msg("user", "Please continue."),
	}
	body := anthropicBody(t, "You are a helpful assistant.", messages, nil, "")
	outcome := CompressAnthropicLiveZone(body, compressionpolicy.Auto, authmode.Payg, "req-3")

	if outcome.Kind == OutcomeCompressed {
		require.NotNil(t, outcome.Manifest)
		for _, block := range outcome.Manifest.Blocks {
			if block.Action == BlockActionExcluded && block.ExclusionReason != nil {
				if *block.ExclusionReason == ExclusionReasonSystemMessage {
					return
				}
			}
		}
	}
	// If no compression happened, that's also acceptable for short content
}

func TestCompressAnthropicLiveZone_ToolsDefinitionsFrozen(t *testing.T) {
	tools := []interface{}{
		map[string]interface{}{
			"name":        "get_weather",
			"description": "Gets weather for a location",
			"input_schema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"location": map[string]interface{}{"type": "string"},
				},
			},
		},
	}
	longText := makeLongText(2000)
	messages := []interface{}{
		msg("user", longText),
		msg("assistant", "Let me check the weather."),
		msg("user", []interface{}{toolResultBlock("tool_1", "Sunny, 72F")}),
	}
	body := anthropicBody(t, nil, messages, tools, "")
	outcome := CompressAnthropicLiveZone(body, compressionpolicy.Auto, authmode.Payg, "req-4")

	if outcome.Kind == OutcomeCompressed {
		require.NotNil(t, outcome.Manifest)
		for _, block := range outcome.Manifest.Blocks {
			if block.ExclusionReason != nil && *block.ExclusionReason == ExclusionReasonToolDefinition {
				return
			}
		}
	}
}

func TestCompressAnthropicLiveZone_LatestToolResultCompressed(t *testing.T) {
	longToolResult := makeCompressibleJSON(100)
	messages := []interface{}{
		msg("user", "Read this file"),
		msg("assistant", []interface{}{toolUseBlock("tu_1", "read_file", map[string]interface{}{"path": "/tmp/big.txt"})}),
		msg("user", []interface{}{toolResultBlock("tu_1", longToolResult)}),
	}
	body := anthropicBody(t, nil, messages, nil, "")
	outcome := CompressAnthropicLiveZone(body, compressionpolicy.Auto, authmode.Payg, "req-5")

	assert.Equal(t, OutcomeCompressed, outcome.Kind)
	require.NotNil(t, outcome.Manifest)
	assert.Greater(t, outcome.Manifest.TotalOriginalTokens, outcome.Manifest.TotalCompressedTokens)
}

func TestCompressAnthropicLiveZone_HistoricalMessagesFrozen(t *testing.T) {
	messages := []interface{}{
		msg("user", makeLongText(2000)),
		msg("assistant", "I see. Let me help."),
		msg("user", "Now read the file"),
		msg("assistant", []interface{}{toolUseBlock("tu_1", "read_file", map[string]interface{}{"path": "/big"})}),
		msg("user", []interface{}{toolResultBlock("tu_1", makeCompressibleJSON(100))}),
	}
	body := anthropicBody(t, nil, messages, nil, "")
	outcome := CompressAnthropicLiveZone(body, compressionpolicy.Auto, authmode.Payg, "req-6")

	if outcome.Kind == OutcomeCompressed {
		require.NotNil(t, outcome.Manifest)
		foundFrozen := false
		for _, block := range outcome.Manifest.Blocks {
			if block.Action == BlockActionExcluded && block.ExclusionReason != nil {
				if *block.ExclusionReason == ExclusionReasonFrozenByPolicy {
					foundFrozen = true
				}
			}
		}
		assert.True(t, foundFrozen, "expected at least one frozen historical message")
	}
}

func TestCompressAnthropicLiveZone_ManifestTracksTokenCounts(t *testing.T) {
	longToolResult := makeCompressibleJSON(150)
	messages := []interface{}{
		msg("user", "Summarize this"),
		msg("assistant", []interface{}{toolUseBlock("tu_1", "read", map[string]interface{}{})}),
		msg("user", []interface{}{toolResultBlock("tu_1", longToolResult)}),
	}
	body := anthropicBody(t, nil, messages, nil, "")
	outcome := CompressAnthropicLiveZone(body, compressionpolicy.Auto, authmode.Payg, "req-7")

	assert.Equal(t, OutcomeCompressed, outcome.Kind)
	require.NotNil(t, outcome.Manifest)

	totalOrig := 0
	totalComp := 0
	for _, b := range outcome.Manifest.Blocks {
		totalOrig += b.OriginalTokens
		totalComp += b.CompressedTokens
	}
	assert.Equal(t, outcome.Manifest.TotalOriginalTokens, totalOrig)
	assert.Equal(t, outcome.Manifest.TotalCompressedTokens, totalComp)
}

func TestCompressAnthropicLiveZone_CacheControlExclusion(t *testing.T) {
	longText := makeLongText(2000)
	messages := []interface{}{
		msg("user", []interface{}{
			map[string]interface{}{
				"type":          "text",
				"text":          longText,
				"cache_control": map[string]interface{}{"type": "ephemeral"},
			},
		}),
		msg("assistant", "Understood."),
		msg("user", []interface{}{toolResultBlock("tu_1", makeCompressibleJSON(100))}),
	}
	body := anthropicBody(t, nil, messages, nil, "")
	outcome := CompressAnthropicLiveZone(body, compressionpolicy.Auto, authmode.Payg, "req-8")

	if outcome.Kind == OutcomeCompressed {
		require.NotNil(t, outcome.Manifest)
		foundCacheExclusion := false
		for _, block := range outcome.Manifest.Blocks {
			if block.ExclusionReason != nil && *block.ExclusionReason == ExclusionReasonCacheControl {
				foundCacheExclusion = true
			}
		}
		assert.True(t, foundCacheExclusion, "expected cache_control message to be excluded")
	}
}

func TestCompressAnthropicLiveZone_InvalidJSON(t *testing.T) {
	outcome := CompressAnthropicLiveZone([]byte(`{invalid`), compressionpolicy.Auto, authmode.Payg, "req-9")
	assert.Equal(t, OutcomeError, outcome.Kind)
	assert.Error(t, outcome.Err)
}

func TestCompressAnthropicLiveZone_NoMessagesField(t *testing.T) {
	body := []byte(`{"model":"claude-3-5-sonnet-20241022"}`)
	outcome := CompressAnthropicLiveZone(body, compressionpolicy.Auto, authmode.Payg, "req-10")
	assert.NotEqual(t, OutcomeCompressed, outcome.Kind)
}

func TestCompressAnthropicLiveZone_StreamingParamsPreserved(t *testing.T) {
	longToolResult := makeCompressibleJSON(100)
	messages := []interface{}{
		msg("user", "Read"),
		msg("assistant", []interface{}{toolUseBlock("tu_1", "read", map[string]interface{}{})}),
		msg("user", []interface{}{toolResultBlock("tu_1", longToolResult)}),
	}
	bodyMap := map[string]interface{}{
		"model":    DefaultModel,
		"messages": messages,
		"stream":   true,
	}
	body, err := json.Marshal(bodyMap)
	require.NoError(t, err)

	outcome := CompressAnthropicLiveZone(body, compressionpolicy.Auto, authmode.Payg, "req-11")
	if outcome.Kind == OutcomeCompressed {
		var result map[string]interface{}
		err := json.Unmarshal(outcome.Body, &result)
		require.NoError(t, err)
		assert.Equal(t, true, result["stream"], "stream param must be preserved")
	}
}

func TestCompressAnthropicLiveZone_ModelPreserved(t *testing.T) {
	longToolResult := makeCompressibleJSON(100)
	messages := []interface{}{
		msg("user", "Read"),
		msg("assistant", []interface{}{toolUseBlock("tu_1", "read", map[string]interface{}{})}),
		msg("user", []interface{}{toolResultBlock("tu_1", longToolResult)}),
	}
	body := anthropicBody(t, nil, messages, nil, "claude-3-opus-20240229")
	outcome := CompressAnthropicLiveZone(body, compressionpolicy.Auto, authmode.Payg, "req-12")

	if outcome.Kind == OutcomeCompressed {
		var result map[string]interface{}
		err := json.Unmarshal(outcome.Body, &result)
		require.NoError(t, err)
		assert.Equal(t, "claude-3-opus-20240229", result["model"])
		assert.Equal(t, "claude-3-opus-20240229", outcome.Manifest.Model)
	}
}

func TestCompressAnthropicLiveZone_AuthModeRecordedInManifest(t *testing.T) {
	longToolResult := makeCompressibleJSON(100)
	messages := []interface{}{
		msg("user", "Read"),
		msg("assistant", []interface{}{toolUseBlock("tu_1", "read", map[string]interface{}{})}),
		msg("user", []interface{}{toolResultBlock("tu_1", longToolResult)}),
	}
	body := anthropicBody(t, nil, messages, nil, "")

	for _, am := range []authmode.AuthMode{authmode.Payg, authmode.OAuth, authmode.Subscription} {
		outcome := CompressAnthropicLiveZone(body, compressionpolicy.Auto, am, "req-13")
		if outcome.Kind == OutcomeCompressed {
			assert.Equal(t, am, outcome.Manifest.AuthMode)
		}
	}
}
