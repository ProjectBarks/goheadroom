package headroom_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/uber/goheadroom/ccr"
	"github.com/uber/goheadroom/tokenizer"
	"github.com/uber/goheadroom/transforms/contentdetector"
	"github.com/uber/goheadroom/transforms/diffcompressor"
	"github.com/uber/goheadroom/transforms/logcompressor"
	"github.com/uber/goheadroom/transforms/smartcrusher"
)

type fixture struct {
	Transform string          `json:"transform"`
	Input     json.RawMessage `json:"input"`
}

func loadFixture(b *testing.B, path string) fixture {
	data, err := os.ReadFile(path)
	if err != nil {
		b.Fatal(err)
	}
	var f fixture
	json.Unmarshal(data, &f)
	return f
}

func BenchmarkDiffCompressor(b *testing.B) {
	files, _ := filepath.Glob("testdata/parity/diff_compressor/*.json")
	for _, f := range files {
		fix := loadFixture(b, f)
		var input string
		json.Unmarshal(fix.Input, &input)
		dc := diffcompressor.New(diffcompressor.DefaultConfig())
		b.Run(filepath.Base(f), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				dc.Compress(input, "")
			}
		})
	}
}

func BenchmarkLogCompressor(b *testing.B) {
	files, _ := filepath.Glob("testdata/parity/log_compressor/*.json")
	for _, f := range files {
		fix := loadFixture(b, f)
		var input string
		json.Unmarshal(fix.Input, &input)
		lc := logcompressor.New(logcompressor.DefaultConfig())
		b.Run(filepath.Base(f), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				lc.Compress(input, 0.0)
			}
		})
	}
}

func BenchmarkSmartCrusher(b *testing.B) {
	files, _ := filepath.Glob("testdata/parity/smart_crusher/*.json")
	for _, f := range files {
		fix := loadFixture(b, f)
		var w struct {
			Bias    float64 `json:"bias"`
			Content string  `json:"content"`
			Query   string  `json:"query"`
		}
		json.Unmarshal(fix.Input, &w)
		cfg := smartcrusher.DefaultSmartCrusherConfig()
		crusher := smartcrusher.NewSmartCrusherBuilder(cfg).Build()
		b.Run(filepath.Base(f), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				crusher.Crush(w.Content, w.Query, w.Bias)
			}
		})
	}
}

func BenchmarkContentDetector(b *testing.B) {
	files, _ := filepath.Glob("testdata/parity/content_detector/*.json")
	for _, f := range files {
		fix := loadFixture(b, f)
		var input string
		json.Unmarshal(fix.Input, &input)
		b.Run(filepath.Base(f), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				contentdetector.DetectContentType(input)
			}
		})
	}
}

func BenchmarkTokenizer(b *testing.B) {
	files, _ := filepath.Glob("testdata/parity/tokenizer/*.json")
	tok := tokenizer.GetTokenizer("gpt-4o")
	for _, f := range files {
		fix := loadFixture(b, f)
		var input string
		json.Unmarshal(fix.Input, &input)
		b.Run(filepath.Base(f), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				tok.CountText(input)
			}
		})
	}
}

func BenchmarkCCR(b *testing.B) {
	files, _ := filepath.Glob("testdata/parity/ccr/*.json")
	for _, f := range files {
		fix := loadFixture(b, f)
		raw, _ := json.Marshal(fix.Input)
		key := ccr.ComputeKey(raw)
		store := ccr.NewInMemoryStore()
		b.Run(filepath.Base(f), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				store.Put(key, raw)
				store.Get(key)
			}
		})
	}
}
