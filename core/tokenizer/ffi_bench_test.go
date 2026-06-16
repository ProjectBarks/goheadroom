//go:build cgo

package tokenizer

import (
	"strings"
	"testing"
)

func BenchmarkTokenizerComparison(b *testing.B) {
	text := strings.Repeat("The quick brown fox jumps over the lazy dog. ", 300)

	b.Run("tiktoken-go/EncodeOrdinary", func(b *testing.B) {
		tc, err := NewTiktokenCounter("gpt-4o")
		if err != nil {
			b.Fatal(err)
		}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			tc.CountText(text)
		}
	})

	b.Run("daulet-FFI/FromTiktoken", func(b *testing.B) {
		tc, err := newTiktokenFFI("gpt-4o")
		if err != nil {
			b.Fatal(err)
		}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			tc.CountText(text)
		}
	})
}

func BenchmarkTokenizerParallel(b *testing.B) {
	text := strings.Repeat("The quick brown fox jumps over the lazy dog. ", 300)

	b.Run("tiktoken-go/Parallel", func(b *testing.B) {
		tc, err := NewTiktokenCounter("gpt-4o")
		if err != nil {
			b.Fatal(err)
		}
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				tc.CountText(text)
			}
		})
	})

	b.Run("daulet-FFI/Parallel", func(b *testing.B) {
		tc, err := newTiktokenFFI("gpt-4o")
		if err != nil {
			b.Fatal(err)
		}
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				tc.CountText(text)
			}
		})
	})
}

func TestFFIParity(t *testing.T) {
	ffiTok, err := newTiktokenFFI("gpt-4o")
	if err != nil {
		t.Fatal(err)
	}
	goTok, err := NewTiktokenCounter("gpt-4o")
	if err != nil {
		t.Fatal(err)
	}

	tests := []string{
		"hello",
		"Hello, world!",
		"the quick brown fox jumps over the lazy dog",
		strings.Repeat("The quick brown fox jumps over the lazy dog. ", 300),
		"func main() {\n\tfmt.Println(\"Hello, world!\")\n}",
		"你好世界 Hello 🌍",
		"ERROR: connection refused at 192.168.1.1:8080\nWARN: retrying in 5s",
		strings.Repeat("a", 10000),
		"line1\nline2\nline3\n\n\nline6",
		"it's don't won't can't I'm you're they'd we'll",
	}

	for _, text := range tests {
		goCount := goTok.CountText(text)
		ffiCount := ffiTok.CountText(text)
		if goCount != ffiCount {
			t.Errorf("parity mismatch for %q...: go=%d, ffi=%d",
				text[:min(50, len(text))], goCount, ffiCount)
		}
	}
}

func TestFFIAllEncodings(t *testing.T) {
	models := []struct {
		model   string
		encName string
	}{
		{"gpt-4o", "o200k_base"},
		{"gpt-4", "cl100k_base"},
		{"code-davinci-002", "p50k_base"},
		{"davinci", "r50k_base"},
	}
	for _, m := range models {
		t.Run(m.model, func(t *testing.T) {
			ffi, err := newTiktokenFFI(m.model)
			if err != nil {
				t.Fatal(err)
			}
			go_, err := NewTiktokenCounter(m.model)
			if err != nil {
				t.Fatal(err)
			}
			if ffi.EncodingName() != m.encName {
				t.Errorf("FFI encoding = %q, want %q", ffi.EncodingName(), m.encName)
			}
			text := "Hello world! The quick brown fox jumps over the lazy dog."
			if ffi.CountText(text) != go_.CountText(text) {
				t.Errorf("count mismatch: ffi=%d, go=%d", ffi.CountText(text), go_.CountText(text))
			}
		})
	}
}

func TestFFIRegistryIntegration(t *testing.T) {
	tok := GetTokenizer("gpt-4o")
	if tok == nil {
		t.Fatal("GetTokenizer returned nil")
	}
	count := tok.CountText("hello world")
	if count < 1 {
		t.Errorf("expected positive count, got %d", count)
	}
	if _, ok := tok.(*TiktokenFFICounter); !ok {
		t.Errorf("expected FFI counter, got %T", tok)
	}
}
