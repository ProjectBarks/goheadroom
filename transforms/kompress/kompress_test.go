package kompress

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAvailable(t *testing.T) {
	_ = Available()
}

func TestTokenScoresCompressed(t *testing.T) {
	ts := &TokenScores{
		Words:    []string{"The", "quick", "brown", "fox"},
		Scores:   []float32{0.2, 0.9, 0.1, 0.8},
		KeepMask: []bool{false, true, false, true},
	}
	assert.Equal(t, "quick fox", ts.Compressed())
}

func TestTokenScoresAllKept(t *testing.T) {
	ts := &TokenScores{
		Words:    []string{"hello", "world"},
		Scores:   []float32{0.9, 0.8},
		KeepMask: []bool{true, true},
	}
	assert.Equal(t, "hello world", ts.Compressed())
}
