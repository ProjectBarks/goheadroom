package comparators

import (
	"encoding/json"

	"github.com/projectbarks/goheadroom/core/parity"
	"github.com/projectbarks/goheadroom/core/tokenizer"
)

type Tokenizer struct{}

func (Tokenizer) Name() string { return "tokenizer" }

func (Tokenizer) Run(input, config json.RawMessage) (interface{}, error) {
	var text string
	if err := json.Unmarshal(input, &text); err != nil {
		return nil, err
	}

	tok := tokenizer.GetTokenizer("gpt-4o")
	return tok.CountText(text), nil
}

var _ parity.Comparator = Tokenizer{}
