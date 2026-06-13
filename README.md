# goheadroom

Go implementation of [headroom-core](https://github.com/uber/headroom) -- a context compression layer for LLM agents. Designed for integration into Uber's genai-api LLM gateway.

Headroom sits between callers and LLM providers, transparently compressing conversation context to reduce token usage without losing information the model needs. It uses a **Compress-Cache-Retrieve (CCR)** architecture: compressible content is replaced inline with compact representations, and the original is stashed in a retrieval store so the model can request it back on demand.

## Architecture

```
caller -> genai-api -> goheadroom -> LLM provider
                          |
                    ┌─────┴──────┐
                    │  LiveZone  │   entry point per API format
                    └─────┬──────┘
                          │
                ┌─────────┼─────────┐
                v         v         v
           Anthropic  OpenAI Chat  OpenAI Responses
                │         │         │
                v         v         v
           ┌────────────────────────────┐
           │   CompressionPipeline      │
           │                            │
           │  content detection         │
           │  -> reformat (minify/template)
           │  -> offload (bloat estimation)
           │  -> compress (transform)   │
           └────────────────────────────┘
                          │
              ┌───────────┼───────────┐
              v           v           v
         DiffCompressor  LogCompressor  SmartCrusher
              │           │               │
              v           v               v
         hunk removal   line selection   tabular compaction
         context trim   dedup/cluster    adaptive item selection
                                         outlier detection
```

## Transforms

| Transform | What it does |
|---|---|
| **DiffCompressor** | Strips lockfile hunks, collapses whitespace-only changes, trims excessive diff context |
| **LogCompressor** | Deduplicates repeated log lines, clusters by template, selects representative samples |
| **SmartCrusher** | Compacts JSON arrays to CSV-with-schema format (`[N]{col:type,...}\nrows`), adaptively selects items by diversity and relevance |
| **SearchCompressor** | Deduplicates near-identical search results using simhash similarity |
| **ContentDetector** | Classifies input as plain text, JSON, source code, git diff, build output, search results, or HTML |
| **AdaptiveSizer** | Computes optimal keep-count (k) using bigram diversity curves and knee detection |

## Usage

```go
import "github.com/uber/goheadroom"

req := headroom.CompressRequest{
    Body:   requestBody,
    Format: headroom.FormatOpenAIChat,
    Mode:   compressionpolicy.ModeAggressive,
}

resp, err := headroom.CompressLiveZone(req)
if err != nil {
    // handle error
}
// resp.Body is the compressed request body
// resp.Stats has per-transform metrics
```

## Project structure

```
compress.go                  top-level CompressLiveZone API
authmode/                    auth mode detection (codex, API key, etc.)
cachecontrol/                cache-control header parsing
ccr/                         compress-cache-retrieve store interface + in-memory impl
compressionpolicy/           compression mode selection (none/moderate/aggressive)
relevance/                   query relevance scoring for context prioritization
signals/                     signal extraction from request metadata
tokenizer/                   tiktoken-compatible token counting (gpt-4o, claude, etc.)
transforms/
  adaptivesizer/             optimal-k computation via bigram curves + knee detection
  anchorselector/            anchor-based item selection preserving boundaries
  contentdetector/           content type classification (text/json/diff/code/...)
  diffcompressor/            git diff compression (lockfiles, context, whitespace)
  livezone/                  per-format live zone compression (Anthropic, OpenAI)
  logcompressor/             log dedup, template clustering, line selection
  pipeline/                  orchestrator chaining reformat -> offload -> compress
    offloads/                bloat estimation wrappers per content type
    reformats/               minification and template extraction
  searchcompressor/          search result deduplication via simhash
  smartcrusher/              JSON array compaction, adaptive item selection
    compaction/              tabular CSV-with-schema compaction engine
```

## Parity

170/170 test fixtures produce byte-identical output between Go and Rust implementations across all transforms: diff compressor, log compressor, smart crusher, content detector, tokenizer, and CCR.

Generate the parity report:

```bash
go build -o /tmp/goheadroom-bench ./cmd/bench/
python3 scripts/generate-parity-report.py
open parity-report.html
```

## Tests

```bash
go test ./...
```

932 tests across 22 packages.

## Build tags

Two optional build tags enable CGO-dependent features:

- `hf_tokenizer` -- uses HuggingFace tokenizers via libtokenizers for exact token counts
- `onnx` -- enables ONNX runtime for ML-based content detection

Without these tags, the library uses pure-Go fallbacks (tiktoken-go for tokenization, heuristic rules for detection).
