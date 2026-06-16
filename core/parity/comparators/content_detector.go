package comparators

import (
	"encoding/json"

	"github.com/projectbarks/goheadroom/core/parity"
	"github.com/projectbarks/goheadroom/core/transforms/contentdetector"
)

type ContentDetector struct{}

func (ContentDetector) Name() string { return "content_detector" }

func (ContentDetector) Run(input, config json.RawMessage) (interface{}, error) {
	var text string
	if err := json.Unmarshal(input, &text); err != nil {
		return nil, err
	}

	result := contentdetector.DetectContentType(text)

	out := map[string]interface{}{
		"content_type": result.ContentType.String(),
		"confidence":   result.Confidence,
		"metadata":     result.Metadata,
	}
	return out, nil
}

var _ parity.Comparator = ContentDetector{}
