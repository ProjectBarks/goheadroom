//go:build hf_tokenizer

package tokenizer

import "testing"

const tinyTokenizerJSON = `{
	"version": "1.0",
	"truncation": null,
	"padding": null,
	"added_tokens": [],
	"normalizer": null,
	"pre_tokenizer": {"type": "Whitespace"},
	"post_processor": null,
	"decoder": null,
	"model": {
		"type": "WordLevel",
		"vocab": {"hello": 0, "world": 1, "[UNK]": 2},
		"unk_token": "[UNK]"
	}
}`

func TestHfFromBytes(t *testing.T) {
	tok, err := NewHfTokenizer("tiny-test", []byte(tinyTokenizerJSON))
	if err != nil {
		t.Fatal(err)
	}
	if got := tok.CountText("hello world"); got != 2 {
		t.Errorf("CountText(\"hello world\") = %d, want 2", got)
	}
}

func TestHfEmptyIsZero(t *testing.T) {
	tok, _ := NewHfTokenizer("tiny", []byte(tinyTokenizerJSON))
	if got := tok.CountText(""); got != 0 {
		t.Errorf("CountText(\"\") = %d, want 0", got)
	}
}

func TestHfBackend(t *testing.T) {
	tok, _ := NewHfTokenizer("tiny", []byte(tinyTokenizerJSON))
	if tok.Backend() != BackendHuggingFace {
		t.Errorf("Backend = %v, want HuggingFace", tok.Backend())
	}
}

func TestHfInvalidBytes(t *testing.T) {
	_, err := NewHfTokenizer("bad", []byte("not valid json"))
	if err == nil {
		t.Error("expected error for invalid tokenizer bytes")
	}
}

func TestHfName(t *testing.T) {
	tok, _ := NewHfTokenizer("my-model", []byte(tinyTokenizerJSON))
	if tok.Name() != "my-model" {
		t.Errorf("Name = %q, want my-model", tok.Name())
	}
}
