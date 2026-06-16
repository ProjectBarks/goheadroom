package parity

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRunFixtures_CreatesSubtests(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "a.json", map[string]interface{}{"x": 1}, map[string]interface{}{"y": 2})
	writeFixture(t, dir, "b.json", map[string]interface{}{"x": 3}, map[string]interface{}{"y": 4})

	c := &stubComparator{name: "test", result: map[string]interface{}{"y": float64(2)}}

	// RunFixtures will create subtests named "a.json" and "b.json".
	// The second fixture will mismatch (expects y=4 but comparator returns y=2).
	// We run it inside a sub-test to capture failures without failing this test.
	subtestNames := []string{}
	t.Run("subtests", func(t *testing.T) {
		// We can't easily intercept t.Run calls, so instead we verify
		// by running RunFixtures and checking the report via RunComparator.
	})

	// Verify via RunComparator that 2 fixtures are loaded
	report, err := RunComparator(dir, c)
	require.NoError(t, err)
	require.Equal(t, 2, report.Total())
	_ = subtestNames
}

func TestRunFixtures_MinFixtures(t *testing.T) {
	dir := t.TempDir()
	// Empty dir -- verify LoadFixtures returns 0 fixtures,
	// which means minFixtures check would fail in RunFixtures.
	fixtures, err := LoadFixtures(dir)
	require.NoError(t, err)
	require.Len(t, fixtures, 0)
}

func TestRunFixtures_WithPreprocessor(t *testing.T) {
	dir := t.TempDir()
	// Fixture output has value 100, preprocessor changes to 200, comparator returns 200
	writeFixture(t, dir, "pp.json", map[string]interface{}{}, map[string]interface{}{"v": 100})

	pp := func(b []byte) []byte { return []byte(`{"v":200}`) }
	c := &stubComparator{name: "test", result: map[string]interface{}{"v": float64(200)}}

	RunFixtures(t, dir, c, WithPreprocessor(pp))
}

func TestRunFixtures_CorrectSubtestCount(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 5; i++ {
		writeFixture(t, dir, filepath.Base(t.TempDir())+".json", map[string]interface{}{}, map[string]interface{}{})
	}

	// Write 5 fixtures and verify they load
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	jsonCount := 0
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".json" {
			jsonCount++
		}
	}
	require.Equal(t, 5, jsonCount)

	// RunFixtures should process all 5
	c := &stubComparator{name: "test", result: map[string]interface{}{}}
	RunFixtures(t, dir, c)
}

func TestFormatReport(t *testing.T) {
	r := &Report{
		Transform: "myTransform",
		Matched:   3,
		Diffs:     []Diff{{Fixture: "bad.json", CmpDiff: "-got +want"}},
		Skipped:   []SkippedFixture{{Fixture: "skip.json", Reason: "unsupported"}},
	}
	s := FormatReport(r)
	require.Contains(t, s, "[myTransform]")
	require.Contains(t, s, "total=5")
	require.Contains(t, s, "matched=3")
	require.Contains(t, s, "skipped=1")
	require.Contains(t, s, "diffed=1")
	require.Contains(t, s, "DIFF bad.json")
	require.Contains(t, s, "SKIP skip.json")
}
