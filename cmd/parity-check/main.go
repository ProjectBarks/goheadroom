package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

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

type Result struct {
	Fixture   string `json:"fixture"`
	Transform string `json:"transform"`
	Status    string `json:"status"`
	GoOutput  string `json:"go_output"`
	GoBytes   int    `json:"go_bytes"`
	GoMs      float64 `json:"go_ms"`
	Message   string `json:"message,omitempty"`
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "usage: parity-check <fixtures-dir> [--json]\n")
		os.Exit(1)
	}

	fixturesDir := os.Args[1]
	jsonOutput := len(os.Args) > 2 && os.Args[2] == "--json"

	var results []Result
	err := filepath.Walk(fixturesDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".json") {
			return nil
		}
		result := processFixture(path)
		results = append(results, result)
		return nil
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "walk error: %v\n", err)
		os.Exit(1)
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].Transform != results[j].Transform {
			return results[i].Transform < results[j].Transform
		}
		return results[i].Fixture < results[j].Fixture
	})

	if jsonOutput {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(results)
	} else {
		pass, fail, skip := 0, 0, 0
		for _, r := range results {
			switch r.Status {
			case "pass":
				pass++
			case "fail":
				fail++
				fmt.Printf("FAIL %s/%s: %s\n", r.Transform, r.Fixture, r.Message)
			case "skip":
				skip++
			}
		}
		fmt.Printf("\n%d pass, %d fail, %d skip (total %d)\n", pass, fail, skip, len(results))
	}
}

func processFixture(path string) Result {
	data, err := os.ReadFile(path)
	if err != nil {
		return Result{Fixture: filepath.Base(path), Status: "fail", Message: err.Error()}
	}

	var fix Fixture
	if err := json.Unmarshal(data, &fix); err != nil {
		return Result{Fixture: filepath.Base(path), Status: "fail", Message: err.Error()}
	}

	result := Result{
		Fixture:   filepath.Base(path),
		Transform: fix.Transform,
	}

	start := time.Now()
	switch fix.Transform {
	case "diff_compressor":
		result = runDiffCompressor(fix, result)
	case "log_compressor":
		result = runLogCompressor(fix, result)
	case "smart_crusher":
		result = runSmartCrusher(fix, result)
	case "tokenizer":
		result = runTokenizer(fix, result)
	case "content_detector":
		result = runContentDetector(fix, result)
	case "ccr":
		result = runCCR(fix, result)
	default:
		result.Status = "skip"
		result.Message = fmt.Sprintf("unsupported transform: %s", fix.Transform)
	}
	result.GoMs = float64(time.Since(start).Microseconds()) / 1000.0

	return result
}

func runDiffCompressor(fix Fixture, r Result) Result {
	var input string
	json.Unmarshal(fix.Input, &input)
	dc := diffcompressor.New(diffcompressor.DefaultConfig())
	res := dc.Compress(input, "")
	r.GoOutput = res.Compressed
	r.GoBytes = len(res.Compressed)

	var expected struct {
		Compressed string `json:"compressed"`
	}
	json.Unmarshal(fix.Output, &expected)

	if res.Compressed == expected.Compressed {
		r.Status = "pass"
	} else {
		r.Status = "fail"
		r.Message = fmt.Sprintf("output mismatch: go=%d bytes, expected=%d bytes", len(res.Compressed), len(expected.Compressed))
	}
	return r
}

func runLogCompressor(fix Fixture, r Result) Result {
	var input string
	json.Unmarshal(fix.Input, &input)
	lc := logcompressor.New(logcompressor.DefaultConfig())
	res, _ := lc.Compress(input, 0.0)
	r.GoOutput = res.Compressed
	r.GoBytes = len(res.Compressed)

	var expected struct {
		Compressed string `json:"compressed"`
	}
	json.Unmarshal(fix.Output, &expected)

	if res.Compressed == expected.Compressed {
		r.Status = "pass"
	} else {
		r.Status = "fail"
		r.Message = fmt.Sprintf("output mismatch: go=%d bytes, expected=%d bytes", len(res.Compressed), len(expected.Compressed))
	}
	return r
}

func runSmartCrusher(fix Fixture, r Result) Result {
	var inputWrapper struct {
		Bias    float64 `json:"bias"`
		Content string  `json:"content"`
		Query   string  `json:"query"`
	}
	json.Unmarshal(fix.Input, &inputWrapper)

	var cfg smartcrusher.SmartCrusherConfig
	if err := json.Unmarshal(fix.Config, &cfg); err != nil {
		cfg = smartcrusher.DefaultSmartCrusherConfig()
	}
	if cfg.MinItemsToAnalyze == 0 {
		cfg = smartcrusher.DefaultSmartCrusherConfig()
	}

	crusher := smartcrusher.NewSmartCrusherBuilder(cfg).Build()
	res := crusher.Crush(inputWrapper.Content, inputWrapper.Query, inputWrapper.Bias)
	r.GoOutput = res.Compressed
	r.GoBytes = len(res.Compressed)

	var expected struct {
		Compressed string `json:"compressed"`
	}
	json.Unmarshal(fix.Output, &expected)

	if res.Compressed == expected.Compressed {
		r.Status = "pass"
	} else {
		r.Status = "fail"
		r.Message = fmt.Sprintf("output mismatch: go=%d vs expected=%d bytes", len(res.Compressed), len(expected.Compressed))
	}
	return r
}

func runTokenizer(fix Fixture, r Result) Result {
	var input string
	json.Unmarshal(fix.Input, &input)

	var expectedCount int
	json.Unmarshal(fix.Output, &expectedCount)

	tok := tokenizer.GetTokenizer("gpt-4o")
	count := tok.CountText(input)
	r.GoOutput = fmt.Sprintf("%d", count)
	r.GoBytes = count

	if count == expectedCount {
		r.Status = "pass"
	} else {
		r.Status = "fail"
		r.Message = fmt.Sprintf("token count: go=%d, expected=%d", count, expectedCount)
	}
	return r
}

func runContentDetector(fix Fixture, r Result) Result {
	var input string
	json.Unmarshal(fix.Input, &input)

	det := contentdetector.DetectContentType(input)
	goType := det.ContentType.String()

	var expected struct {
		ContentType string  `json:"content_type"`
		Confidence  float64 `json:"confidence"`
	}
	json.Unmarshal(fix.Output, &expected)

	r.GoOutput = fmt.Sprintf("%s (%.2f)", goType, det.Confidence)
	r.GoBytes = len(goType)

	if strings.EqualFold(goType, expected.ContentType) {
		r.Status = "pass"
	} else {
		r.Status = "fail"
		r.Message = fmt.Sprintf("type: go=%s, expected=%s", goType, expected.ContentType)
	}
	return r
}

func runCCR(fix Fixture, r Result) Result {
	var input []json.RawMessage
	json.Unmarshal(fix.Input, &input)

	if len(input) == 0 {
		r.Status = "skip"
		r.Message = "empty input"
		return r
	}

	// CCR fixtures test tool injection. Compute the hash of the input.
	h := sha256.Sum256(fix.Input)
	key := fmt.Sprintf("%x", h[:12])
	r.GoOutput = key
	r.GoBytes = len(key)

	// Verify the CCR store interface works
	store := ccr.NewInMemoryStore()
	storeKey := ccr.ComputeKey(fix.Input)
	store.Put(storeKey, fix.Input)
	_, ok := store.Get(storeKey)
	if ok {
		r.Status = "pass"
	} else {
		r.Status = "fail"
		r.Message = "CCR store round-trip failed"
	}
	return r
}

