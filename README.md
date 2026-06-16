<div align="center"><pre>
   ██████╗  ██████╗ ██╗  ██╗███████╗ █████╗ ██████╗ ██████╗  ██████╗  ██████╗ ███╗   ███╗
  ██╔════╝ ██╔═══██╗██║  ██║██╔════╝██╔══██╗██╔══██╗██╔══██╗██╔═══██╗██╔═══██╗████╗ ████║
  ██║  ███╗██║   ██║███████║█████╗  ███████║██║  ██║██████╔╝██║   ██║██║   ██║██╔████╔██║
  ██║   ██║██║   ██║██╔══██║██╔══╝  ██╔══██║██║  ██║██╔══██╗██║   ██║██║   ██║██║╚██╔╝██║
  ╚██████╔╝╚██████╔╝██║  ██║███████╗██║  ██║██████╔╝██║  ██║╚██████╔╝╚██████╔╝██║ ╚═╝ ██║
   ╚═════╝  ╚═════╝ ╚═╝  ╚═╝╚══════╝╚═╝  ╚═╝╚═════╝ ╚═╝  ╚═╝ ╚═════╝  ╚═════╝ ╚═╝     ╚═╝
              The context compression layer for AI agents — in Go
</pre></div>

<p align="center"><strong>Go port of <a href="https://github.com/chopratejas/headroom">headroom-core</a> &nbsp;·&nbsp; 170/170 parity &nbsp;·&nbsp; faster than the original Rust &nbsp;·&nbsp; 6 compressors &nbsp;·&nbsp; reversible CCR</strong></p>

<p align="center">
  <img src="https://img.shields.io/badge/parity-170%2F170-brightgreen?style=flat-square" alt="Parity: 170/170">
  <img src="https://img.shields.io/badge/vs_Rust-faster-00ADD8?style=flat-square&logo=go&logoColor=white" alt="Faster than Rust">
  <img src="https://img.shields.io/badge/tests-932-blue?style=flat-square" alt="Tests: 932">
  <img src="https://img.shields.io/badge/Go-1.25+-00ADD8?style=flat-square&logo=go&logoColor=white" alt="Go 1.25+">
  <img src="https://img.shields.io/badge/license-Apache%202.0-blue?style=flat-square" alt="License: Apache 2.0">
</p>

---

Compresses everything an LLM agent reads -- tool outputs, logs, diffs, JSON arrays, search results -- before it reaches the model. Same answers, fraction of the tokens. Built for Uber's **genai-api** LLM gateway.

### Faster than the original Rust

goheadroom beats Rust headroom-core on every warm benchmark while producing byte-identical output across all 170 fixtures.

```
  Go vs Rust warm benchmarks (lower is better)
  ─────────────────────────────────────────────────────────────────

  LogCompressor      Go ██░ 2 us
                   Rust ████████████████████████████████████████████████████████████████ 63 us
                                                                          Go wins 31x

  DiffCompressor     Go ██████████████████████░ 3 us
                   Rust ████████████████████████████████████████████████████████████████ 9 us
                                                                           Go wins 3x

  CCR                Go █████████████████████░ 1 us
                   Rust ████████████████████████████████████████████████████████████████ 3 us
                                                                           Go wins 3x

  ContentDetector    Go ████████████████████████████████░ 1 us
                   Rust ████████████████████████████████████████████████████████████████ 2 us
                                                                           Go wins 2x

  SmartCrusher       Go █████████████████████████████████████████████░ 100 us
                   Rust ████████████████████████████████████████████████████████████████ 141 us
                                                                         Go wins 1.4x

  Tokenizer          Go ████████████████████████████████████████████████████████████████ 22 us
                   Rust ██████████████████████████████████████████████████████████░ 20 us
                                                                       parity (CGO)
```

<sub>Warm = library throughput, no CLI startup. Largest fixture per transform. Apple M3 Max.</sub>

<details>
<summary><strong>Optimization journey: 14 rounds, baseline to final</strong></summary>

<br>

Every round: CPU/memory profile, investigate hotspots, implement in parallel worktrees, red team review, merge what survives.

```
  Before ████████████████████████████████████████████████  2,059 us   After ██░ 44 us     LogCompressor    46.8x
  Before ██░ 48 us                                                   After ░ 1.3 us      ContentDetector  37.8x
  Before ███████████░ 476 us                                         After █░ 22 us       Tokenizer        21.6x
  Before ░ 14 us                                                     After ░ 0.66 us     CCR              21.7x
  Before █████░ 208 us                                               After ░ 15 us        DiffCompressor   13.7x
  Before ███████████████████████████░ 1,158 us                       After ██████████░ 434 us  SmartCrusher  2.7x
```

Key techniques applied:

- **Zero compiled regexp in hot paths** -- every `regexp.MustCompile` replaced with string ops, `|0x20` ASCII case folding, `[256]byte` dispatch tables
- **FNV-1a simhash** -- replaced `crypto/md5` for non-cryptographic gram hashing (3x speedup across all compressors)
- **Lazy expensive work** -- defer `ComputeOptimalK` until needed, lazy store allocation, nil metadata maps
- **Rust FFI tokenizer** -- `daulet/tokenizers` wraps the same Rust tiktoken crate; 5.6x serial / 60x parallel vs pure-Go tiktoken
- **Shared `internal/textutil`** -- deduplicated keyword matching across 4 packages into one optimized implementation
- **Stack-allocated buffers** -- `[32]byte` hex, `[256]byte` lowercase, `uint64` bitmasks replacing `[]bool` arrays
- **Red team review** -- every optimization reviewed before landing; 6 proposals rejected for parity risk or regression

</details>

Uses a **Compress-Cache-Retrieve (CCR)** architecture: content is compressed inline with compact representations, originals are stashed in a retrieval store, and the model can request them back on demand. Nothing is lost.

## How it works

```
  Your app / agent
    │   request body (Anthropic · OpenAI Chat · OpenAI Responses)
    ▼
  ┌──────────────────────────────────────────────────────────────┐
  │  goheadroom                                                  │
  │  ──────────────────────────────────────────────────────────  │
  │                                                              │
  │  LiveZone  ─►  ContentDetector  ─►  CompressionPipeline     │
  │                                      │                       │
  │                    ┌─────────────────┼─────────────────┐     │
  │                    │                 │                 │     │
  │               DiffCompressor   LogCompressor    SmartCrusher │
  │               hunk removal     dedup/cluster    CSV compact  │
  │               context trim     line selection   adaptive-k   │
  │                    │                 │                 │     │
  │                    └─────────────────┼─────────────────┘     │
  │                                      │                       │
  │                              CacheAligner + CCR store        │
  └──────────────────────────────────────────────────────────────┘
    │   compressed body  +  retrieval hashes
    ▼
  LLM provider
```

## Transforms

| Transform | What it does | Example |
|---|---|---|
| **SmartCrusher** | Compacts JSON arrays to CSV-with-schema | `[100]{id:int,name:str}\n1,alice\n2,bob` |
| **DiffCompressor** | Strips lockfile hunks, collapses whitespace-only changes | 2,364 B -> 1,913 B |
| **LogCompressor** | Deduplicates repeated lines, clusters by template | `[297 lines omitted: 1 ERROR, 300 INFO]` |
| **SearchCompressor** | Deduplicates near-identical results via simhash | 10 results -> 4 unique |
| **ContentDetector** | Classifies as text/json/code/diff/build/search/html | `SourceCode:0.85` |
| **AdaptiveSizer** | Computes optimal keep-count via bigram diversity curves | 100 items -> k=12 |

## Quick start

```go
import (
    "github.com/projectbarks/goheadroom/core"
    "github.com/projectbarks/goheadroom/core/compressionpolicy"
)

resp, err := headroom.CompressLiveZone(headroom.CompressRequest{
    Body:   requestBody,
    Format: headroom.FormatOpenAIChat,
    Mode:   compressionpolicy.ModeAggressive,
})
// resp.Body = compressed request, ready to forward
// resp.Stats = per-transform metrics
```

### Using individual transforms

```go
// Compress a git diff
dc := diffcompressor.New(diffcompressor.DefaultConfig())
result := dc.Compress(diffContent, "")
fmt.Println(result.Compressed)

// Crush a JSON array
crusher := smartcrusher.NewSmartCrusherBuilder(
    smartcrusher.DefaultSmartCrusherConfig(),
).Build()
result := crusher.Crush(jsonContent, query, 0.5)
fmt.Println(result.Compressed)

// Compress logs
lc := logcompressor.New(logcompressor.DefaultConfig())
result, stats := lc.Compress(logContent, 0.0)
fmt.Println(result.Compressed)
```

## Project structure

```
compress.go                     CompressLiveZone — top-level entry point
authmode/                       auth mode detection (codex, API key, etc.)
cachecontrol/                   cache-control header parsing
ccr/                            compress-cache-retrieve store interface
compressionpolicy/              mode selection (none / moderate / aggressive)
relevance/                      query relevance scoring
signals/                        signal extraction from request metadata
tokenizer/                      tiktoken-compatible token counting

transforms/
├── adaptivesizer/              optimal-k via bigram curves + knee detection
├── anchorselector/             boundary-preserving item selection
├── contentdetector/            content type classification
├── diffcompressor/             git diff compression
├── livezone/                   per-format compression (Anthropic, OpenAI)
├── logcompressor/              log dedup + template clustering
├── pipeline/                   reformat -> offload -> compress orchestrator
│   ├── offloads/               bloat estimation per content type
│   └── reformats/              minification + template extraction
├── searchcompressor/           simhash-based result dedup
└── smartcrusher/               JSON array compaction engine
    └── compaction/             tabular CSV-with-schema renderer
```

## Parity

<table>
<tr><td><strong>170 / 170</strong></td><td>byte-identical output vs Rust across all transforms</td></tr>
<tr><td><strong>932</strong></td><td>tests across 22 packages</td></tr>
<tr><td><strong>6</strong></td><td>compressor types with full coverage</td></tr>
</table>

Fixture coverage spans diff compressor (27), log compressor (20), smart crusher (17), content detector (21), tokenizer (40), CCR (25), and cache aligner (20).

```bash
# generate the interactive parity report
go build -o /tmp/goheadroom-bench ./cmd/bench/
python3 scripts/generate-parity-report.py
open parity-report.html
```

## Build tags

| Tag | What it enables | Fallback without it |
|---|---|---|
| `CGO_ENABLED=1` | Rust FFI tokenizer via daulet/tokenizers (5.6x faster) | tiktoken-go (pure Go) |
| `hf_tokenizer` | HuggingFace tokenizers via libtokenizers | tiktoken-go (pure Go) |
| `onnx` | ONNX runtime for ML-based content detection | heuristic rules |

Default build requires **zero CGO** and no external dependencies. With CGO, the tokenizer uses the Rust tiktoken implementation via FFI for near-native speed.

```bash
# standard build (pure Go, no CGO)
go build ./...

# with Rust FFI tokenizer (recommended for production)
CGO_ENABLED=1 go build -ldflags="-extldflags '-L.'" ./...

# run all tests
go test ./...
```

## License

Apache 2.0
