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
- Rust bench binary: set via `$PARITY_RUST_BIN` or `--rust-bin`, or place `headroom-bench` on `$PATH`

## Adding a New Transform

1. Create a comparator in `core/parity/comparators/<transform>.go` (~20 lines)
2. Add it to `AllComparators()` in `core/parity/comparators/all.go`
3. Add handler to `scripts/python-bench.py` (for Python-native transforms)
4. Generate fixtures: run Rust or Python on test inputs, save as fixture JSON under `core/testdata/parity/<transform>/`
5. Add a `parity_test.go` in the transform's package (~15 lines calling `parity.RunFixtures`)
6. Run: `go test ./core/transforms/<pkg>/ -run Parity -v`

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

## Environment Variables

All binary paths are configurable. No hardcoded paths.

| Variable | Flag | Purpose |
|---|---|---|
| `$PARITY_GO_BIN` | `--go-bin` | Go parity-check binary |
| `$PARITY_RUST_BIN` | `--rust-bin` | Rust headroom-bench binary |
| `$PARITY_PYTHON_BIN` | `--python-bin` | Python bench script |
| `$PARITY_FIXTURES` | `--fixtures` | Fixtures directory |
