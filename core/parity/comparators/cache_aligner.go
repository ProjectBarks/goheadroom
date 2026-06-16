package comparators

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/projectbarks/goheadroom/core/parity"
	"github.com/projectbarks/goheadroom/core/tokenizer"
)

type CacheAligner struct {
	prevHash *string // tracks previous stable_prefix_hash across sequential calls
}

func (CacheAligner) Name() string { return "cache_aligner" }

// Sequential signals to the harness that fixtures must be sorted by
// recorded_at so that stateful fields (previous_hash) are computed correctly.
func (CacheAligner) Sequential() bool { return true }

type cacheAlignerConfig struct {
	DatePatterns        []string `json:"date_patterns"`
	DynamicTailSeparator string  `json:"dynamic_tail_separator"`
	UseDynamicDetector  bool     `json:"use_dynamic_detector"`
}

func (c *CacheAligner) Run(input, config json.RawMessage) (interface{}, error) {
	var messages []map[string]interface{}
	if err := json.Unmarshal(input, &messages); err != nil {
		return nil, err
	}

	var cfg cacheAlignerConfig
	if err := json.Unmarshal(config, &cfg); err != nil {
		return nil, err
	}

	// Compile date patterns from config. Process longer/more-specific
	// patterns first so they match before shorter ones.
	var patterns []*regexp.Regexp
	for _, p := range cfg.DatePatterns {
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, err
		}
		patterns = append(patterns, re)
	}
	// When the dynamic detector is enabled, add a bare ISO date fallback
	// that catches dates not covered by the explicit patterns above.
	if cfg.UseDynamicDetector {
		patterns = append(patterns, regexp.MustCompile(`\d{4}-\d{2}-\d{2}`))
	}

	sep := cfg.DynamicTailSeparator
	if sep == "" {
		sep = "\n\n---\n[Dynamic Context]\n"
	}

	tok := tokenizer.GetTokenizer("gpt-4o")

	// Count tokens before transform (ChatML overhead: 4 per message + 3 for assistant priming).
	tokensBefore := 3
	for _, msg := range messages {
		tokensBefore += 4
		if role, ok := msg["role"].(string); ok {
			tokensBefore += tok.CountText(role)
		}
		if content, ok := msg["content"].(string); ok {
			tokensBefore += tok.CountText(content)
		}
	}

	// Build output messages: strip dynamic content from system messages.
	outMessages := make([]map[string]interface{}, len(messages))
	var stableParts []string

	for i, msg := range messages {
		outMsg := make(map[string]interface{})
		for k, v := range msg {
			outMsg[k] = v
		}

		role, _ := msg["role"].(string)
		content, _ := msg["content"].(string)

		if role == "system" {
			// Extract dynamic matches and strip them from content.
			var dynamicValues []string
			stripped := content
			for _, re := range patterns {
				matches := re.FindAllString(stripped, -1)
				for _, m := range matches {
					dynamicValues = append(dynamicValues, m)
				}
				stripped = re.ReplaceAllString(stripped, "")
			}

			stableParts = append(stableParts, stripped)

			// Reconstruct content with dynamic tail if there are dynamic values.
			if len(dynamicValues) > 0 {
				outMsg["content"] = stripped + sep + strings.Join(dynamicValues, "\n")
			}
		}

		outMessages[i] = outMsg
	}

	// Compute stable prefix hash from stripped system content.
	stablePrefix := strings.Join(stableParts, "\n---\n")
	stablePrefixHash := sha256Hex16(stablePrefix)
	stablePrefixBytes := len(stablePrefix)
	stablePrefixTokensEst := tok.CountText(stablePrefix)

	// Compute bench_hash from original system content.
	var origParts []string
	for _, msg := range messages {
		if role, _ := msg["role"].(string); role == "system" {
			if content, ok := msg["content"].(string); ok {
				origParts = append(origParts, content)
			}
		}
	}
	benchHash := sha256Hex16(strings.Join(origParts, "\n---\n"))

	// Count tokens after transform.
	tokensAfter := 3
	for _, msg := range outMessages {
		tokensAfter += 4
		if role, ok := msg["role"].(string); ok {
			tokensAfter += tok.CountText(role)
		}
		if content, ok := msg["content"].(string); ok {
			tokensAfter += tok.CountText(content)
		}
	}

	// Determine prefix_changed and previous_hash from state.
	prefixChanged := true
	var previousHash interface{} // typed as interface{} so JSON null works
	if c.prevHash == nil {
		prefixChanged = false
		previousHash = nil
	} else {
		previousHash = *c.prevHash
		prefixChanged = *c.prevHash != stablePrefixHash
	}

	// Update state for next call.
	h := stablePrefixHash
	c.prevHash = &h

	out := map[string]interface{}{
		"bench_hash": benchHash,
		"cache_metrics": map[string]interface{}{
			"prefix_changed":           prefixChanged,
			"previous_hash":            previousHash,
			"stable_prefix_bytes":      stablePrefixBytes,
			"stable_prefix_hash":       stablePrefixHash,
			"stable_prefix_tokens_est": stablePrefixTokensEst,
		},
		"diff_artifact":     nil,
		"markers_inserted":  []string{"stable_prefix_hash:" + stablePrefixHash},
		"messages":          outMessages,
		"timing":            map[string]interface{}{},
		"tokens_after":      tokensAfter,
		"tokens_before":     tokensBefore,
		"transforms_applied": []string{"cache_align"},
		"warnings":          []interface{}{},
		"waste_signals":     nil,
	}

	return out, nil
}

func sha256Hex16(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])[:16]
}

var _ parity.Comparator = &CacheAligner{}
