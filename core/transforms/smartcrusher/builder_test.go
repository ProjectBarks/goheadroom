package smartcrusher

import (
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
)

// ---------- Builder tests ----------

type markerConstraint struct {
	name string
}

func (m markerConstraint) Name() string { return m.name }
func (m markerConstraint) MustKeep(_ []byte, _ []string) []int {
	return nil
}

// Override the interface method properly.
func (m markerConstraint) MustKeepRaw(_ []interface{}, _ []string) []int {
	return nil
}

type markerObserver struct {
	count *int64
}

func (m markerObserver) Name() string { return "marker" }
func (m markerObserver) OnEvent(_ *CrushEvent) {
	atomic.AddInt64(m.count, 1)
}

func TestEmptyBuilderBuildsWithDefaults(t *testing.T) {
	crusher := NewSmartCrusherBuilder(DefaultSmartCrusherConfig()).Build()
	assert.Empty(t, crusher.Constraints)
	assert.Empty(t, crusher.Observers)
}

func TestAddDefaultOSSConstraintsAppendsTwo(t *testing.T) {
	crusher := NewSmartCrusherBuilder(DefaultSmartCrusherConfig()).
		AddDefaultOSSConstraints().
		Build()
	assert.Len(t, crusher.Constraints, 2)
	names := make([]string, len(crusher.Constraints))
	for i, c := range crusher.Constraints {
		names[i] = c.Name()
	}
	assert.Equal(t, []string{"keep_errors", "keep_structural_outliers"}, names)
}

func TestAddConstraintPreservesOrder(t *testing.T) {
	crusher := NewSmartCrusherBuilder(DefaultSmartCrusherConfig()).
		AddConstraint(KeepErrorsConstraint{}).
		AddConstraint(KeepStructuralOutliersConstraint{}).
		Build()
	names := make([]string, len(crusher.Constraints))
	for i, c := range crusher.Constraints {
		names[i] = c.Name()
	}
	assert.Equal(t, []string{"keep_errors", "keep_structural_outliers"}, names)
}

func TestWithDefaultOSSSetupYieldsTwoConstraintsOneObserver(t *testing.T) {
	crusher := NewSmartCrusherBuilder(DefaultSmartCrusherConfig()).
		WithDefaultOSSSetup().
		Build()
	assert.Len(t, crusher.Constraints, 2)
	assert.Len(t, crusher.Observers, 1)
}

func TestBuilderObserverFiresOnCrush(t *testing.T) {
	var count int64
	crusher := NewSmartCrusherBuilder(DefaultSmartCrusherConfig()).
		AddObserver(markerObserver{count: &count}).
		Build()
	_ = crusher.Crush(`[1, 2, 3]`, "", 1.0)
	assert.Equal(t, int64(1), atomic.LoadInt64(&count))
}
