package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/uber/goheadroom/transforms/diffcompressor"
	"github.com/uber/goheadroom/transforms/logcompressor"
	"github.com/uber/goheadroom/transforms/smartcrusher"
)

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

	var fixture struct {
		Transform string          `json:"transform"`
		Input     json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		fmt.Fprintf(os.Stderr, "parse: %v\n", err)
		os.Exit(1)
	}

	switch fixture.Transform {
	case "diff_compressor":
		var input string
		json.Unmarshal(fixture.Input, &input)
		dc := diffcompressor.New(diffcompressor.DefaultConfig())
		result := dc.Compress(input, "")
		fmt.Print(result.Compressed)

	case "log_compressor":
		var input string
		json.Unmarshal(fixture.Input, &input)
		lc := logcompressor.New(logcompressor.DefaultConfig())
		result, _ := lc.Compress(input, 0.0)
		fmt.Print(result.Compressed)

	case "smart_crusher":
		var inputWrapper struct {
			Bias    float64 `json:"bias"`
			Content string  `json:"content"`
			Query   string  `json:"query"`
		}
		json.Unmarshal(fixture.Input, &inputWrapper)
		cfg := smartcrusher.DefaultSmartCrusherConfig()
		crusher := smartcrusher.NewSmartCrusherBuilder(cfg).Build()
		result := crusher.Crush(inputWrapper.Content, inputWrapper.Query, inputWrapper.Bias)
		fmt.Print(result.Compressed)

	default:
		fmt.Fprintf(os.Stderr, "unknown transform: %s\n", fixture.Transform)
		os.Exit(1)
	}
}
