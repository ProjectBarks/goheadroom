package livezone

import (
	"encoding/json"
	"fmt"

	"github.com/uber/goheadroom/authmode"
	"github.com/uber/goheadroom/compressionpolicy"
)

// responsesOutputMinBytes is the minimum size for Responses output items before
// compression is even attempted. Matches the Rust RESPONSES_OUTPUT_MIN_BYTES constant.
const responsesOutputMinBytes = 2048

// CompressOpenAIResponsesLiveZone compresses the live zone of an OpenAI Responses API request.
// The live zone for Responses is function_call_output, local_shell_call_output,
// apply_patch_call_output items, plus the latest user message text.
func CompressOpenAIResponsesLiveZone(body []byte, mode compressionpolicy.Mode, am authmode.AuthMode, requestID string) LiveZoneOutcome {
	if mode == compressionpolicy.Off {
		return LiveZoneOutcome{Kind: OutcomeNoCompression, Reason: "mode_off"}
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return LiveZoneOutcome{Kind: OutcomeError, Err: fmt.Errorf("%w: %v", ErrInvalidBody, err)}
	}

	// Responses uses "input" as the canonical field, "messages" as alias
	var items []interface{}
	if inputRaw, ok := parsed["input"]; ok {
		items, _ = inputRaw.([]interface{})
	}
	if items == nil {
		if msgsRaw, ok := parsed["messages"]; ok {
			items, _ = msgsRaw.([]interface{})
		}
	}

	if items == nil {
		return LiveZoneOutcome{Kind: OutcomeNoCompression, Reason: "no_input"}
	}
	if len(items) == 0 {
		return LiveZoneOutcome{Kind: OutcomeNoCompression, Reason: "empty_input"}
	}

	// String input (just a prompt) - no compression
	if _, isString := parsed["input"].(string); isString {
		return LiveZoneOutcome{Kind: OutcomeNoCompression, Reason: "string_input"}
	}

	model, _ := parsed["model"].(string)
	if model == "" {
		model = DefaultModel
	}

	var blocks []BlockOutcome
	totalOrigTokens := 0
	totalCompTokens := 0
	anyCompressed := false

	// Walk items and find compressible candidates
	for i, itemRaw := range items {
		itemMap, ok := itemRaw.(map[string]interface{})
		if !ok {
			continue
		}

		typeTag, _ := itemMap["type"].(string)

		switch typeTag {
		case "function_call_output", "local_shell_call_output", "apply_patch_call_output":
			output, _ := itemMap["output"].(string)
			if len(output) < minCompressibleBytes {
				reason := ExclusionReasonTooSmall
				blocks = append(blocks, BlockOutcome{Index: i, Action: BlockActionExcluded, ExclusionReason: &reason})
				continue
			}
			compressed, origToks, compToks, strategy, didCompress := CompressText(output, model)
			totalOrigTokens += origToks
			if didCompress {
				totalCompTokens += compToks
				anyCompressed = true
				itemMap["output"] = compressed
				blocks = append(blocks, BlockOutcome{
					Index: i, Action: BlockActionCompressed,
					Strategy: strategy, OriginalTokens: origToks, CompressedTokens: compToks,
				})
			} else {
				totalCompTokens += origToks
				blocks = append(blocks, BlockOutcome{Index: i, Action: BlockActionPassthrough, OriginalTokens: origToks})
			}

		case "message":
			// Compress text content in user messages
			contentArr, ok := itemMap["content"].([]interface{})
			if !ok {
				continue
			}
			for j, partRaw := range contentArr {
				partMap, ok := partRaw.(map[string]interface{})
				if !ok {
					continue
				}
				partType, _ := partMap["type"].(string)
				if partType != "input_text" {
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
					_ = j
					blocks = append(blocks, BlockOutcome{
						Index: i, Action: BlockActionCompressed,
						Strategy: strategy, OriginalTokens: origToks, CompressedTokens: compToks,
					})
				} else {
					totalCompTokens += origToks
					blocks = append(blocks, BlockOutcome{Index: i, Action: BlockActionPassthrough, OriginalTokens: origToks})
				}
			}

		default:
			// Other item types pass through (reasoning, compaction, etc.)
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

// SummarizeOpenAIResponsesNoChangeReason returns a grep-able reason string
// explaining why a Responses dispatch made no changes.
func SummarizeOpenAIResponsesNoChangeReason(manifest *CompressionManifest) string {
	if manifest == nil {
		return "no_manifest"
	}
	if len(manifest.Blocks) == 0 {
		return "no_eligible_items"
	}
	if manifest.TotalOriginalTokens == manifest.TotalCompressedTokens {
		return "no_token_savings"
	}
	return "no_change"
}
