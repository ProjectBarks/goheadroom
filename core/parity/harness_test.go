package parity

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// stubComparator returns a fixed result for testing.
type stubComparator struct {
	name   string
	result interface{}
	err    error
}

func (s *stubComparator) Name() string { return s.name }
func (s *stubComparator) Run(input, config json.RawMessage) (interface{}, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.result, nil
}

func writeFixture(t *testing.T, dir, name string, input, output interface{}) {
	t.Helper()
	f := map[string]interface{}{
		"transform": "test",
		"input":     input,
		"config":    map[string]interface{}{},
		"output":    output,
	}
	data, err := json.Marshal(f)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), data, 0644))
}

func TestRunComparator_ExactMatch(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "match.json", map[string]interface{}{"x": 1}, map[string]interface{}{"y": 2})

	c := &stubComparator{name: "test", result: map[string]interface{}{"y": float64(2)}}
	report, err := RunComparator(dir, c)
	require.NoError(t, err)
	require.Equal(t, 1, report.Matched)
	require.Empty(t, report.Diffs)
	require.True(t, report.IsClean())
	require.Equal(t, 1, report.Total())
}

func TestRunComparator_Mismatch(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "mismatch.json", map[string]interface{}{"x": 1}, map[string]interface{}{"y": 2})

	c := &stubComparator{name: "test", result: map[string]interface{}{"y": float64(999)}}
	report, err := RunComparator(dir, c)
	require.NoError(t, err)
	require.Equal(t, 0, report.Matched)
	require.Len(t, report.Diffs, 1)
	require.Equal(t, "mismatch.json", report.Diffs[0].Fixture)
	require.False(t, report.IsClean())
}

func TestRunComparator_ApproxFloat(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "approx.json", map[string]interface{}{}, map[string]interface{}{"score": 0.95})

	c := &stubComparator{name: "test", result: map[string]interface{}{"score": 0.9500000000000001}}
	report, err := RunComparator(dir, c)
	require.NoError(t, err)
	require.Equal(t, 1, report.Matched, "approx floats within 0.001 should match")
	require.Empty(t, report.Diffs)
}

func TestRunComparator_EmptySliceEquality(t *testing.T) {
	dir := t.TempDir()
	// Output has empty array, comparator returns empty slice (not nil).
	// After JSON round-trip both become []interface{}{}, EquateEmpty matches.
	writeFixture(t, dir, "empty.json", map[string]interface{}{}, map[string]interface{}{"items": []interface{}{}})

	c := &stubComparator{name: "test", result: map[string]interface{}{"items": []interface{}{}}}
	report, err := RunComparator(dir, c)
	require.NoError(t, err)
	require.Equal(t, 1, report.Matched, "empty slices should match with EquateEmpty")
	require.Empty(t, report.Diffs)
}

func TestRunComparator_EmptyMapEquality(t *testing.T) {
	dir := t.TempDir()
	// Both sides have empty maps - EquateEmpty should treat them as equal
	writeFixture(t, dir, "emptymap.json", map[string]interface{}{}, map[string]interface{}{"meta": map[string]interface{}{}})

	c := &stubComparator{name: "test", result: map[string]interface{}{"meta": map[string]interface{}{}}}
	report, err := RunComparator(dir, c)
	require.NoError(t, err)
	require.Equal(t, 1, report.Matched, "empty maps should match with EquateEmpty")
	require.Empty(t, report.Diffs)
}

func TestRunComparator_SkipsOnError(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "err.json", map[string]interface{}{}, map[string]interface{}{})

	c := &stubComparator{name: "test", err: fmt.Errorf("unsupported")}
	report, err := RunComparator(dir, c)
	require.NoError(t, err)
	require.Len(t, report.Skipped, 1)
	require.Equal(t, "err.json", report.Skipped[0].Fixture)
	require.Equal(t, "unsupported", report.Skipped[0].Reason)
}

func TestRunComparator_Preprocessor(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "pp.json", map[string]interface{}{}, map[string]interface{}{"val": 100})

	// Preprocessor replaces "100" with "200" in expected output
	pp := func(b []byte) []byte {
		return []byte(`{"val":200}`)
	}
	c := &stubComparator{name: "test", result: map[string]interface{}{"val": float64(200)}}
	report, err := RunComparator(dir, c, pp)
	require.NoError(t, err)
	require.Equal(t, 1, report.Matched)
}

func TestRunComparator_NonexistentDir(t *testing.T) {
	c := &stubComparator{name: "test"}
	_, err := RunComparator("/nonexistent/dir", c)
	require.Error(t, err)
}
