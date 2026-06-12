package transforms

import (
	"os"

	"github.com/BurntSushi/toml"

	"github.com/uber/goheadroom/transforms/contentdetector"
)

// Recommendation represents a single recommendation entry.
type Recommendation struct {
	ID          string `toml:"id"`
	ContentType string `toml:"content_type"`
	Title       string `toml:"title"`
	Description string `toml:"description"`
	Suggestion  string `toml:"suggestion"`
	MinSize     int    `toml:"min_size,omitempty"`
}

// RecommendationStore holds loaded recommendations.
type RecommendationStore struct {
	Recommendations []Recommendation `toml:"recommendations"`
}

// contentTypeFromString maps a string content type name to a ContentType value.
func contentTypeFromString(s string) (contentdetector.ContentType, bool) {
	switch s {
	case "JsonArray":
		return contentdetector.JsonArray, true
	case "SourceCode":
		return contentdetector.SourceCode, true
	case "SearchResults":
		return contentdetector.SearchResults, true
	case "BuildOutput":
		return contentdetector.BuildOutput, true
	case "GitDiff":
		return contentdetector.GitDiff, true
	case "Html":
		return contentdetector.Html, true
	case "PlainText":
		return contentdetector.PlainText, true
	default:
		return contentdetector.PlainText, false
	}
}

// LoadRecommendations loads recommendations from a TOML file.
func LoadRecommendations(path string) (*RecommendationStore, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var store RecommendationStore
	if err := toml.Unmarshal(data, &store); err != nil {
		return nil, err
	}

	return &store, nil
}

// GetByContentType returns recommendations matching the given content type.
func (s *RecommendationStore) GetByContentType(ct contentdetector.ContentType) []Recommendation {
	var result []Recommendation
	for _, rec := range s.Recommendations {
		if rct, ok := contentTypeFromString(rec.ContentType); ok && rct == ct {
			result = append(result, rec)
		}
	}
	return result
}

// GetByID returns a recommendation by its ID.
func (s *RecommendationStore) GetByID(id string) (Recommendation, bool) {
	for _, rec := range s.Recommendations {
		if rec.ID == id {
			return rec, true
		}
	}
	return Recommendation{}, false
}
