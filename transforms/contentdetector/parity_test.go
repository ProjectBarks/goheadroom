package contentdetector

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type cdParityFixture struct {
	Transform string         `json:"transform"`
	Input     string         `json:"input"`
	Output    cdParityOutput `json:"output"`
}

type cdParityOutput struct {
	ContentType string                 `json:"content_type"`
	Confidence  float64                `json:"confidence"`
	Metadata    map[string]interface{} `json:"metadata"`
}

func TestContentDetectorParity(t *testing.T) {
	fixtureDir := filepath.Join("..", "..", "testdata", "parity", "content_detector")
	entries, err := os.ReadDir(fixtureDir)
	require.NoError(t, err)
	require.NotEmpty(t, entries)

	count := 0
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		count++
		t.Run(entry.Name(), func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(fixtureDir, entry.Name()))
			require.NoError(t, err)
			var fix cdParityFixture
			require.NoError(t, json.Unmarshal(data, &fix))
			assert.Equal(t, "content_detector", fix.Transform)

			result := DetectContentType(fix.Input)
			assert.Equal(t, fix.Output.ContentType, result.ContentType.String(),
				"content_type mismatch for %s", entry.Name())
			assert.InDelta(t, fix.Output.Confidence, result.Confidence, 0.05,
				"confidence mismatch for %s", entry.Name())

			// Assert metadata fields match Python fixtures.
			for key, expected := range fix.Output.Metadata {
				actual, ok := result.Metadata[key]
				assert.True(t, ok, "metadata key %q missing for %s", key, entry.Name())
				if ok {
					switch ev := expected.(type) {
					case float64:
						av, aOk := actual.(int)
						if aOk {
							assert.InDelta(t, ev, float64(av), 0.5,
								"metadata %q mismatch for %s", key, entry.Name())
						} else {
							assert.InDelta(t, ev, actual, 0.5,
								"metadata %q mismatch for %s", key, entry.Name())
						}
					case bool:
						assert.Equal(t, ev, actual,
							"metadata %q mismatch for %s", key, entry.Name())
					case string:
						assert.Equal(t, ev, actual,
							"metadata %q mismatch for %s", key, entry.Name())
					}
				}
			}
		})
	}
	assert.Equal(t, 21, count, "expected 21 fixture files")
}
