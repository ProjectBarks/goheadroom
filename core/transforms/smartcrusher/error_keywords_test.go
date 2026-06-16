package smartcrusher

import (
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestErrorKeywordsMatchesPythonCount(t *testing.T) {
	// Port: matches_python_count
	assert.Len(t, ErrorKeywords, 12)
}

func TestErrorKeywordsAllLowercase(t *testing.T) {
	// Port: all_lowercase_invariant
	for _, kw := range ErrorKeywords {
		assert.Equal(t, strings.ToLower(kw), kw, "ERROR_KEYWORDS must all be lowercase")
	}
}

func TestErrorKeywordsPinnedMembership(t *testing.T) {
	// Port: pinned_membership
	expected := []string{
		"error", "exception", "failed", "failure",
		"critical", "fatal", "crash", "panic",
		"abort", "timeout", "denied", "rejected",
	}
	actualSorted := make([]string, len(ErrorKeywords))
	copy(actualSorted, ErrorKeywords)
	sort.Strings(actualSorted)

	expectedSorted := make([]string, len(expected))
	copy(expectedSorted, expected)
	sort.Strings(expectedSorted)

	assert.Equal(t, expectedSorted, actualSorted)
}
