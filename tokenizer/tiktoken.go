package tokenizer

import (
	"fmt"
	"strings"
	"sync"

	"github.com/pkoukk/tiktoken-go"
)

type TiktokenCounter struct {
	model        string
	encodingName string
	enc          *tiktoken.Tiktoken
}

var (
	encodingCache   = make(map[string]*tiktoken.Tiktoken)
	encodingCacheMu sync.Mutex
)

func getOrCreateEncoding(name string) (*tiktoken.Tiktoken, error) {
	encodingCacheMu.Lock()
	defer encodingCacheMu.Unlock()
	if enc, ok := encodingCache[name]; ok {
		return enc, nil
	}
	enc, err := tiktoken.GetEncoding(name)
	if err != nil {
		return nil, err
	}
	encodingCache[name] = enc
	return enc, nil
}

func NewTiktokenCounter(model string) (*TiktokenCounter, error) {
	encName, err := encodingFor(model)
	if err != nil {
		return nil, err
	}
	enc, err := getOrCreateEncoding(encName)
	if err != nil {
		return nil, fmt.Errorf("tiktoken init %s: %w", encName, err)
	}
	return &TiktokenCounter{model: model, encodingName: encName, enc: enc}, nil
}

func (t *TiktokenCounter) CountText(text string) int {
	if text == "" {
		return 0
	}
	return len(t.enc.EncodeOrdinary(text))
}

func (t *TiktokenCounter) Backend() Backend    { return BackendTiktoken }
func (t *TiktokenCounter) Model() string       { return t.model }
func (t *TiktokenCounter) EncodingName() string { return t.encodingName }

func encodingFor(model string) (string, error) {
	m := strings.ToLower(model)
	if strings.HasPrefix(m, "gpt-4o") || strings.HasPrefix(m, "o1") || strings.HasPrefix(m, "o3") {
		return "o200k_base", nil
	}
	if strings.HasPrefix(m, "gpt-4") || strings.HasPrefix(m, "gpt-3.5") || strings.HasPrefix(m, "text-embedding") {
		return "cl100k_base", nil
	}
	if strings.HasPrefix(m, "code-") || strings.HasPrefix(m, "text-davinci-002") || strings.HasPrefix(m, "text-davinci-003") {
		return "p50k_base", nil
	}
	if strings.HasPrefix(m, "text-davinci") || strings.HasPrefix(m, "davinci") || strings.HasPrefix(m, "curie") || strings.HasPrefix(m, "babbage") || strings.HasPrefix(m, "ada") {
		return "r50k_base", nil
	}
	return "", fmt.Errorf("unknown encoding for model %q", model)
}
