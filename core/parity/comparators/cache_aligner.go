package comparators

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/projectbarks/goheadroom/core/parity"
)

type CacheAligner struct{}

func (CacheAligner) Name() string { return "cache_aligner" }

func (CacheAligner) Run(input, config json.RawMessage) (interface{}, error) {
	var messages []map[string]interface{}
	if err := json.Unmarshal(input, &messages); err != nil {
		return nil, err
	}

	var parts []string
	for _, msg := range messages {
		role, _ := msg["role"].(string)
		if role != "system" {
			continue
		}
		if content, ok := msg["content"].(string); ok {
			parts = append(parts, content)
		}
	}

	joined := strings.Join(parts, "\n---\n")
	h := sha256.Sum256([]byte(joined))
	benchHash := hex.EncodeToString(h[:])[:16]

	out := map[string]interface{}{
		"bench_hash": benchHash,
	}
	return out, nil
}

var _ parity.Comparator = CacheAligner{}
