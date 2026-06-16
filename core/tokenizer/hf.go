//go:build hf_tokenizer

package tokenizer

import (
	"fmt"
	"os"

	hftokenizers "github.com/daulet/tokenizers"
)

// HfTokenizer wraps a HuggingFace tokenizer via Rust FFI.
type HfTokenizer struct {
	name  string
	inner *hftokenizers.Tokenizer
}

// NewHfTokenizer creates a tokenizer from raw JSON bytes.
func NewHfTokenizer(name string, jsonBytes []byte) (*HfTokenizer, error) {
	tok, err := hftokenizers.FromBytes(jsonBytes)
	if err != nil {
		return nil, fmt.Errorf("hf tokenizer load %q: %w", name, err)
	}
	return &HfTokenizer{name: name, inner: tok}, nil
}

// NewHfTokenizerFromFile creates a tokenizer from a JSON file on disk.
func NewHfTokenizerFromFile(name, path string) (*HfTokenizer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("hf tokenizer read %q: %w", path, err)
	}
	return NewHfTokenizer(name, data)
}

func (t *HfTokenizer) CountText(text string) int {
	if text == "" {
		return 0
	}
	ids, _ := t.inner.Encode(text, false)
	return len(ids)
}

func (t *HfTokenizer) Backend() Backend { return BackendHuggingFace }
func (t *HfTokenizer) Name() string     { return t.name }

func (t *HfTokenizer) Close() {
	if t.inner != nil {
		t.inner.Close()
	}
}
