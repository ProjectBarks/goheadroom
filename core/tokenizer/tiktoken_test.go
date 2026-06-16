package tokenizer

import (
	"strings"
	"testing"
)

func TestTiktokenEmptyIsZero(t *testing.T) {
	tc, err := NewTiktokenCounter("gpt-4o")
	if err != nil {
		t.Fatal(err)
	}
	if got := tc.CountText(""); got != 0 {
		t.Errorf("empty string: got %d, want 0", got)
	}
}

func TestTiktokenKnownCounts(t *testing.T) {
	tc, err := NewTiktokenCounter("gpt-4o")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		text string
		want int
	}{
		{"hello", 1},
		{"Hello, world!", 4},
		{"the quick brown fox jumps over the lazy dog", 9},
	}
	for _, tt := range tests {
		got := tc.CountText(tt.text)
		if got != tt.want {
			t.Errorf("CountText(%q) = %d, want %d", tt.text, got, tt.want)
		}
	}
}

func TestTiktokenDeterministic(t *testing.T) {
	tc, err := NewTiktokenCounter("gpt-4o")
	if err != nil {
		t.Fatal(err)
	}
	text := "deterministic tokenization test string with some content"
	first := tc.CountText(text)
	for i := 0; i < 1000; i++ {
		if got := tc.CountText(text); got != first {
			t.Fatalf("call %d: got %d, want %d", i, got, first)
		}
	}
}

func TestTiktokenEncodingDispatch(t *testing.T) {
	tests := []struct {
		model    string
		wantEnc  string
	}{
		{"gpt-4o", "o200k_base"},
		{"gpt-4o-mini", "o200k_base"},
		{"o1-preview", "o200k_base"},
		{"o3-mini", "o200k_base"},
		{"gpt-4", "cl100k_base"},
		{"gpt-4-turbo", "cl100k_base"},
		{"gpt-3.5-turbo", "cl100k_base"},
		{"text-embedding-ada-002", "cl100k_base"},
		{"code-davinci-002", "p50k_base"},
		{"text-davinci-002", "p50k_base"},
		{"davinci", "r50k_base"},
		{"curie", "r50k_base"},
		{"babbage", "r50k_base"},
		{"ada", "r50k_base"},
	}
	for _, tt := range tests {
		tc, err := NewTiktokenCounter(tt.model)
		if err != nil {
			t.Errorf("NewTiktokenCounter(%q): %v", tt.model, err)
			continue
		}
		if tc.EncodingName() != tt.wantEnc {
			t.Errorf("model %q: encoding = %q, want %q", tt.model, tc.EncodingName(), tt.wantEnc)
		}
	}
}

func TestTiktokenUnknownModel(t *testing.T) {
	_, err := NewTiktokenCounter("claude-3-opus")
	if err == nil {
		t.Error("expected error for unknown model claude-3-opus, got nil")
	}
}

func TestTiktokenCaseInsensitive(t *testing.T) {
	tc, err := NewTiktokenCounter("GPT-4o-Mini")
	if err != nil {
		t.Fatalf("GPT-4o-Mini should work case-insensitively: %v", err)
	}
	if tc.EncodingName() != "o200k_base" {
		t.Errorf("encoding = %q, want o200k_base", tc.EncodingName())
	}
}

func TestTiktokenUnicode(t *testing.T) {
	tc, err := NewTiktokenCounter("gpt-4o")
	if err != nil {
		t.Fatal(err)
	}
	tests := []string{
		"你好世界",
		"こんにちは",
		"🎉🚀💻",
		"Hello 世界 🌍",
	}
	for _, text := range tests {
		got := tc.CountText(text)
		if got < 1 {
			t.Errorf("CountText(%q) = %d, want >= 1", text, got)
		}
	}
}

func TestTiktokenBackend(t *testing.T) {
	tc, err := NewTiktokenCounter("gpt-4o")
	if err != nil {
		t.Fatal(err)
	}
	if tc.Backend() != BackendTiktoken {
		t.Errorf("Backend() = %v, want BackendTiktoken", tc.Backend())
	}
}

func TestTiktokenLargeInput(t *testing.T) {
	tc, err := NewTiktokenCounter("gpt-4o")
	if err != nil {
		t.Fatal(err)
	}
	large := strings.Repeat("hello world ", 10000)
	got := tc.CountText(large)
	if got < 1000 {
		t.Errorf("large input: got %d tokens, expected >= 1000", got)
	}
}
