package smartcrusher

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCompressionStrategyStringsMatchPython(t *testing.T) {
	// Port: compression_strategy_strings_match_python
	assert.Equal(t, "none", StrategyNone.String())
	assert.Equal(t, "skip", StrategySkip.String())
	assert.Equal(t, "time_series", StrategyTimeSeries.String())
	assert.Equal(t, "cluster", StrategyClusterSample.String())
	assert.Equal(t, "top_n", StrategyTopN.String())
	assert.Equal(t, "smart_sample", StrategySmartSample.String())
}

func TestCrushabilitySkipHelper(t *testing.T) {
	// Port: crushability_skip_helper
	r := CrushabilitySkip("too small", 1.0)
	assert.False(t, r.Crushable)
	assert.Equal(t, 1.0, r.Confidence)
	assert.Equal(t, "too small", r.Reason)
}

func TestCompressionPlanDefaultKeepCount(t *testing.T) {
	// Port: compression_plan_default_keep_count_matches_python
	p := CompressionPlan{}
	assert.Equal(t, StrategyNone, p.Strategy)
	assert.Empty(t, p.KeepIndices)
	// Default keep_count in Rust is 10 via Default impl.
	// In Go we use NewCompressionPlan() for defaults.
	p2 := NewCompressionPlan()
	assert.Equal(t, 10, p2.KeepCount)
	assert.Equal(t, StrategyNone, p2.Strategy)
	assert.Empty(t, p2.KeepIndices)
}

func TestCrushResultPassthrough(t *testing.T) {
	// Port: crush_result_passthrough
	r := CrushResultPassthrough("hello")
	assert.Equal(t, "hello", r.Compressed)
	assert.Equal(t, "hello", r.Original)
	assert.False(t, r.WasModified)
	assert.Equal(t, "passthrough", r.Strategy)
}
