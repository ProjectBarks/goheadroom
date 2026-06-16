package parity

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
)

type Option func(*runConfig)

type runConfig struct {
	preprocessors []Preprocessor
	cmpOpts       []cmp.Option
	minFixtures   int
}

func WithPreprocessor(fn Preprocessor) Option {
	return func(c *runConfig) { c.preprocessors = append(c.preprocessors, fn) }
}

func WithCmpOption(opt cmp.Option) Option {
	return func(c *runConfig) { c.cmpOpts = append(c.cmpOpts, opt) }
}

func WithMinFixtures(n int) Option {
	return func(c *runConfig) { c.minFixtures = n }
}

func RunFixtures(t *testing.T, dir string, c Comparator, opts ...Option) {
	t.Helper()
	cfg := runConfig{minFixtures: 1}
	for _, o := range opts {
		o(&cfg)
	}
	fixtures, err := LoadFixtures(dir)
	require.NoError(t, err, "failed to load fixtures from %s", dir)
	allOpts := append(DefaultCmpOpts(), cfg.cmpOpts...)
	count := 0
	for _, fix := range fixtures {
		count++
		t.Run(fix.Name, func(t *testing.T) {
			goResult, err := c.Run(fix.Input, fix.Config)
			require.NoError(t, err, "comparator error for %s", fix.Name)
			expectedBytes := []byte(fix.Output)
			for _, pp := range cfg.preprocessors {
				expectedBytes = pp(expectedBytes)
			}
			var expected interface{}
			require.NoError(t, json.Unmarshal(expectedBytes, &expected), "unmarshal expected for %s", fix.Name)
			actualBytes, err := json.Marshal(goResult)
			require.NoError(t, err, "marshal Go result for %s", fix.Name)
			var actual interface{}
			require.NoError(t, json.Unmarshal(actualBytes, &actual), "unmarshal Go result for %s", fix.Name)
			if diff := cmp.Diff(expected, actual, allOpts...); diff != "" {
				t.Errorf("parity mismatch for %s:\n%s", fix.Name, diff)
			}
		})
	}
	if count < cfg.minFixtures {
		t.Errorf("expected at least %d fixtures in %s, found %d", cfg.minFixtures, dir, count)
	}
}

func FormatReport(r *Report) string {
	s := fmt.Sprintf("[%s] total=%d matched=%d skipped=%d diffed=%d\n",
		r.Transform, r.Total(), r.Matched, len(r.Skipped), len(r.Diffs))
	for _, d := range r.Diffs {
		s += fmt.Sprintf("  DIFF %s:\n%s\n", d.Fixture, d.CmpDiff)
	}
	for _, sk := range r.Skipped {
		s += fmt.Sprintf("  SKIP %s: %s\n", sk.Fixture, sk.Reason)
	}
	return s
}
