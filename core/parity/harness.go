package parity

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

type Comparator interface {
	Name() string
	Run(input, config json.RawMessage) (interface{}, error)
}

// SequentialComparator is implemented by comparators that need fixtures
// processed in recorded_at order (e.g. because they maintain state across calls).
type SequentialComparator interface {
	Sequential() bool
}

func DefaultCmpOpts() []cmp.Option {
	return []cmp.Option{
		cmpopts.EquateApprox(0, 0.001),
		cmpopts.EquateEmpty(),
		cmpopts.SortMaps(func(a, b string) bool { return a < b }),
	}
}

type Diff struct {
	Fixture string
	CmpDiff string
}

type Report struct {
	Transform string
	Matched   int
	Diffs     []Diff
	Skipped   []SkippedFixture
}

type SkippedFixture struct {
	Fixture string
	Reason  string
}

func (r *Report) Total() int    { return r.Matched + len(r.Diffs) + len(r.Skipped) }
func (r *Report) IsClean() bool { return len(r.Diffs) == 0 }

type Preprocessor func([]byte) []byte

func RunComparator(dir string, c Comparator, preprocessors ...Preprocessor) (*Report, error) {
	fixtures, err := LoadFixtures(dir)
	if err != nil {
		return nil, err
	}
	report := &Report{Transform: c.Name()}
	opts := DefaultCmpOpts()

	// If the comparator is sequential (stateful), sort fixtures by recorded_at
	// so that state-dependent fields (e.g. previous_hash) are computed correctly.
	if sc, ok := c.(SequentialComparator); ok && sc.Sequential() {
		sort.SliceStable(fixtures, func(i, j int) bool {
			return fixtures[i].RecordedAt < fixtures[j].RecordedAt
		})
	}

	for _, fix := range fixtures {
		goResult, err := c.Run(fix.Input, fix.Config)
		if err != nil {
			report.Skipped = append(report.Skipped, SkippedFixture{Fixture: fix.Name, Reason: err.Error()})
			continue
		}
		expectedBytes := []byte(fix.Output)
		for _, pp := range preprocessors {
			expectedBytes = pp(expectedBytes)
		}
		var expected interface{}
		if err := json.Unmarshal(expectedBytes, &expected); err != nil {
			return nil, fmt.Errorf("fixture %s: unmarshal expected: %w", fix.Name, err)
		}
		actualBytes, err := json.Marshal(goResult)
		if err != nil {
			return nil, fmt.Errorf("fixture %s: marshal Go result: %w", fix.Name, err)
		}
		var actual interface{}
		if err := json.Unmarshal(actualBytes, &actual); err != nil {
			return nil, fmt.Errorf("fixture %s: unmarshal Go result: %w", fix.Name, err)
		}
		if diff := cmp.Diff(expected, actual, opts...); diff != "" {
			report.Diffs = append(report.Diffs, Diff{Fixture: fix.Name, CmpDiff: diff})
		} else {
			report.Matched++
		}
	}
	return report, nil
}
