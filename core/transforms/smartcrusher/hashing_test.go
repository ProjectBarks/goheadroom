package smartcrusher

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHashFieldNameMatchesPython(t *testing.T) {
	// Port: matches_python_sha256_truncated_to_8
	assert.Equal(t, "1e38d67d", HashFieldName("customer_id"))
}

func TestHashFieldNameEmptyString(t *testing.T) {
	// Port: empty_string
	assert.Equal(t, "e3b0c442", HashFieldName(""))
}

func TestHashFieldNameUnicode(t *testing.T) {
	// Port: unicode_field_name
	assert.Equal(t, "850f7dc4", HashFieldName("café"))
}

func TestHashFieldNameDeterministic(t *testing.T) {
	// Port: deterministic
	assert.Equal(t, HashFieldName("test"), HashFieldName("test"))
}

func TestHashFieldNameOutputLength(t *testing.T) {
	// Port: output_length_is_8
	assert.Len(t, HashFieldName("a"), 8)
	long := ""
	for i := 0; i < 1000; i++ {
		long += "x"
	}
	assert.Len(t, HashFieldName(long), 8)
}
