package parity

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
)

type Fixture struct {
	Name       string          // filename, e.g. "case1.json"
	Transform  string          `json:"transform"`
	Input      json.RawMessage `json:"input"`
	Config     json.RawMessage `json:"config"`
	Output     json.RawMessage `json:"output"`
	RecordedAt string          `json:"recorded_at"`
}

func LoadFixtures(dir string) ([]Fixture, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var fixtures []Fixture
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		var f Fixture
		if err := json.Unmarshal(data, &f); err != nil {
			return nil, err
		}
		f.Name = e.Name()
		fixtures = append(fixtures, f)
	}
	sort.Slice(fixtures, func(i, j int) bool {
		return fixtures[i].Name < fixtures[j].Name
	})
	return fixtures, nil
}
