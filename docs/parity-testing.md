# Parity Testing

## Goal

Every Go transform in goheadroom must produce **byte-identical output** to the
Python/Rust SOT (Source of Truth) implementation in headroom. No exceptions,
no skips, no normalization.

## Architecture

```
testdata/parity/<transform>/<hash>.json   ← fixture files (input + expected SOT output)
cmd/parity-check/main.go                  ← Go binary: runs transform, compares to fixture
cmd/bench/main.go                         ← Go binary: runs transform, outputs result (for report)
scripts/python-bench.py                   ← Python wrapper: runs Python headroom transform
scripts/generate-parity-report.py         ← Generates HTML report comparing Go vs Python/Rust
```

## Comparison Rules

- `PYTHON_NATIVE_TRANSFORMS` (content_detector, ccr, cache_aligner, code_compressor,
  json_compressor): Go output is compared against **Python** output.
- All other transforms: Go output is compared against **Rust** output.
- Rust bench binary: `/Users/bbarker/headroom-src/target/release/headroom-bench`

## Adding a New Transform

1. Add handler to `cmd/bench/main.go` (same switch pattern)
2. Add handler to `cmd/parity-check/main.go` (same switch pattern)
3. Add handler to `scripts/python-bench.py`
4. Generate fixtures: run Rust or Python on test inputs, save as fixture JSON
5. Run parity-check to verify: `go run ./cmd/parity-check testdata/parity`
6. Rebuild bench binary and regenerate report

## Fixture Format

```json
{
  "transform": "transform_name",
  "input": "<string or object>",
  "output": {"compressed": "<expected output>", ...},
  "config": {"<optional config overrides>": "..."}
}
```

## Rules

- **No SKIP.** Every fixture must pass or fail.
- **No normalization.** Comparison is byte-identical (`==`).
- **Same config everywhere.** bench, parity-check, and production code must use
  identical configuration for each transform.
