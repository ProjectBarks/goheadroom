package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/uber/goheadroom/tokenizer"
	"github.com/uber/goheadroom/transforms/codecompressor"
	"github.com/uber/goheadroom/transforms/contentdetector"
	"github.com/uber/goheadroom/transforms/diffcompressor"
	"github.com/uber/goheadroom/transforms/jsoncompressor"
	"github.com/uber/goheadroom/transforms/livezone"
	"github.com/uber/goheadroom/transforms/logcompressor"
	"github.com/uber/goheadroom/transforms/smartcrusher"
)

type Fixture struct {
	Transform string          `json:"transform"`
	Input     json.RawMessage `json:"input"`
	Output    json.RawMessage `json:"output"`
	Config    json.RawMessage `json:"config"`
}

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

	var fix Fixture
	if err := json.Unmarshal(data, &fix); err != nil {
		fmt.Fprintf(os.Stderr, "parse: %v\n", err)
		os.Exit(1)
	}

	// --bench N: run the transform N times, output ns/op to stderr, output to stdout once
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
		// Warm up
		run()
		// Timed iterations
		start := time.Now()
		for i := 0; i < benchN; i++ {
			run()
		}
		elapsed := time.Since(start)
		nsPerOp := elapsed.Nanoseconds() / int64(benchN)
		fmt.Fprintf(os.Stderr, "%d\n", nsPerOp)
	}

	// Final run for output
	fmt.Print(run())
}

func makeRunner(fix Fixture) func() string {
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
		return func() string { r, _ := lc.Compress(input, 0.0); return r.Compressed }

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

