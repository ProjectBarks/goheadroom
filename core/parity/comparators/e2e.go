package comparators

import (
	"encoding/json"

	"github.com/projectbarks/goheadroom/core/parity"
	"github.com/projectbarks/goheadroom/core/transforms/contentdetector"
	"github.com/projectbarks/goheadroom/core/transforms/livezone"
)

// E2EUnmutated runs livezone.CompressText and returns only whether it
// was mutated plus the detected content type.
type E2EUnmutated struct{}

func (E2EUnmutated) Name() string { return "e2e_unmutated" }

func (E2EUnmutated) Run(input, config json.RawMessage) (interface{}, error) {
	var text string
	if err := json.Unmarshal(input, &text); err != nil {
		return nil, err
	}

	_, _, _, _, ok := livezone.CompressText(text, "gpt-4o")
	ct := contentdetector.DetectContentType(text)

	out := map[string]interface{}{
		"mutated":      ok,
		"content_type": ct.ContentType.String(),
	}
	return out, nil
}

var _ parity.Comparator = E2EUnmutated{}

// E2EMutated runs livezone.CompressText and returns full mutation details.
type E2EMutated struct{}

func (E2EMutated) Name() string { return "e2e_mutated" }

func (E2EMutated) Run(input, config json.RawMessage) (interface{}, error) {
	var text string
	if err := json.Unmarshal(input, &text); err != nil {
		return nil, err
	}

	compressed, _, _, strategy, ok := livezone.CompressText(text, "gpt-4o")
	ct := contentdetector.DetectContentType(text)

	out := map[string]interface{}{
		"mutated":      ok,
		"strategy":     strategy,
		"compressed":   compressed,
		"content_type": ct.ContentType.String(),
	}
	return out, nil
}

var _ parity.Comparator = E2EMutated{}
