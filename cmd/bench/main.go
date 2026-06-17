package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/projectbarks/goheadroom/core/parity"
	"github.com/projectbarks/goheadroom/core/parity/comparators"
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
	c, ok := comparators.DefaultRegistry().Get(fix.Transform)
	if !ok {
		fmt.Fprintf(os.Stderr, "unsupported: %s\n", fix.Transform)
		os.Exit(2)
	}

	benchN := 0
	for i := 2; i < len(os.Args); i++ {
		if os.Args[i] == "--bench" && i+1 < len(os.Args) {
			benchN, _ = strconv.Atoi(os.Args[i+1])
		}
	}

	run := func() interface{} {
		v, _ := c.Run(fix.Input, fix.Config)
		return v
	}
	if benchN > 0 {
		run()
		start := time.Now()
		for i := 0; i < benchN; i++ {
			run()
		}
		fmt.Fprintf(os.Stderr, "%d\n", time.Since(start).Nanoseconds()/int64(benchN))
	}
	fmt.Print(benchString(run()))
}

// benchString extracts the primary output string from a comparator result,
// matching the format that the Rust/Python bench binaries produce for
// string comparison in the report generator.
func benchString(v interface{}) string {
	switch val := v.(type) {
	case string:
		return val
	case int:
		return strconv.Itoa(val)
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64)
	case map[string]interface{}:
		if c, ok := val["compressed"].(string); ok {
			return c
		}
		if ct, ok := val["content_type"].(string); ok {
			if conf, ok := val["confidence"].(float64); ok {
				return fmt.Sprintf("%s:%.4f", ct, conf)
			}
		}
		if bh, ok := val["bench_hash"].(string); ok {
			return bh
		}
		b, _ := json.Marshal(val)
		return string(b)
	default:
		b, _ := json.Marshal(val)
		return string(b)
	}
}
