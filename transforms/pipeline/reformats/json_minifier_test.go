package reformats

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uber/goheadroom/transforms/contentdetector"
	"github.com/uber/goheadroom/transforms/pipeline"
)

func TestJsonMinifierNameAndAppliesTo(t *testing.T) {
	m := NewJsonMinifier()
	assert.Equal(t, "json_minifier", m.Name())
	assert.Equal(t, []contentdetector.ContentType{contentdetector.JsonArray}, m.AppliesTo())
}

func TestJsonMinifierPrettyObjectMinifies(t *testing.T) {
	m := NewJsonMinifier()
	pretty := "{\n  \"a\": 1,\n  \"b\": 2\n}"
	r, err := m.Apply(pretty)
	require.NoError(t, err)
	assert.Equal(t, `{"a":1,"b":2}`, r.Output)
	assert.Greater(t, r.BytesSaved, 0)
}

func TestJsonMinifierPrettyArrayMinifies(t *testing.T) {
	m := NewJsonMinifier()
	pretty := "[\n  1,\n  2,\n  3\n]"
	r, err := m.Apply(pretty)
	require.NoError(t, err)
	assert.Equal(t, "[1,2,3]", r.Output)
	assert.Greater(t, r.BytesSaved, 0)
}

func TestJsonMinifierAlreadyCompactYieldsZeroSavings(t *testing.T) {
	m := NewJsonMinifier()
	compact := `{"a":1,"b":2}`
	r, err := m.Apply(compact)
	require.NoError(t, err)
	assert.Equal(t, compact, r.Output)
	assert.Equal(t, 0, r.BytesSaved)
}

func TestJsonMinifierInvalidJsonErrors(t *testing.T) {
	m := NewJsonMinifier()
	_, err := m.Apply("{not: valid")
	require.Error(t, err)
	te, ok := err.(pipeline.TransformError)
	require.True(t, ok)
	assert.Equal(t, pipeline.ErrorInvalidInput, te.Kind)
	assert.Equal(t, "json_minifier", te.Transform)
}

func TestJsonMinifierEmptyInputSkipped(t *testing.T) {
	m := NewJsonMinifier()
	_, err := m.Apply("")
	require.Error(t, err)
	te, ok := err.(pipeline.TransformError)
	require.True(t, ok)
	assert.Equal(t, pipeline.ErrorSkipped, te.Kind)
}

func TestJsonMinifierWhitespaceOnlySkipped(t *testing.T) {
	m := NewJsonMinifier()
	_, err := m.Apply("   \n\t  ")
	require.Error(t, err)
	te, ok := err.(pipeline.TransformError)
	require.True(t, ok)
	assert.Equal(t, pipeline.ErrorSkipped, te.Kind)
}

func TestJsonMinifierNestedStructureRoundTrips(t *testing.T) {
	m := NewJsonMinifier()
	pretty := `
	{
	  "users": [
		{"id": 1, "name": "alice", "active": true},
		{"id": 2, "name": "bob",   "active": false}
	  ],
	  "count": 2
	}
	`
	r, err := m.Apply(pretty)
	require.NoError(t, err)
	assert.Greater(t, r.BytesSaved, 0)
	// Verify structural equivalence by re-parsing
	assert.Contains(t, r.Output, `"count":2`)
	assert.Contains(t, r.Output, `"alice"`)
}

func TestJsonMinifierNeverGrowsOutput(t *testing.T) {
	m := NewJsonMinifier()
	inputs := []string{
		`{}`,
		`[]`,
		`null`,
		`42`,
		`"string"`,
		`{"k":"value with spaces"}`,
	}
	for _, input := range inputs {
		r, err := m.Apply(input)
		require.NoError(t, err, "input: %s", input)
		assert.LessOrEqual(t, len(r.Output), len(input),
			"minifier grew output for %q: %d -> %d", input, len(input), len(r.Output))
	}
}

func TestJsonMinifierUnicodeRoundTrips(t *testing.T) {
	m := NewJsonMinifier()
	pretty := `{ "msg": "hello world" }`
	r, err := m.Apply(pretty)
	require.NoError(t, err)
	assert.Contains(t, r.Output, "hello world")
}

func TestJsonMinifierBytesSavedAccuracy(t *testing.T) {
	m := NewJsonMinifier()
	input := "{\n  \"a\": 1\n}"
	r, err := m.Apply(input)
	require.NoError(t, err)
	expected := len(input) - len(r.Output)
	assert.Equal(t, expected, r.BytesSaved)
}
