package core_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/projectbarks/goheadroom/core/ccr"
	"github.com/projectbarks/goheadroom/core/tokenizer"
	"github.com/projectbarks/goheadroom/core/transforms/contentdetector"
	"github.com/projectbarks/goheadroom/core/transforms/diffcompressor"
	"github.com/projectbarks/goheadroom/core/transforms/logcompressor"
	"github.com/projectbarks/goheadroom/core/transforms/smartcrusher"
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

// ---------------------------------------------------------------------------
// Warm benchmarks: pre-initialized compressors, measures pure throughput.
// Run with: go test -bench='Warm/' -benchmem .
// ---------------------------------------------------------------------------

func BenchmarkWarm(b *testing.B) {
	b.Run("DiffCompressor", benchWarmDiffCompressor)
	b.Run("LogCompressor", benchWarmLogCompressor)
	b.Run("SmartCrusher", benchWarmSmartCrusher)
	b.Run("ContentDetector", benchWarmContentDetector)
	b.Run("Tokenizer", benchWarmTokenizer)
	b.Run("CCR", benchWarmCCR)
}

func benchWarmDiffCompressor(b *testing.B) {
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

func benchWarmLogCompressor(b *testing.B) {
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

func benchWarmSmartCrusher(b *testing.B) {
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

func benchWarmContentDetector(b *testing.B) {
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

func benchWarmTokenizer(b *testing.B) {
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

func benchWarmCCR(b *testing.B) {
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

// ---------------------------------------------------------------------------
// Cold benchmarks: includes compressor creation + first-call init cost.
// Measures what a single CLI invocation pays.
// Run with: go test -bench='Cold/' -benchmem .
// ---------------------------------------------------------------------------

func BenchmarkCold(b *testing.B) {
	b.Run("DiffCompressor", benchColdDiffCompressor)
	b.Run("LogCompressor", benchColdLogCompressor)
	b.Run("SmartCrusher", benchColdSmartCrusher)
	b.Run("ContentDetector", benchColdContentDetector)
	b.Run("Tokenizer", benchColdTokenizer)
	b.Run("CCR", benchColdCCR)
}

func benchColdDiffCompressor(b *testing.B) {
	files, _ := filepath.Glob("testdata/parity/diff_compressor/*.json")
	largest := pickLargest(files)
	fix := loadFixture(b, largest)
	var input string
	json.Unmarshal(fix.Input, &input)
	b.Run(filepath.Base(largest), func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			dc := diffcompressor.New(diffcompressor.DefaultConfig())
			dc.Compress(input, "")
		}
	})
}

func benchColdLogCompressor(b *testing.B) {
	files, _ := filepath.Glob("testdata/parity/log_compressor/*.json")
	largest := pickLargest(files)
	fix := loadFixture(b, largest)
	var input string
	json.Unmarshal(fix.Input, &input)
	b.Run(filepath.Base(largest), func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			lc := logcompressor.New(logcompressor.DefaultConfig())
			lc.Compress(input, 0.0)
		}
	})
}

func benchColdSmartCrusher(b *testing.B) {
	files, _ := filepath.Glob("testdata/parity/smart_crusher/*.json")
	largest := pickLargest(files)
	fix := loadFixture(b, largest)
	var w struct {
		Bias    float64 `json:"bias"`
		Content string  `json:"content"`
		Query   string  `json:"query"`
	}
	json.Unmarshal(fix.Input, &w)
	b.Run(filepath.Base(largest), func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			cfg := smartcrusher.DefaultSmartCrusherConfig()
			crusher := smartcrusher.NewSmartCrusherBuilder(cfg).Build()
			crusher.Crush(w.Content, w.Query, w.Bias)
		}
	})
}

func benchColdContentDetector(b *testing.B) {
	files, _ := filepath.Glob("testdata/parity/content_detector/*.json")
	largest := pickLargest(files)
	fix := loadFixture(b, largest)
	var input string
	json.Unmarshal(fix.Input, &input)
	b.Run(filepath.Base(largest), func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			contentdetector.DetectContentType(input)
		}
	})
}

func benchColdTokenizer(b *testing.B) {
	files, _ := filepath.Glob("testdata/parity/tokenizer/*.json")
	largest := pickLargest(files)
	fix := loadFixture(b, largest)
	var input string
	json.Unmarshal(fix.Input, &input)
	b.Run(filepath.Base(largest), func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			tok := tokenizer.GetTokenizer("gpt-4o")
			tok.CountText(input)
		}
	})
}

func benchColdCCR(b *testing.B) {
	files, _ := filepath.Glob("testdata/parity/ccr/*.json")
	largest := pickLargest(files)
	fix := loadFixture(b, largest)
	raw, _ := json.Marshal(fix.Input)
	b.Run(filepath.Base(largest), func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			store := ccr.NewInMemoryStore()
			key := ccr.ComputeKey(raw)
			store.Put(key, raw)
			store.Get(key)
		}
	})
}

func pickLargest(files []string) string {
	best := files[0]
	bestSize := int64(0)
	for _, f := range files {
		info, err := os.Stat(f)
		if err == nil && info.Size() > bestSize {
			bestSize = info.Size()
			best = f
		}
	}
	return best
}
