package tokenizer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type tokParityFixture struct {
	Transform string `json:"transform"`
	Input     string `json:"input"`
	Output    int    `json:"output"`
}

func TestTokenizerParity(t *testing.T) {
	fixtureDir := filepath.Join("..", "testdata", "parity", "tokenizer")
	entries, err := os.ReadDir(fixtureDir)
	require.NoError(t, err)
	require.NotEmpty(t, entries)

	tok := GetTokenizer("gpt-4o")
	require.NotNil(t, tok)

	count := 0
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		count++
		t.Run(entry.Name(), func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(fixtureDir, entry.Name()))
			require.NoError(t, err)
			var fix tokParityFixture
			require.NoError(t, json.Unmarshal(data, &fix))
			assert.Equal(t, "tokenizer", fix.Transform)
			assert.Equal(t, fix.Output, tok.CountText(fix.Input),
				"token count mismatch for %s", entry.Name())
		})
	}
	assert.Equal(t, 40, count, "expected 40 fixture files")
}
