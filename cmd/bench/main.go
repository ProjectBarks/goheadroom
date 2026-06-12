package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/uber/goheadroom/ccr"
	"github.com/uber/goheadroom/tokenizer"
	"github.com/uber/goheadroom/transforms/contentdetector"
	"github.com/uber/goheadroom/transforms/diffcompressor"
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
		fmt.Fprintf(os.Stderr, "usage: bench <fixture.json>\n")
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

	switch fix.Transform {
	case "diff_compressor":
		var input string
		json.Unmarshal(fix.Input, &input)
		dc := diffcompressor.New(diffcompressor.DefaultConfig())
		result := dc.Compress(input, "")
		fmt.Print(result.Compressed)

	case "log_compressor":
		var input string
		json.Unmarshal(fix.Input, &input)
		lc := logcompressor.New(logcompressor.DefaultConfig())
		result, _ := lc.Compress(input, 0.0)
		fmt.Print(result.Compressed)

	case "smart_crusher":
		var w struct {
			Bias    float64 `json:"bias"`
			Content string  `json:"content"`
			Query   string  `json:"query"`
		}
		json.Unmarshal(fix.Input, &w)
		cfg := smartcrusher.DefaultSmartCrusherConfig()
		crusher := smartcrusher.NewSmartCrusherBuilder(cfg).Build()
		result := crusher.Crush(w.Content, w.Query, w.Bias)
		fmt.Print(result.Compressed)

	case "tokenizer":
		var input string
		json.Unmarshal(fix.Input, &input)
		tok := tokenizer.GetTokenizer("gpt-4o")
		count := tok.CountText(input)
		fmt.Print(count)

	case "content_detector":
		var input string
		json.Unmarshal(fix.Input, &input)
		det := contentdetector.DetectContentType(input)
		name := ctName(det.ContentType)
		fmt.Printf("%s:%.2f", name, det.Confidence)

	case "ccr":
		var input []json.RawMessage
		json.Unmarshal(fix.Input, &input)
		if len(input) > 0 {
			raw, _ := json.Marshal(input[0])
			key := ccr.ComputeKey(raw)
			store := ccr.NewInMemoryStore()
			store.Put(key, raw)
			got, ok := store.Get(key)
			if ok && len(got) > 0 {
				fmt.Print("OK:" + key)
			} else {
				fmt.Print("FAIL")
			}
		} else {
			fmt.Print("OK:empty")
		}

	case "cache_aligner":
		fmt.Print("SKIP:cache_aligner")

	default:
		fmt.Fprintf(os.Stderr, "unsupported: %s\n", fix.Transform)
		os.Exit(2)
	}
}

func ctName(ct contentdetector.ContentType) string {
	names := map[contentdetector.ContentType]string{
		contentdetector.PlainText:     "text",
		contentdetector.JsonArray:     "json",
		contentdetector.SourceCode:    "code",
		contentdetector.SearchResults: "search",
		contentdetector.BuildOutput:   "build",
		contentdetector.GitDiff:       "diff",
		contentdetector.Html:          "html",
	}
	if n, ok := names[ct]; ok {
		return n
	}
	return "unknown"
}

func init() {
	_ = strings.TrimSpace
}
