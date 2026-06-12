package smartcrusher

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSmartCrusherConfigDefaultsMatchPython(t *testing.T) {
	// Port: defaults_match_python
	c := DefaultSmartCrusherConfig()
	assert.True(t, c.Enabled)
	assert.Equal(t, 5, c.MinItemsToAnalyze)
	assert.Equal(t, 200, c.MinTokensToCrush)
	assert.Equal(t, 2.0, c.VarianceThreshold)
	assert.Equal(t, 0.1, c.UniquenessThreshold)
	assert.Equal(t, 0.8, c.SimilarityThreshold)
	assert.Equal(t, 15, c.MaxItemsAfterCrush)
	assert.True(t, c.PreserveChangePoints)
	assert.False(t, c.FactorOutConstants)
	assert.False(t, c.IncludeSummaries)
	assert.True(t, c.UseFeedbackHints)
	assert.Equal(t, 0.5, c.TOINConfidenceThreshold)
	assert.True(t, c.DedupIdenticalItems)
	assert.Equal(t, 0.3, c.FirstFraction)
	assert.Equal(t, 0.15, c.LastFraction)
	assert.Equal(t, 0.3, c.RelevanceThreshold)
	assert.Equal(t, 0.30, c.LosslessMinSavingsRatio)
	assert.True(t, c.EnableCCRMarker)
}
