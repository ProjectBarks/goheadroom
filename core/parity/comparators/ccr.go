package comparators

import (
	"encoding/json"
	"fmt"

	"github.com/projectbarks/goheadroom/core/parity"
)

type CCR struct{}

func (CCR) Name() string { return "ccr" }

func (CCR) Run(input, config json.RawMessage) (interface{}, error) {
	var v interface{}
	if err := json.Unmarshal(input, &v); err != nil {
		return nil, err
	}

	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}

	return fmt.Sprintf("roundtrip:%d", len(b)), nil
}

var _ parity.Comparator = CCR{}
