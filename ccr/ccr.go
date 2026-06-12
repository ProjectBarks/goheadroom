package ccr

import (
	"encoding/hex"
	"fmt"
	"time"

	"lukechampine.com/blake3"
)

const (
	DefaultCapacity = 1000
	DefaultTTL      = 5 * time.Minute
)

// CcrStore is the interface for a cache of computed responses.
type CcrStore interface {
	Put(key string, value []byte)
	Get(key string) ([]byte, bool)
	Len() int
}

// ComputeKey returns the first 24 hex characters of the BLAKE3 hash of payload.
func ComputeKey(payload []byte) string {
	h := blake3.Sum256(payload)
	return hex.EncodeToString(h[:])[:24]
}

// MarkerFor returns the CCR marker string for a given hash.
func MarkerFor(hash string) string {
	return fmt.Sprintf("<<ccr:%s>>", hash)
}
