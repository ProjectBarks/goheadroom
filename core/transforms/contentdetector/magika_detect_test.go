package contentdetector

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMagikaAvailable(t *testing.T) {
	// Just confirm the function is callable and returns a bool.
	_ = MagikaAvailable()
}

func TestMapMagikaLabelCoverage(t *testing.T) {
	cases := map[string]ContentType{
		"python":     SourceCode,
		"javascript": SourceCode,
		"go":         SourceCode,
		"c":          SourceCode,
		"cpp":        SourceCode,
		"json":       JsonArray,
		"diff":       GitDiff,
		"html":       Html,
		"txt":        PlainText,
		"unknown":    PlainText,
	}
	for label, expected := range cases {
		assert.Equal(t, expected, MapMagikaLabel(label), "label: %s", label)
	}
}
