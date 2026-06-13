<div align="center"><pre>
   ██████╗  ██████╗ ██╗  ██╗███████╗ █████╗ ██████╗ ██████╗  ██████╗  ██████╗ ███╗   ███╗
  ██╔════╝ ██╔═══██╗██║  ██║██╔════╝██╔══██╗██╔══██╗██╔══██╗██╔═══██╗██╔═══██╗████╗ ████║
  ██║  ███╗██║   ██║███████║█████╗  ███████║██║  ██║██████╔╝██║   ██║██║   ██║██╔████╔██║
  ██║   ██║██║   ██║██╔══██║██╔══╝  ██╔══██║██║  ██║██╔══██╗██║   ██║██║   ██║██║╚██╔╝██║
  ╚██████╔╝╚██████╔╝██║  ██║███████╗██║  ██║██████╔╝██║  ██║╚██████╔╝╚██████╔╝██║ ╚═╝ ██║
   ╚═════╝  ╚═════╝ ╚═╝  ╚═╝╚══════╝╚═╝  ╚═╝╚═════╝ ╚═╝  ╚═╝ ╚═════╝  ╚═════╝ ╚═╝     ╚═╝
              The context compression layer for AI agents — in Go
</pre></div>

<p align="center"><strong>Go port of <a href="https://github.com/chopratejas/headroom">headroom-core</a> &nbsp;·&nbsp; 170/170 parity &nbsp;·&nbsp; 932 tests &nbsp;·&nbsp; 6 compressors &nbsp;·&nbsp; reversible CCR</strong></p>

<p align="center">
  <img src="https://img.shields.io/badge/parity-170%2F170-brightgreen?style=flat-square" alt="Parity: 170/170">
  <img src="https://img.shields.io/badge/tests-932-blue?style=flat-square" alt="Tests: 932">
  <img src="https://img.shields.io/badge/Go-1.25+-00ADD8?style=flat-square&logo=go&logoColor=white" alt="Go 1.25+">
  <img src="https://img.shields.io/badge/license-Apache%202.0-blue?style=flat-square" alt="License: Apache 2.0">
</p>

---

Compresses everything an LLM agent reads -- tool outputs, logs, diffs, JSON arrays, search results -- before it reaches the model. Same answers, fraction of the tokens. Built for Uber's **genai-api** LLM gateway.

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
    "github.com/uber/goheadroom"
    "github.com/uber/goheadroom/compressionpolicy"
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
| `hf_tokenizer` | HuggingFace tokenizers via libtokenizers | tiktoken-go (pure Go) |
| `onnx` | ONNX runtime for ML-based content detection | heuristic rules |

Default build requires **zero CGO** and no external dependencies.

```bash
# standard build (pure Go, no CGO)
go build ./...

# with HuggingFace tokenizers
go build -tags hf_tokenizer ./...

# run all tests
go test ./...
```

## License

Apache 2.0
