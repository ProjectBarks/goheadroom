package transforms

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/projectbarks/goheadroom/core/transforms/contentdetector"
)

func TestRecommendationStoreLoadFromToml(t *testing.T) {
	store, err := LoadRecommendations("testdata/recommendations.toml")
	require.NoError(t, err)
	assert.True(t, len(store.Recommendations) >= 3)
}

func TestRecommendationStoreLoadInvalidPath(t *testing.T) {
	_, err := LoadRecommendations("/nonexistent/path.toml")
	assert.Error(t, err)
}

func TestRecommendationStoreGetByContentType(t *testing.T) {
	store, err := LoadRecommendations("testdata/recommendations.toml")
	require.NoError(t, err)
	recs := store.GetByContentType(contentdetector.JsonArray)
	assert.True(t, len(recs) >= 1)
	assert.Equal(t, "json_array", recs[0].ID)
}

func TestRecommendationStoreGetByContentTypeNoMatch(t *testing.T) {
	store, err := LoadRecommendations("testdata/recommendations.toml")
	require.NoError(t, err)
	recs := store.GetByContentType(contentdetector.PlainText)
	assert.Equal(t, 0, len(recs))
}

func TestRecommendationFields(t *testing.T) {
	store, err := LoadRecommendations("testdata/recommendations.toml")
	require.NoError(t, err)
	rec := store.Recommendations[0]
	assert.NotEmpty(t, rec.ID)
	assert.NotEmpty(t, rec.Title)
	assert.NotEmpty(t, rec.Description)
	assert.NotEmpty(t, rec.Suggestion)
}

func TestRecommendationStoreGetByID(t *testing.T) {
	store, err := LoadRecommendations("testdata/recommendations.toml")
	require.NoError(t, err)
	rec, found := store.GetByID("json_array")
	assert.True(t, found)
	assert.Equal(t, "JSON Array Detected", rec.Title)
}
