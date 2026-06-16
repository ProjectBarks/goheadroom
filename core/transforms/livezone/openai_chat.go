package livezone

import (
	"encoding/json"
	"fmt"

	"github.com/projectbarks/goheadroom/core/authmode"
	"github.com/projectbarks/goheadroom/core/compressionpolicy"
)

// CompressOpenAIChatLiveZone compresses the live zone of an OpenAI Chat Completions API request.
// The live zone for OpenAI Chat is the latest tool message(s) and the latest user message.
func CompressOpenAIChatLiveZone(body []byte, mode compressionpolicy.Mode, am authmode.AuthMode, requestID string) LiveZoneOutcome {
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

	// Find latest tool and user message indices
	latestToolIdx := -1
	latestUserIdx := -1
	for i := len(messages) - 1; i >= 0; i-- {
		msgMap, ok := messages[i].(map[string]interface{})
		if !ok {
			continue
		}
		role, _ := msgMap["role"].(string)
		if role == "tool" && latestToolIdx < 0 {
			latestToolIdx = i
		}
		if role == "user" && latestUserIdx < 0 {
			latestUserIdx = i
		}
		if latestToolIdx >= 0 && latestUserIdx >= 0 {
			break
		}
	}

	if latestToolIdx < 0 && latestUserIdx < 0 {
		return LiveZoneOutcome{Kind: OutcomeNoCompression, Reason: "no_live_zone_candidates"}
	}

	var blocks []BlockOutcome
	totalOrigTokens := 0
	totalCompTokens := 0
	anyCompressed := false

	for i, msgRaw := range messages {
		msgMap, ok := msgRaw.(map[string]interface{})
		if !ok {
			continue
		}
		role, _ := msgMap["role"].(string)

		// System messages are excluded
		if role == "system" || role == "developer" {
			reason := ExclusionReasonSystemMessage
			blocks = append(blocks, BlockOutcome{Index: i, Action: BlockActionExcluded, ExclusionReason: &reason})
			continue
		}

		// Frozen historical messages (before the latest live zone targets)
		liveFloor := latestUserIdx
		if latestToolIdx >= 0 && (liveFloor < 0 || latestToolIdx < liveFloor) {
			liveFloor = latestToolIdx
		}
		if i < liveFloor && role != "tool" {
			reason := ExclusionReasonFrozenByPolicy
			blocks = append(blocks, BlockOutcome{Index: i, Action: BlockActionExcluded, ExclusionReason: &reason})
			continue
		}
		if role == "tool" && i != latestToolIdx && i < liveFloor {
			reason := ExclusionReasonFrozenByPolicy
			blocks = append(blocks, BlockOutcome{Index: i, Action: BlockActionExcluded, ExclusionReason: &reason})
			continue
		}

		// Assistant messages are not compressed
		if role == "assistant" {
			if i > latestUserIdx || i > latestToolIdx {
				reason := ExclusionReasonLatestAssistant
				blocks = append(blocks, BlockOutcome{Index: i, Action: BlockActionExcluded, ExclusionReason: &reason})
			} else {
				reason := ExclusionReasonFrozenByPolicy
				blocks = append(blocks, BlockOutcome{Index: i, Action: BlockActionExcluded, ExclusionReason: &reason})
			}
			continue
		}

		// Latest tool message - compress its content
		if role == "tool" && i == latestToolIdx {
			content, _ := msgMap["content"].(string)
			if len(content) < minCompressibleBytes {
				continue
			}
			compressed, origToks, compToks, strategy, didCompress := CompressText(content, model)
			totalOrigTokens += origToks
			if didCompress {
				totalCompTokens += compToks
				anyCompressed = true
				msgMap["content"] = compressed
				blocks = append(blocks, BlockOutcome{
					Index: i, Action: BlockActionCompressed,
					Strategy: strategy, OriginalTokens: origToks, CompressedTokens: compToks,
				})
			} else {
				totalCompTokens += origToks
				blocks = append(blocks, BlockOutcome{Index: i, Action: BlockActionPassthrough, OriginalTokens: origToks})
			}
			continue
		}

		// Also check for adjacent tool messages (multiple tool results from parallel calls)
		if role == "tool" && i > liveFloor {
			content, _ := msgMap["content"].(string)
			if len(content) < minCompressibleBytes {
				continue
			}
			compressed, origToks, compToks, strategy, didCompress := CompressText(content, model)
			totalOrigTokens += origToks
			if didCompress {
				totalCompTokens += compToks
				anyCompressed = true
				msgMap["content"] = compressed
				blocks = append(blocks, BlockOutcome{
					Index: i, Action: BlockActionCompressed,
					Strategy: strategy, OriginalTokens: origToks, CompressedTokens: compToks,
				})
			} else {
				totalCompTokens += origToks
				blocks = append(blocks, BlockOutcome{Index: i, Action: BlockActionPassthrough, OriginalTokens: origToks})
			}
			continue
		}

		// Latest user message - compress its text content
		if role == "user" && i == latestUserIdx {
			content := msgMap["content"]
			switch c := content.(type) {
			case string:
				if len(c) < minCompressibleBytes {
					continue
				}
				compressed, origToks, compToks, strategy, didCompress := CompressText(c, model)
				totalOrigTokens += origToks
				if didCompress {
					totalCompTokens += compToks
					anyCompressed = true
					msgMap["content"] = compressed
					blocks = append(blocks, BlockOutcome{
						Index: i, Action: BlockActionCompressed,
						Strategy: strategy, OriginalTokens: origToks, CompressedTokens: compToks,
					})
				} else {
					totalCompTokens += origToks
					blocks = append(blocks, BlockOutcome{Index: i, Action: BlockActionPassthrough, OriginalTokens: origToks})
				}
			case []interface{}:
				for j, part := range c {
					partMap, ok := part.(map[string]interface{})
					if !ok {
						continue
					}
					partType, _ := partMap["type"].(string)
					if partType != "text" {
						continue
					}
					text, _ := partMap["text"].(string)
					if len(text) < minCompressibleBytes {
						continue
					}
					compressed, origToks, compToks, strategy, didCompress := CompressText(text, model)
					totalOrigTokens += origToks
					if didCompress {
						totalCompTokens += compToks
						anyCompressed = true
						partMap["text"] = compressed
						_ = j // suppress unused
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
