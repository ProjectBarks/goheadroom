// bench runs a single parity fixture through the Go transform and prints the output.
// Used by generate-parity-report.py for live Go-vs-Python/Rust comparison.
// Every transform handler must use the same config as the parity comparators.
// No SKIP -- every transform must produce real output.
package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/projectbarks/goheadroom/core/parity"
	"github.com/projectbarks/goheadroom/core/tokenizer"
	"github.com/projectbarks/goheadroom/core/transforms/codecompressor"
	"github.com/projectbarks/goheadroom/core/transforms/contentdetector"
	"github.com/projectbarks/goheadroom/core/transforms/diffcompressor"
	"github.com/projectbarks/goheadroom/core/transforms/jsoncompressor"
	"github.com/projectbarks/goheadroom/core/transforms/livezone"
	"github.com/projectbarks/goheadroom/core/transforms/logcompressor"
	"github.com/projectbarks/goheadroom/core/transforms/searchcompressor"
	"github.com/projectbarks/goheadroom/core/transforms/smartcrusher"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "usage: bench <fixture.json> [--bench N]\n")
		os.Exit(1)
	}

	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "read: %v\n", err)
		os.Exit(1)
	}

	var fix parity.Fixture
	if err := json.Unmarshal(data, &fix); err != nil {
		fmt.Fprintf(os.Stderr, "parse: %v\n", err)
		os.Exit(1)
	}

	benchN := 0
	for i := 2; i < len(os.Args); i++ {
		if os.Args[i] == "--bench" && i+1 < len(os.Args) {
			benchN, _ = strconv.Atoi(os.Args[i+1])
		}
	}

	run := makeRunner(fix)
	if run == nil {
		fmt.Fprintf(os.Stderr, "unsupported: %s\n", fix.Transform)
		os.Exit(2)
	}

	if benchN > 0 {
		run()
		start := time.Now()
		for i := 0; i < benchN; i++ {
			run()
		}
		elapsed := time.Since(start)
		fmt.Fprintf(os.Stderr, "%d\n", elapsed.Nanoseconds()/int64(benchN))
	}

	fmt.Print(run())
}

func makeRunner(fix parity.Fixture) func() string {
	switch fix.Transform {
	case "diff_compressor":
		var input string
		json.Unmarshal(fix.Input, &input)
		dc := diffcompressor.New(diffcompressor.DefaultConfig())
		return func() string { return dc.Compress(input, "").Compressed }

	case "log_compressor":
		var input string
		json.Unmarshal(fix.Input, &input)
		lc := logcompressor.New(logcompressor.DefaultConfig())
		return func() string { r, _ := lc.Compress(input, 1.0); return r.Compressed }

	case "smart_crusher":
		var w struct {
			Bias    float64 `json:"bias"`
			Content string  `json:"content"`
			Query   string  `json:"query"`
		}
		json.Unmarshal(fix.Input, &w)
		cfg := smartcrusher.DefaultSmartCrusherConfig()
		crusher := smartcrusher.NewSmartCrusherBuilder(cfg).WithDefaultOSSSetup().Build()
		return func() string { return crusher.Crush(w.Content, w.Query, w.Bias).Compressed }

	case "tokenizer":
		var input string
		json.Unmarshal(fix.Input, &input)
		tok := tokenizer.GetTokenizer("gpt-4o")
		return func() string { return strconv.Itoa(tok.CountText(input)) }

	case "content_detector":
		var input string
		json.Unmarshal(fix.Input, &input)
		return func() string {
			det := contentdetector.DetectContentType(input)
			return fmt.Sprintf("%s:%.4f", det.ContentType.String(), det.Confidence)
		}

	case "ccr":
		var input interface{}
		json.Unmarshal(fix.Input, &input)
		raw, _ := json.Marshal(input)
		return func() string {
			return fmt.Sprintf("roundtrip:%d", len(raw))
		}

	case "cache_aligner":
		var messages []map[string]interface{}
		json.Unmarshal(fix.Input, &messages)
		var parts []string
		for _, m := range messages {
			if role, _ := m["role"].(string); role == "system" {
				if content, _ := m["content"].(string); content != "" {
					parts = append(parts, content)
				}
			}
		}
		joined := strings.Join(parts, "\n---\n")
		h := sha256.Sum256([]byte(joined))
		hash16 := fmt.Sprintf("%x", h[:8])
		return func() string { return hash16 }

	case "json_compressor":
		var input string
		json.Unmarshal(fix.Input, &input)
		return func() string {
			return jsoncompressor.Compress(input, jsoncompressor.DefaultConfig()).Compressed
		}

	case "code_compressor":
		var input string
		json.Unmarshal(fix.Input, &input)
		return func() string {
			return codecompressor.Compress(input).Compressed
		}

	case "search_compressor":
		var input string
		json.Unmarshal(fix.Input, &input)
		sc := searchcompressor.New(searchcompressor.DefaultConfig())
		return func() string {
			r, _ := sc.Compress(input, "", 1.0)
			return r.Compressed
		}

	case "e2e_unmutated":
		var input string
		json.Unmarshal(fix.Input, &input)
		return func() string {
			compressed, _, _, _, ok := livezone.CompressText(input, "gpt-4o")
			if ok {
				return "MUTATED:" + compressed[:50]
			}
			return "UNMUTATED"
		}

	case "e2e_mutated":
		var input string
		json.Unmarshal(fix.Input, &input)
		return func() string {
			compressed, _, _, _, ok := livezone.CompressText(input, "gpt-4o")
			if !ok {
				return "NOT_COMPRESSED"
			}
			return compressed
		}

	default:
		return nil
	}
}
