package livezone

import (
	"encoding/json"
	"fmt"

	"github.com/uber/goheadroom/authmode"
	"github.com/uber/goheadroom/cachecontrol"
	"github.com/uber/goheadroom/ccr"
	"github.com/uber/goheadroom/compressionpolicy"
)

// CCRConfig holds configuration for CCR marker injection.
type CCRConfig struct {
	// Placeholder for future config options.
}

// CompressAnthropicLiveZone compresses the live zone of an Anthropic Messages API request body.
// It identifies the latest user message (above the frozen floor), compresses eligible blocks,
// and returns the modified body with a manifest of what was done.
func CompressAnthropicLiveZone(body []byte, mode compressionpolicy.Mode, am authmode.AuthMode, requestID string) LiveZoneOutcome {
	if mode == compressionpolicy.Off {
		return LiveZoneOutcome{Kind: OutcomeNoCompression, Reason: "mode_off"}
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return LiveZoneOutcome{Kind: OutcomeError, Err: fmt.Errorf("%w: %v", ErrInvalidBody, err)}
	}

	messagesRaw, ok := parsed["messages"]
	if !ok {
		return LiveZoneOutcome{Kind: OutcomeNoCompression, Reason: "no_messages"}
	}
	messages, ok := messagesRaw.([]interface{})
	if !ok {
		return LiveZoneOutcome{Kind: OutcomeNoCompression, Reason: "messages_not_array"}
	}
	if len(messages) == 0 {
		return LiveZoneOutcome{Kind: OutcomeNoCompression, Reason: "empty_messages"}
	}

	model, _ := parsed["model"].(string)
	if model == "" {
		model = DefaultModel
	}

	// Compute frozen message count from cache_control markers
	frozenCount := cachecontrol.ComputeFrozenCount(parsed)

	// Find the latest user message index above the frozen floor
	latestUserIdx := -1
	for i := len(messages) - 1; i >= frozenCount; i-- {
		msgMap, ok := messages[i].(map[string]interface{})
		if !ok {
			continue
		}
		if role, _ := msgMap["role"].(string); role == "user" {
			latestUserIdx = i
			break
		}
	}

	if latestUserIdx < 0 {
		return LiveZoneOutcome{Kind: OutcomeNoCompression, Reason: "no_live_user_message"}
	}

	// Walk messages and classify each block
	var blocks []BlockOutcome
	totalOrigTokens := 0
	totalCompTokens := 0
	anyCompressed := false

	// System message exclusion
	if _, hasSys := parsed["system"]; hasSys {
		reason := ExclusionReasonSystemMessage
		blocks = append(blocks, BlockOutcome{
			Index:           -1,
			Action:          BlockActionExcluded,
			ExclusionReason: &reason,
		})
	}

	// Tools exclusion
	if tools, hasTools := parsed["tools"]; hasTools {
		if toolsArr, ok := tools.([]interface{}); ok && len(toolsArr) > 0 {
			reason := ExclusionReasonToolDefinition
			blocks = append(blocks, BlockOutcome{
				Index:           -2,
				Action:          BlockActionExcluded,
				ExclusionReason: &reason,
			})
		}
	}

	// Process each message
	for i, msgRaw := range messages {
		msgMap, ok := msgRaw.(map[string]interface{})
		if !ok {
			continue
		}
		role, _ := msgMap["role"].(string)
		content := msgMap["content"]

		// Messages with cache_control are excluded (check before frozen-by-policy)
		if hasCacheControl(content) {
			reason := ExclusionReasonCacheControl
			blocks = append(blocks, BlockOutcome{
				Index:           i,
				Action:          BlockActionExcluded,
				ExclusionReason: &reason,
			})
			continue
		}

		// Messages below frozen floor are excluded
		if i < frozenCount {
			reason := ExclusionReasonFrozenByPolicy
			blocks = append(blocks, BlockOutcome{
				Index:           i,
				Action:          BlockActionExcluded,
				ExclusionReason: &reason,
			})
			continue
		}

		// Historical messages (before latest user message) are frozen
		if i < latestUserIdx {
			reason := ExclusionReasonFrozenByPolicy
			blocks = append(blocks, BlockOutcome{
				Index:           i,
				Action:          BlockActionExcluded,
				ExclusionReason: &reason,
			})
			continue
		}

		// Latest assistant message is excluded (cache hot zone)
		if role == "assistant" && i > latestUserIdx {
			reason := ExclusionReasonLatestAssistant
			blocks = append(blocks, BlockOutcome{
				Index:           i,
				Action:          BlockActionExcluded,
				ExclusionReason: &reason,
			})
			continue
		}

		// Latest user message - this is the live zone. Compress eligible blocks.
		if i == latestUserIdx {
			slots := extractAnthropicTextSlots(content)
			if len(slots) == 0 {
				// Content too small or not text
				reason := ExclusionReasonTooSmall
				blocks = append(blocks, BlockOutcome{
					Index:           i,
					Action:          BlockActionExcluded,
					ExclusionReason: &reason,
				})
				continue
			}

			for _, slot := range slots {
				compressed, origToks, compToks, strategy, didCompress := CompressText(slot.text, model)
				totalOrigTokens += origToks
				if didCompress {
					totalCompTokens += compToks
					anyCompressed = true

					// Replace the text in the parsed body
					replaceAnthropicContent(messages, i, slot, compressed)

					blocks = append(blocks, BlockOutcome{
						Index:            i,
						Action:           BlockActionCompressed,
						Strategy:         strategy,
						OriginalTokens:   origToks,
						CompressedTokens: compToks,
					})
				} else {
					totalCompTokens += origToks
					blocks = append(blocks, BlockOutcome{
						Index:          i,
						Action:         BlockActionPassthrough,
						OriginalTokens: origToks,
					})
				}
			}
		}
	}

	if !anyCompressed {
		return LiveZoneOutcome{Kind: OutcomeNoCompression, Reason: "nothing_compressible"}
	}

	// Re-serialize the modified body
	newBody, err := json.Marshal(parsed)
	if err != nil {
		return LiveZoneOutcome{Kind: OutcomeError, Err: fmt.Errorf("%w: %v", ErrCompressionFailed, err)}
	}

	manifest := &CompressionManifest{
		Blocks:                blocks,
		TotalOriginalTokens:   totalOrigTokens,
		TotalCompressedTokens: totalCompTokens,
		Model:                 model,
		AuthMode:              am,
	}

	return LiveZoneOutcome{
		Kind:     OutcomeCompressed,
		Body:     newBody,
		Manifest: manifest,
	}
}

// replaceAnthropicContent replaces a text slot's content in the message structure.
func replaceAnthropicContent(messages []interface{}, msgIdx int, slot textSlot, compressed string) {
	msgMap, ok := messages[msgIdx].(map[string]interface{})
	if !ok {
		return
	}

	if slot.index < 0 {
		// String content
		msgMap["content"] = compressed
		return
	}

	// Array content
	contentArr, ok := msgMap["content"].([]interface{})
	if !ok || slot.index >= len(contentArr) {
		return
	}
	blockMap, ok := contentArr[slot.index].(map[string]interface{})
	if !ok {
		return
	}
	blockMap[slot.key] = compressed
}

// CompressAnthropicLiveZoneWithCCR is the CCR-aware variant of CompressAnthropicLiveZone.
// When store is non-nil, compressed blocks get <<ccr:HASH>> markers appended and
// the original content is stored for later retrieval.
// When store is nil, behaves identically to CompressAnthropicLiveZone.
func CompressAnthropicLiveZoneWithCCR(body []byte, mode compressionpolicy.Mode, am authmode.AuthMode, requestID string, store ccr.CcrStore, config *CCRConfig) LiveZoneOutcome {
	if mode == compressionpolicy.Off {
		return LiveZoneOutcome{Kind: OutcomeNoCompression, Reason: "mode_off"}
	}

	// If no store provided, fall back to standard compression
	if store == nil {
		return CompressAnthropicLiveZone(body, mode, am, requestID)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return LiveZoneOutcome{Kind: OutcomeError, Err: fmt.Errorf("%w: %v", ErrInvalidBody, err)}
	}

	messagesRaw, ok := parsed["messages"]
	if !ok {
		return LiveZoneOutcome{Kind: OutcomeNoCompression, Reason: "no_messages"}
	}
	messages, ok := messagesRaw.([]interface{})
	if !ok {
		return LiveZoneOutcome{Kind: OutcomeNoCompression, Reason: "messages_not_array"}
	}
	if len(messages) == 0 {
		return LiveZoneOutcome{Kind: OutcomeNoCompression, Reason: "empty_messages"}
	}

	model, _ := parsed["model"].(string)
	if model == "" {
		model = DefaultModel
	}

	frozenCount := cachecontrol.ComputeFrozenCount(parsed)

	latestUserIdx := -1
	for i := len(messages) - 1; i >= frozenCount; i-- {
		msgMap, ok := messages[i].(map[string]interface{})
		if !ok {
			continue
		}
		if role, _ := msgMap["role"].(string); role == "user" {
			latestUserIdx = i
			break
		}
	}

	if latestUserIdx < 0 {
		return LiveZoneOutcome{Kind: OutcomeNoCompression, Reason: "no_live_user_message"}
	}

	var blocks []BlockOutcome
	totalOrigTokens := 0
	totalCompTokens := 0
	anyCompressed := false

	if _, hasSys := parsed["system"]; hasSys {
		reason := ExclusionReasonSystemMessage
		blocks = append(blocks, BlockOutcome{Index: -1, Action: BlockActionExcluded, ExclusionReason: &reason})
	}
	if tools, hasTools := parsed["tools"]; hasTools {
		if toolsArr, ok := tools.([]interface{}); ok && len(toolsArr) > 0 {
			reason := ExclusionReasonToolDefinition
			blocks = append(blocks, BlockOutcome{Index: -2, Action: BlockActionExcluded, ExclusionReason: &reason})
		}
	}

	for i, msgRaw := range messages {
		msgMap, ok := msgRaw.(map[string]interface{})
		if !ok {
			continue
		}
		role, _ := msgMap["role"].(string)
		content := msgMap["content"]

		if hasCacheControl(content) {
			reason := ExclusionReasonCacheControl
			blocks = append(blocks, BlockOutcome{Index: i, Action: BlockActionExcluded, ExclusionReason: &reason})
			continue
		}
		if i < frozenCount || i < latestUserIdx {
			reason := ExclusionReasonFrozenByPolicy
			blocks = append(blocks, BlockOutcome{Index: i, Action: BlockActionExcluded, ExclusionReason: &reason})
			continue
		}
		if role == "assistant" && i > latestUserIdx {
			reason := ExclusionReasonLatestAssistant
			blocks = append(blocks, BlockOutcome{Index: i, Action: BlockActionExcluded, ExclusionReason: &reason})
			continue
		}

		if i == latestUserIdx {
			slots := extractAnthropicTextSlots(content)
			if len(slots) == 0 {
				reason := ExclusionReasonTooSmall
				blocks = append(blocks, BlockOutcome{Index: i, Action: BlockActionExcluded, ExclusionReason: &reason})
				continue
			}
			for _, slot := range slots {
				// Use CCR-aware compression
				compressed, origToks, compToks, strategy, didCompress := compressTextWithCCR(slot.text, model, store)
				totalOrigTokens += origToks
				if didCompress {
					totalCompTokens += compToks
					anyCompressed = true
					replaceAnthropicContent(messages, i, slot, compressed)
					blocks = append(blocks, BlockOutcome{
						Index: i, Action: BlockActionCompressed,
						Strategy: strategy, OriginalTokens: origToks, CompressedTokens: compToks,
					})
				} else {
					totalCompTokens += origToks
					blocks = append(blocks, BlockOutcome{Index: i, Action: BlockActionPassthrough, OriginalTokens: origToks})
				}
			}
		}
	}

	if !anyCompressed {
		return LiveZoneOutcome{Kind: OutcomeNoCompression, Reason: "nothing_compressible"}
	}

	newBody, err := json.Marshal(parsed)
	if err != nil {
		return LiveZoneOutcome{Kind: OutcomeError, Err: fmt.Errorf("%w: %v", ErrCompressionFailed, err)}
	}

	return LiveZoneOutcome{
		Kind: OutcomeCompressed,
		Body: newBody,
		Manifest: &CompressionManifest{
			Blocks: blocks, TotalOriginalTokens: totalOrigTokens, TotalCompressedTokens: totalCompTokens,
			Model: model, AuthMode: am,
		},
	}
}
