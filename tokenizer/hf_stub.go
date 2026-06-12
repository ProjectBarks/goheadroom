//go:build !cgo

package tokenizer

import "fmt"

// HfTokenizer is a stub when CGO is not available.
type HfTokenizer struct {
	name string
}

// NewHfTokenizer returns an error when CGO is disabled.
func NewHfTokenizer(name string, jsonBytes []byte) (*HfTokenizer, error) {
	return nil, fmt.Errorf("HuggingFace tokenizer requires CGO (build with CGO_ENABLED=1)")
}

// NewHfTokenizerFromFile returns an error when CGO is disabled.
func NewHfTokenizerFromFile(name, path string) (*HfTokenizer, error) {
	return nil, fmt.Errorf("HuggingFace tokenizer requires CGO (build with CGO_ENABLED=1)")
}

func (t *HfTokenizer) CountText(text string) int { return 0 }
func (t *HfTokenizer) Backend() Backend           { return BackendHuggingFace }
func (t *HfTokenizer) Name() string               { return t.name }
func (t *HfTokenizer) Close()                     {}
