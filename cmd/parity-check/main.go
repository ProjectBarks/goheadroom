package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/projectbarks/goheadroom/core/parity"
	"github.com/projectbarks/goheadroom/core/parity/comparators"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "usage: parity-check <fixtures-dir> [--json] [--only <transform>]\n")
		os.Exit(1)
	}
	fixturesDir := os.Args[1]
	jsonOutput := false
	only := ""
	for i := 2; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "--json":
			jsonOutput = true
		case "--only":
			if i+1 < len(os.Args) {
				only = os.Args[i+1]
				i++
			}
		}
	}

	registry := comparators.DefaultRegistry()
	names := registry.Names()
	if only != "" {
		names = []string{only}
	}

	type result struct {
		Fixture   string  `json:"fixture"`
		Transform string  `json:"transform"`
		Status    string  `json:"status"`
		GoMs      float64 `json:"go_ms"`
		Message   string  `json:"message,omitempty"`
	}

	var results []result
	anyFail := false

	for _, name := range names {
		c, _ := registry.Get(name)
		dir := fixturesDir + "/" + name
		start := time.Now()
		report, err := parity.RunComparator(dir, c)
		ms := float64(time.Since(start).Microseconds()) / 1000.0
		if err != nil {
			results = append(results, result{Transform: name, Status: "fail", GoMs: ms, Message: err.Error()})
			anyFail = true
			continue
		}
		if !report.IsClean() {
			anyFail = true
		}
		for _, d := range report.Diffs {
			results = append(results, result{Transform: name, Fixture: d.Fixture, Status: "fail", GoMs: ms, Message: d.CmpDiff})
		}
		for _, s := range report.Skipped {
			results = append(results, result{Transform: name, Fixture: s.Fixture, Status: "skip", GoMs: ms, Message: s.Reason})
		}
		for _, fixName := range report.Passed {
			results = append(results, result{Transform: name, Fixture: fixName, Status: "pass", GoMs: ms})
		}
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
				fmt.Printf("FAIL %s/%s\n", r.Transform, r.Fixture)
			case "skip":
				skip++
			}
		}
		fmt.Printf("\n%d pass, %d fail, %d skip (total %d)\n", pass, fail, skip, len(results))
	}
	if anyFail {
		os.Exit(1)
	}
}
