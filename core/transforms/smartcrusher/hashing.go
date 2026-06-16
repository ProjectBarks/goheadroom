package smartcrusher

import (
	"crypto/sha256"
	"fmt"
)

// HashFieldName returns SHA-256 of field_name, hex-encoded, truncated to 8 chars.
// Matches Python: hashlib.sha256(field_name.encode()).hexdigest()[:8]
func HashFieldName(fieldName string) string {
	h := sha256.Sum256([]byte(fieldName))
	hex := fmt.Sprintf("%x", h)
	return hex[:8]
}
