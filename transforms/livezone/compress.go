package livezone

import (
	"github.com/uber/goheadroom/ccr"
	"github.com/uber/goheadroom/tokenizer"
	"github.com/uber/goheadroom/transforms/contentdetector"
	"github.com/uber/goheadroom/transforms/diffcompressor"
	"github.com/uber/goheadroom/transforms/logcompressor"
	"github.com/uber/goheadroom/transforms/searchcompressor"
	"github.com/uber/goheadroom/transforms/smartcrusher"
)

// Minimum byte size for content to be considered for compression.
// Matches the Rust per-content-type thresholds (all 512).
const minCompressibleBytes = 512

// compressText runs content-type-aware compression on a text string.
// Returns the compressed text, original tokens, compressed tokens, strategy, and whether compression succeeded.
func compressText(text string, model string) (compressed string, origTokens, compTokens int, strategy string, ok bool) {
	return compressTextWithCCR(text, model, nil)
}

// compressTextWithCCR is like compressText but optionally injects CCR markers.
// When store is non-nil, a <<ccr:HASH>> marker is appended and the original is stored.
func compressTextWithCCR(text string, model string, store ccr.CcrStore) (compressed string, origTokens, compTokens int, strategy string, ok bool) {
	if len(text) < minCompressibleBytes {
		return "", 0, 0, "", false
	}

	tok := tokenizer.GetTokenizer(model)
	origTokens = tok.CountText(text)

	// Detect content type
	result := contentdetector.DetectContentType(text)

	// Dispatch to the appropriate compressor
	var compressedText string
	var didCompress bool

	switch result.ContentType {
	case contentdetector.JsonArray:
		crusher := smartcrusher.NewSmartCrusherBuilder(smartcrusher.DefaultSmartCrusherConfig()).WithDefaultOSSSetup().Build()
		cr := crusher.Crush(text, "", 0.0)
		if cr.WasModified {
			compressedText = cr.Compressed
			strategy = "smart_crusher"
			didCompress = true
		}
	case contentdetector.BuildOutput:
		lc := logcompressor.New(logcompressor.DefaultConfig())
		lr, _ := lc.Compress(text, 0.0)
		if lr.Compressed != lr.Original {
			compressedText = lr.Compressed
			strategy = "log_compressor"
			didCompress = true
		}
	case contentdetector.SearchResults:
		sc := searchcompressor.New(searchcompressor.DefaultConfig())
		sr, _ := sc.Compress(text, "", 0.0)
		if sr.Compressed != sr.Original {
			compressedText = sr.Compressed
			strategy = "search_compressor"
			didCompress = true
		}
	case contentdetector.GitDiff:
		dc := diffcompressor.New(diffcompressor.DefaultConfig())
		dr := dc.Compress(text, "")
		if dr.Compressed != text {
			compressedText = dr.Compressed
			strategy = "diff_compressor"
			didCompress = true
		}
	default:
		// PlainText, SourceCode, Html - no compressor available yet
		return "", origTokens, origTokens, "", false
	}

	if !didCompress {
		return "", origTokens, origTokens, "", false
	}

	// Tokenizer-validated rejection gate: accept only when compressed token count is strictly less
	compTokens = tok.CountText(compressedText)

	// If CCR store is provided, inject marker and re-validate token count
	if store != nil {
		hash := ccr.ComputeKey([]byte(text))
		marker := ccr.MarkerFor(hash)
		if compressedText[len(compressedText)-1] == '\n' {
			compressedText = compressedText + marker
		} else {
			compressedText = compressedText + "\n" + marker
		}
		compTokens = tok.CountText(compressedText)
		if compTokens >= origTokens {
			return "", origTokens, origTokens, "", false
		}
		// Store the original under the hash
		store.Put(hash, []byte(text))
	}

	if compTokens >= origTokens {
		return "", origTokens, origTokens, "", false
	}

	return compressedText, origTokens, compTokens, strategy, true
}

// textSlot represents a compressible text slot in a message.
type textSlot struct {
	text  string
	index int    // -1 for string content, >= 0 for array element index
	key   string // "text" for text blocks, "content" for tool_result blocks
}

// extractAnthropicTextSlots extracts compressible text slots from Anthropic message content.
func extractAnthropicTextSlots(content interface{}) []textSlot {
	switch c := content.(type) {
	case string:
		if len(c) >= minCompressibleBytes {
			return []textSlot{{text: c, index: -1, key: "content"}}
		}
		return nil
	case []interface{}:
		var slots []textSlot
		for i, block := range c {
			blockMap, ok := block.(map[string]interface{})
			if !ok {
				continue
			}
			blockType, _ := blockMap["type"].(string)
			switch blockType {
			case "tool_result":
				if text, ok := blockMap["content"].(string); ok && len(text) >= minCompressibleBytes {
					slots = append(slots, textSlot{text: text, index: i, key: "content"})
				}
			case "text":
				if text, ok := blockMap["text"].(string); ok && len(text) >= minCompressibleBytes {
					slots = append(slots, textSlot{text: text, index: i, key: "text"})
				}
			}
		}
		return slots
	}
	return nil
}

// hasCacheControl checks if a message content block has cache_control markers.
func hasCacheControl(content interface{}) bool {
	switch c := content.(type) {
	case []interface{}:
		for _, block := range c {
			blockMap, ok := block.(map[string]interface{})
			if !ok {
				continue
			}
			if _, has := blockMap["cache_control"]; has {
				return true
			}
		}
	}
	return false
}
