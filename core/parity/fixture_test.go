package parity

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadFixtures_Valid(t *testing.T) {
	dir := t.TempDir()
	content := `{"transform":"t1","input":{"a":1},"config":{},"output":{"b":2}}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "case1.json"), []byte(content), 0644))

	fixtures, err := LoadFixtures(dir)
	require.NoError(t, err)
	require.Len(t, fixtures, 1)
	require.Equal(t, "case1.json", fixtures[0].Name)
	require.Equal(t, "t1", fixtures[0].Transform)
	require.JSONEq(t, `{"a":1}`, string(fixtures[0].Input))
	require.JSONEq(t, `{"b":2}`, string(fixtures[0].Output))
}

func TestLoadFixtures_MultipleFilesSorted(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"z_case.json", "a_case.json", "m_case.json"} {
		content := `{"transform":"t","input":{},"config":{},"output":{}}`
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0644))
	}

	fixtures, err := LoadFixtures(dir)
	require.NoError(t, err)
	require.Len(t, fixtures, 3)
	require.Equal(t, "a_case.json", fixtures[0].Name)
	require.Equal(t, "m_case.json", fixtures[1].Name)
	require.Equal(t, "z_case.json", fixtures[2].Name)
}

func TestLoadFixtures_SkipsNonJSON(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("hi"), 0644))
	require.NoError(t, os.Mkdir(filepath.Join(dir, "subdir"), 0755))

	fixtures, err := LoadFixtures(dir)
	require.NoError(t, err)
	require.Empty(t, fixtures)
}

func TestLoadFixtures_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	fixtures, err := LoadFixtures(dir)
	require.NoError(t, err)
	require.Empty(t, fixtures)
}

func TestLoadFixtures_NonexistentDir(t *testing.T) {
	_, err := LoadFixtures("/nonexistent/path/that/does/not/exist")
	require.Error(t, err)
}

func TestLoadFixtures_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bad.json"), []byte("{invalid"), 0644))

	_, err := LoadFixtures(dir)
	require.Error(t, err)
}
