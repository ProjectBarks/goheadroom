# Headroom Proxy: Complete Technical Analysis

## Architecture Overview

Headroom is a **hybrid Python + Rust** LLM proxy that intercepts requests to LLM providers, compresses context to reduce token usage, and forwards the optimized request upstream. The Rust layer is being built crate-by-crate with byte-parity guarantees against the original Python implementation.

### Two Proxy Implementations

```
Client -> Rust proxy (axum) -> Python proxy (FastAPI) -> LLM backend
```

| Layer | Language | Framework | Entry Point |
|---|---|---|---|
| **Python Proxy** | Python | FastAPI + Uvicorn | `headroom/proxy/server.py` via `headroom proxy` CLI |
| **Rust Proxy** | Rust | axum + tokio + reqwest | `crates/headroom-proxy/src/main.rs` binary |

The **Python proxy** is the main, feature-complete server users run today. It owns semantic caching, rate limiting, budget enforcement, memory, image compression, ML-based compression, the dashboard, and all Python-side compressors.

The **Rust proxy** sits in front of the Python proxy as a transparent reverse proxy. It progressively takes over compression duties as Rust ports reach byte-parity. It handles HTTP routing, SSE streaming, WebSocket passthrough, Bedrock SigV4 signing, Vertex ADC auth, and live-zone compression for all ported transforms.

### Rust Workspace (`crates/`)

| Crate | Purpose |
|---|---|
| `headroom-core` | All compression transforms, tokenizers, CCR store, relevance scoring |
| `headroom-proxy` | Standalone Rust reverse proxy (axum + tokio) |
| `headroom-py` | PyO3 bridge exposing Rust transforms to the Python runtime (maturin-built) |
| `headroom-parity` | Parity test harness ensuring Rust output is byte-identical to Python |

---

## Compressors

### 1. Smart Crusher (JSON arrays of objects)

**Source:** `headroom/transforms/smart_crusher.py` (Python), `crates/headroom-core/src/transforms/smart_crusher/` (Rust)

Statistical compressor for structured data like tool outputs and API responses. Deduplicates schemas, samples representative rows, preserves error/outlier items, and performs anchor-aware selection. Detects sequential patterns, rare status values, structural outliers, and score fields. The Rust port is byte-equal to Python output for every parity fixture.

Sub-components:
- `analyzer.rs` / `SmartAnalyzer`: field analysis and statistics
- `classifier.rs`: classifies arrays by type (homogeneous objects, strings, numbers, mixed)
- `crusher.rs` / `SmartCrusher`: main compression entry point
- `crushers.rs`: per-type crush implementations (object, string, number arrays)
- `compaction/`: structured compaction (classifier, compactor, formatter, IR, walker)
- `orchestration.rs`: deduplication, slot filling, index prioritization
- `outliers.rs`: error item preservation, rare status detection, structural outlier detection
- `planning.rs` / `SmartCrusherPlanner`: compression strategy planning
- `anchors.rs`: query anchor extraction and matching
- `statistics.rs`: entropy calculation, sequential pattern detection, UUID detection
- `constraints.rs`: keep-errors and keep-structural-outliers constraints
- `observer.rs` / `TracingObserver`: OpenTelemetry-compatible event emission

### 2. Diff Compressor (git diffs / unified diffs)

**Source:** `headroom/transforms/diff_compressor.py` (Python), `crates/headroom-core/src/transforms/diff_compressor.rs` (Rust)

Compresses unified diffs by trimming context lines, dropping low-value hunks, and preserving change-bearing lines. Gates on context-to-change ratio.

### 3. Log Compressor (build/test output)

**Source:** `headroom/transforms/log_compressor.py` (Python), `crates/headroom-core/src/transforms/log_compressor.rs` (Rust)

Compresses build logs and CI output. Detects log levels, deduplicates repeated lines, and preserves error/warning lines. Parses log format and individual log lines with level detection.

### 4. Search Compressor (grep/ripgrep results)

**Source:** `headroom/transforms/search_compressor.py` (Python), `crates/headroom-core/src/transforms/search_compressor.rs` (Rust)

Compresses search results by clustering matches across files and sampling representative results. Tracks per-file match counts and statistics.

### 5. Code-Aware Compressor (source code)

**Source:** `headroom/transforms/code_compressor.py` (Python only)

AST-based compression using tree-sitter. Preserves imports, function signatures, type annotations, and error handlers while compressing function bodies. Guarantees syntactically valid output.

Supported languages:
- **Tier 1:** Python, JavaScript, TypeScript
- **Tier 2:** Go, Rust, Java, C, C++

Reference: [LongCodeZip (arXiv:2510.00446)](https://arxiv.org/abs/2510.00446)

### 6. Kompress (plain text, ML-based)

**Source:** `headroom/transforms/kompress_compressor.py` (Python only)

ModernBERT token compressor using the `chopratejas/kompress-v2-base` model. Runs ONNX inference to classify which tokens to keep vs. drop. Uses weight-only int8 quantization (261MB). Requires `pip install headroom-ai[ml]`.

Evaluated on labeled dataset_v2 test split (n=500): f1=0.9130, must_keep_recall=0.9765, keep_rate=0.8097.

### 7. LLMLingua-2 (ML-based text compression)

**Source:** Python side, requires `[ml]` extra

Uses torch + transformers for ML-based compression. Configurable compression rate (default 0.3 = keep 30%).

### 8. Image Compressor

**Source:** `headroom/image/compressor.py`, `headroom/image/trained_router.py`, `headroom/image/tile_optimizer.py`, `headroom/image/onnx_router.py`

Detects images in messages, routes to optimal compression technique via a trained model. Uses RapidOCR (v1 or v3) for text extraction. Applies provider-specific compression and tile optimization.

### 9. Content Router (orchestrator)

**Source:** `headroom/transforms/content_router.py` (Python)

Auto-detects content type and dispatches to the optimal compressor. Handles mixed content by splitting sections and routing each independently. Supports source hints for highest-confidence routing.

Routing strategy:
1. Use source hint if available
2. Check for mixed content (split and route sections)
3. Detect content type (JSON, code, search, logs, text)
4. Route to appropriate compressor
5. Reassemble and return with routing metadata

---

## Pipeline Transforms (Rust `headroom-core`)

### Reformat Transforms (lossless)

These pack content denser without dropping any information. Output is semantically equivalent to input.

| Transform | Source | Description |
|---|---|---|
| **JsonMinifier** | `pipeline/reformats/json_minifier.rs` | Strips JSON whitespace via serde round-trip |
| **LogTemplate** | `pipeline/reformats/log_template.rs` | Drain-style template miner. Emits `[Template Tn: ...] (Nx)` with variant tables. Every original line is reconstructible. |

### Offload Transforms (lossy with CCR recovery)

These drop bytes from the wire but stash the originals in the CCR store. The LLM can retrieve dropped content via a tool call. Each offload exposes a domain-specific `estimate_bloat` method so the orchestrator can decide if the offload is worth the retrieval round-trip.

| Transform | Source | Description |
|---|---|---|
| **JsonOffload** | `pipeline/offloads/json_offload.rs` | Wraps SmartCrusher for JSON arrays. Estimator counts row separators; delegates to SmartCrusher for schema dedup, row sampling, anchor-aware selection. |
| **LogOffload** | `pipeline/offloads/log_offload.rs` | Wraps LogCompressor. Gates on per-line bloat heuristic. |
| **DiffOffload** | `pipeline/offloads/diff_offload.rs` | Wraps DiffCompressor. Gates on context-to-change ratio. |
| **DiffNoise** | `pipeline/offloads/diff_noise.rs` | Drops lockfile + whitespace-only hunks via CCR. Runs alongside DiffOffload for different shapes of diff bloat. |
| **SearchOffload** | `pipeline/offloads/search_offload.rs` | Exists but NOT in default pipeline (deprecated; modern agents use scoped `rg`/`grep`). |

**Deferred:** ProseFieldCompressor (Phase 3g PR3) for prose-shaped string fields inside structured payloads.

### Other Core Transforms

| Transform | Source | Description |
|---|---|---|
| **Live Zone Dispatcher** | `transforms/live_zone.rs` | Identifies which message blocks are safe to compress without busting provider prompt cache. Separate walkers for Anthropic, OpenAI Chat, and OpenAI Responses. Uses byte-range surgery to splice replacements without re-serializing unchanged bytes. |
| **Tag Protector** | `transforms/tag_protector.rs` | Protects and restores HTML-like tags during compression |
| **Adaptive Sizer** | `transforms/adaptive_sizer.rs` | Adjusts compression aggressiveness based on context budget |
| **Anchor Selector** | `transforms/anchor_selector.rs` | Selects representative items based on query anchors |
| **Content Detector** | `transforms/content_detector.rs` | Classifies content type (JSON, code, search, logs, diff, etc.) |
| **Magika Detector** | `transforms/magika_detector.rs` | Content type detection via Magika labels |
| **Unidiff Detector** | `transforms/unidiff_detector.rs` | Detects unified diff format |
| **Compression Policy** | `compression_policy.rs` | Policy engine for compression decisions |

### Compression Pipeline Orchestrator

**Source:** `crates/headroom-core/src/transforms/pipeline/orchestrator.rs`

The `CompressionPipeline` dispatches both reformat and offload transforms by content type. It runs the reformat phase serially while running per-offload bloat estimators in parallel via `rayon` so large inputs don't pay a sequential cost for the gating decision.

Key design rules:
- No regex (project convention). Uses `serde_json` round-trips and `aho-corasick` + ASCII word boundary for pattern matching.
- Bloat estimators must be cheap: under O(n) on input length, no allocations beyond the structural read.
- Estimators run in parallel with the reformat phase via `rayon::par_iter`.

---

## Live Zone Compression (Rust Proxy)

The live zone is the set of message blocks the model will respond to. Only these blocks can be mutated without busting the provider's prompt cache.

### Cache Safety Invariant

Bytes outside the live zone are never touched. The proxy uses byte-range surgery: it locates each rewritten block by pointer arithmetic on `serde_json::value::RawValue` borrowed slices, then splices the replacement:

```
out = body[..block_start] || replacement || body[block_end..]
```

Bytes outside rewritten ranges are literally copied from input, never re-serialized. SHA-256 of prefix and suffix are byte-identical to input.

### Per-Provider Compression

| Endpoint | Module | Status |
|---|---|---|
| `POST /v1/messages` | `compression/live_zone_anthropic.rs` | Active |
| `POST /v1/chat/completions` | `compression/live_zone_openai.rs` | Active |
| `POST /v1/responses` | `compression/live_zone_responses.rs` | Active |
| Google Gemini | Planned | Deferred |

---

## Cache & Storage Systems

### Semantic Cache

**Source:** `headroom/cache/semantic.py`

Embedding-based response cache. Computes query embeddings and searches for similar queries via cosine similarity. If similarity exceeds a threshold, returns the cached response. Complementary to provider prompt caching (which caches KV-cache for identical prefixes).

### Provider Cache Optimizers

**Source:** `headroom/cache/anthropic.py`, `headroom/cache/openai.py`, `headroom/cache/google.py`

Per-provider prompt cache alignment strategies.

### Compression Cache

**Source:** `headroom/cache/compression_cache.py`, `headroom/cache/compression_store.py`

Caches compression results to avoid re-compressing identical content. Includes compression feedback tracking.

### Cache Stabilization (Rust Proxy)

**Source:** `crates/headroom-proxy/src/cache_stabilization/`

| Module | Description |
|---|---|
| `drift_detector.rs` | Per-session structural-hash LRU. Detects when request structure changes across turns to avoid false cache hits. |
| `volatile_detector.rs` | Identifies volatile fields that should not affect cache keys |
| `tool_def_normalize.rs` | Normalizes tool definitions for stable cache keys |
| `anthropic_cache_control.rs` | Anthropic-specific cache control handling |
| `openai_cache_key.rs` | OpenAI-specific cache key derivation |

### CCR (Context Compression Retrieval)

**Source:** `headroom/ccr/` (Python), `crates/headroom-core/src/ccr/` (Rust)

Stores dropped content in a key-value store so the LLM can retrieve it via a tool call. The system injects a retrieval tool into the request, and a response handler intercepts tool calls to serve cached content.

Storage backends (Rust):
- In-memory (`in_memory.rs`)
- SQLite (`sqlite.rs`)
- Redis (`redis.rs`)

Python-side components:
- `tool_injection.py` / `CCRToolInjector`: injects the retrieval tool into requests
- `response_handler.py` / `CCRResponseHandler`: intercepts tool calls to serve cached content
- `batch_processor.py` / `batch_store.py`: batch processing for multiple CCR entries
- `context_tracker.py`: tracks context across turns
- `mcp_server.py`: exposes CCR as an MCP server

---

## Proxy Routing & Endpoints

### Rust Proxy Routes

| Route | Method | Handler | Description |
|---|---|---|---|
| `/healthz` | GET | `health::healthz` | Service health check |
| `/healthz/upstream` | GET | `health::healthz_upstream` | Upstream connectivity check |
| `/metrics` | GET | `observability::handle_metrics` | Prometheus scrape endpoint |
| `/v1/messages` | POST (via catch-all) | `handlers::anthropic` | Anthropic Messages API with live-zone compression |
| `/v1/chat/completions` | POST | `handlers::chat_completions` | OpenAI Chat Completions with live-zone compression |
| `/v1/responses` | POST | `handlers::responses` | OpenAI Responses with live-zone compression |
| `/v1beta1/projects/.../models/:action` | POST | `vertex::handle_vertex_predict_dispatch` | Google Vertex AI rawPredict / streamRawPredict |
| Bedrock InvokeModel / Converse | POST | `bedrock::invoke` | AWS Bedrock with SigV4 signing + live-zone compression |
| WebSocket upgrade | ANY | `websocket::ws_handler` | WebSocket pass-through |
| `*` (catch-all) | ANY | `proxy::forward_http` | Transparent reverse proxy to upstream |

### Python Proxy Features

| Feature | Source | Description |
|---|---|---|
| Rate Limiter | `proxy/rate_limiter.py` | Token bucket rate limiting (disableable) |
| Budget Enforcement | `proxy/cost.py` | Daily USD spending limits |
| Semantic Cache | `proxy/semantic_cache.py` | Query-level semantic caching layer |
| Image Compression | `proxy/image_compression_decision.py` | Gate for image compression decisions |
| Memory System | `proxy/memory_*.py` | Memory injection, query, ranking, tool adapter |
| Auth Mode | `proxy/auth_mode.py` | API key vs Copilot vs Bedrock classification |
| Probe Recorder | `proxy/probe_recorder.py` | Debugging and introspection |
| Loopback Guard | `proxy/loopback_guard.py` | Prevents proxy-to-self loops |
| Forwarded Headers | `proxy/forwarded_headers.py` | Header management for forwarded requests |
| SSL Context | `proxy/ssl_context.py` | TLS configuration |
| Stage Timer | `proxy/stage_timer.py` | Per-stage latency tracking |
| Warmup | `proxy/warmup.py` | Pre-loads models on startup |
| Savings Tracker | `proxy/savings_tracker.py` | Persistent savings data |
| Request Logger | `proxy/request_logger.py` | JSONL request/response logging |
| Prometheus Metrics | `proxy/prometheus_metrics.py` | Metrics exposition |
| Dashboard | `headroom/dashboard/` | Web UI for monitoring |
| WebSocket Registry | `proxy/ws_session_registry.py` | WebSocket session management |
| Project Context | `proxy/project_context.py` | Project-level context injection |

---

## Python Pipeline Lifecycle

**Source:** `headroom/pipeline.py`

The canonical pipeline emits events at each stage. Extensions can hook into any stage to mutate messages, tools, headers, or metadata.

```
SETUP -> PRE_START -> POST_START -> INPUT_RECEIVED -> INPUT_CACHED ->
INPUT_ROUTED -> INPUT_COMPRESSED -> INPUT_REMEMBERED -> PRE_SEND ->
POST_SEND -> RESPONSE_RECEIVED
```

Extensions are discovered via Python entry points (`headroom.pipeline_extension` group) or passed explicitly.

---

## Supported LLM Providers

| Provider | Protocol | Auth |
|---|---|---|
| **Anthropic** | `/v1/messages` | API key |
| **OpenAI** | `/v1/chat/completions`, `/v1/responses` | API key |
| **AWS Bedrock** | InvokeModel / Converse | SigV4 signing (resolved at startup via aws-config default chain) |
| **Google Vertex AI** | rawPredict / streamRawPredict | GCP ADC bearer token (gcloud creds, GCE metadata, service account, workload identity) |
| **Azure OpenAI** | OpenAI-compatible | API key |
| **OpenRouter** | OpenAI-compatible | API key |

---

## Relevance Scoring

**Source:** `headroom/relevance/` (Python), `crates/headroom-core/src/relevance/` (Rust)

Used by the context manager to rank message importance when deciding what to compress or drop.

| Scorer | Description |
|---|---|
| **BM25** | Term-frequency relevance scoring (`bm25.rs` / `bm25.py`) |
| **Embedding** | Semantic similarity via embeddings (`embedding.rs` / `embedding.py`) |
| **Hybrid** | Combines BM25 + embedding scores (`hybrid.rs` / `hybrid.py`) |

### Signals (Rust)

**Source:** `crates/headroom-core/src/signals/`

| Signal | Description |
|---|---|
| `keyword_detector.rs` | Keyword-based importance detection |
| `line_importance.rs` | Per-line importance scoring |
| `tiered.rs` | Tiered signal aggregation |

---

## Observability & Metrics

### Rust Proxy

**Source:** `crates/headroom-proxy/src/observability/`

| Module | Description |
|---|---|
| `prometheus.rs` | Prometheus metrics registry and exposition |
| `proxy_metrics.rs` | Per-request metrics collection |
| `cache_hit_rate.rs` | Cache hit rate tracking |
| `compression_ratio.rs` | Compression ratio tracking |
| `metric_names.rs` | Centralized metric name constants |

### Python Proxy

- **Prometheus metrics:** requests, tokens saved, compression ratios, latency, cache hits
- **OpenTelemetry tracing:** full tracing support via `headroom/observability/`
- **Savings tracker:** persistent savings in `~/.headroom/proxy_savings.json` (configurable via `HEADROOM_SAVINGS_PATH`)
- **Stats history:** hourly/daily/weekly/monthly rollups
- **Telemetry:** opt-out via `HEADROOM_TELEMETRY=off`

---

## SSE & Streaming

**Source:** `crates/headroom-proxy/src/sse/`

Per-provider SSE frame parsers for streaming responses:

| Module | Description |
|---|---|
| `framing.rs` | Generic SSE frame parser |
| `anthropic.rs` | Anthropic streaming format |
| `openai_chat.rs` | OpenAI Chat Completions streaming format |
| `openai_responses.rs` | OpenAI Responses streaming format |

---

## Cloud Provider Integrations (Rust Proxy)

### AWS Bedrock

**Source:** `crates/headroom-proxy/src/bedrock/`

| Module | Description |
|---|---|
| `sigv4.rs` | AWS SigV4 request signing |
| `auth_mode_layer.rs` | Bedrock-scoped auth mode middleware |
| `invoke.rs` / `invoke_streaming.rs` | InvokeModel handlers (sync + streaming) |
| `eventstream.rs` / `eventstream_to_sse.rs` | AWS EventStream to SSE conversion |
| `envelope.rs` | Bedrock request/response envelope handling |
| `vendor.rs` | Vendor-specific configuration |

### Google Vertex AI

**Source:** `crates/headroom-proxy/src/vertex/`

| Module | Description |
|---|---|
| `adc.rs` | Application Default Credentials token source (lazy resolution, cached with refresh-ahead-of-expiry) |
| `raw_predict.rs` | rawPredict handler |
| `stream_raw_predict.rs` | streamRawPredict handler |
| `envelope.rs` | Vertex request/response envelope handling |

---

## Tokenizers

**Source:** `crates/headroom-core/src/tokenizer/`

| Module | Description |
|---|---|
| `registry.rs` | Tokenizer selection by model name |
| `tiktoken_impl.rs` | OpenAI tiktoken tokenizer |
| `hf_impl.rs` | HuggingFace tokenizers |
| `estimator.rs` | Fast token count estimation without full tokenization |

Python side: `headroom/tokenizer.py`, `headroom/tokenizers/`

---

## Configuration

### Rust Proxy (`headroom-proxy`)

**Source:** `crates/headroom-proxy/src/config.rs`

Key settings: listen address, upstream URL, timeouts, max body size, host rewriting, graceful shutdown, Bedrock region/profile, Bedrock native toggle, log level.

### Rust Core Pipeline (`headroom-core`)

**Source:** `crates/headroom-core/src/transforms/pipeline/config.rs`

Configurable per-transform: bloat thresholds for JSON/log/diff/search offloads, diff noise settings, log template settings, orchestrator-level settings.

### Python Proxy

Key CLI flags:
- `--host`, `--port`: bind configuration
- `--no-optimize`, `--no-cache`, `--no-rate-limit`: feature toggles
- `--log-file`: JSONL output path
- `--budget`: daily USD spending limit
- `--openai-api-url`: custom upstream endpoint
- `--no-intelligent-context`: fall back to RollingWindow
- `--no-intelligent-scoring`: faster, less sophisticated scoring
- `--no-compress-first`: skip deeper compression before dropping
- `--llmlingua`: enable ML compression
- `--llmlingua-device`: auto/cuda/cpu/mps
- `--llmlingua-rate`: target compression ratio

---

## Additional Python Subsystems

| Directory | Description |
|---|---|
| `headroom/backends/` | Provider backend implementations |
| `headroom/graph/` | Dependency graph for compression planning |
| `headroom/learn/` | Learning/training utilities |
| `headroom/memory/` | Long-term memory system |
| `headroom/prediction/` | Prediction utilities |
| `headroom/pricing/` | Model pricing data |
| `headroom/providers/` | Provider registry and API target resolution |
| `headroom/reporting/` | Usage reporting |
| `headroom/storage/` | Storage backend abstraction |
| `headroom/subscription/` | Subscription/license management |
| `headroom/integrations/` | Third-party integrations |
| `headroom/mcp_registry/` | MCP server registry |
| `headroom/evals/` | Evaluation framework |
| `headroom/capture/` | Request capture for replay/debugging |
| `headroom/install/` | Installation utilities |
| `headroom/perf/` | Performance profiling |
| `headroom/rtk/` | RTK (Runtime Toolkit) integration |
| `headroom/lean_ctx/` | Lean context tool integration |

---

## Build & Deployment

- **Python:** `pip install headroom-ai` (with optional extras: `[ml]`, `[code]`, `[image]`)
- **Rust:** `cargo build --release` for the proxy binary; `maturin build` for the PyO3 extension
- **Docker:** Multi-stage Dockerfile with Python 3.11, Rust compilation, and C++ (for hnswlib)
- **Release profile:** `strip = "symbols"`, `lto = "thin"`, `codegen-units = 1` for minimal wheel size
- **CI profile:** `lto = false`, `codegen-units = 256`, `opt-level = 1` for fast test builds

---

## goheadroom Feature Gap Analysis

goheadroom is a **Go port of headroom-core** (the compression library). It is not a proxy server. It provides `CompressLiveZone()` as a library function for embedding into Go services (e.g. genai-api).

**Legend:** ✅ Ported | ⭐ Go-exclusive (not in upstream) | ❌ Missing | ⬜ N/A (proxy-level, out of scope for a library)

### Compressors

Each compressor targets a specific content type and applies domain-aware compression to reduce token count while preserving the information the LLM needs.

| Feature | Status | goheadroom | Rust Upstream | What It Does |
|---|---|---|---|---|
| Smart Crusher (JSON arrays) | ✅ | `core/transforms/smartcrusher/` | [`smart_crusher/`](https://github.com/chopratejas/headroom/tree/main/crates/headroom-core/src/transforms/smart_crusher) | Compresses JSON arrays of objects (tool outputs, API responses). Deduplicates schemas across rows, samples representative items, preserves error/outlier rows and anchor-matched items. Uses statistical analysis to decide what to keep. |
| Diff Compressor (git diffs) | ✅ | `core/transforms/diffcompressor/` | [`diff_compressor.rs`](https://github.com/chopratejas/headroom/blob/main/crates/headroom-core/src/transforms/diff_compressor.rs) | Compresses unified diffs by trimming context lines around changes, dropping low-value hunks (whitespace-only, lockfile noise), and preserving the actual changed lines. |
| Log Compressor (build output) | ✅ | `core/transforms/logcompressor/` | [`log_compressor.rs`](https://github.com/chopratejas/headroom/blob/main/crates/headroom-core/src/transforms/log_compressor.rs) | Compresses build logs and CI output. Detects log levels, deduplicates repeated lines, and always preserves error/warning lines so failures are never lost. |
| Search Compressor (grep results) | ✅ | `core/transforms/searchcompressor/` | [`search_compressor.rs`](https://github.com/chopratejas/headroom/blob/main/crates/headroom-core/src/transforms/search_compressor.rs) | Compresses grep/ripgrep search results by clustering matches across files, sampling representative matches per file, and preserving match context. |
| Code-Aware Compressor (AST-based) | ✅ | `core/transforms/codecompressor/` | Python only: [`code_compressor.py`](https://github.com/chopratejas/headroom/blob/main/headroom/transforms/code_compressor.py) | Parses source code into an AST via tree-sitter. Keeps imports, function signatures, type annotations, and error handlers intact while eliding function bodies. Output is always syntactically valid. Supports Python, JS, TS, Go, Rust, Java, C, C++. |
| Kompress (ModernBERT ML) | ✅ | `core/transforms/kompress/` | Python only: [`kompress_compressor.py`](https://github.com/chopratejas/headroom/blob/main/headroom/transforms/kompress_compressor.py) | ML-based token compressor using a fine-tuned ModernBERT model (`kompress-v2-base`). Runs ONNX inference to classify each token as keep/drop. Works on plain text and prose-shaped content where structural compressors don't apply. |
| JSON Compressor | ⭐ | `core/transforms/jsoncompressor/` | No upstream equivalent | Go-exclusive. Compresses JSON objects and arrays by preserving key structure while truncating long string values. Has its own parity fixtures. Useful for large JSON blobs that aren't arrays-of-dicts (which Smart Crusher handles). |
| LLMLingua-2 (torch ML) | ❌ | -- | Alternative ML-based text compressor using torch + transformers. Configurable compression rate (e.g. keep 30%). A different approach than Kompress -- uses attention-based token importance rather than a classification head. |
| Image Compressor (OCR + tiles) | ❌ | -- | Detects images in LLM messages, extracts text via OCR (RapidOCR), routes to optimal compression technique via a trained model, and applies provider-specific tile optimization to minimize image tokens. |
| Content Router (mixed-content orchestrator) | ❌ | -- | Analyzes content that mixes multiple types (e.g. a message with code blocks, JSON, and prose). Splits it into sections, routes each section to the optimal compressor, and reassembles. Higher-level than the pipeline orchestrator. |
| HTML Extractor | ❌ | -- | Strips HTML markup to extract meaningful text content. Useful when tool outputs return HTML pages that waste tokens on tags and boilerplate. |

### Live Zone Compression

The live zone is the set of message blocks the LLM will respond to. Only these blocks can be modified without busting the provider's prompt cache. The live-zone compressor identifies the safe-to-compress region (bounded by cache_control markers and the latest user message), applies per-block compression, and splices replacements via byte-range surgery so bytes outside the zone are never touched.

| Feature | Status | goheadroom | Rust Upstream | What It Does |
|---|---|---|---|---|
| Anthropic `/v1/messages` | ✅ | `core/transforms/livezone/anthropic.go` | [`live_zone.rs`](https://github.com/chopratejas/headroom/blob/main/crates/headroom-core/src/transforms/live_zone.rs) | Walks Anthropic message arrays, identifies the live zone (between frozen count floor and latest user message), and compresses tool_result and text blocks within it. |
| Anthropic with CCR | ✅ | `core/transforms/livezone/anthropic.go` | [`live_zone.rs`](https://github.com/chopratejas/headroom/blob/main/crates/headroom-core/src/transforms/live_zone.rs) | Same as above but stores dropped content in CCR and injects retrieval markers so the LLM can fetch it back. |
| OpenAI Chat `/v1/chat/completions` | ✅ | `core/transforms/livezone/openai_chat.go` | [`live_zone_openai.rs`](https://github.com/chopratejas/headroom/blob/main/crates/headroom-proxy/src/compression/live_zone_openai.rs) | Walks OpenAI chat messages (role: tool messages are standalone, not nested). Identifies and compresses the live zone. |
| OpenAI Responses `/v1/responses` | ✅ | `core/transforms/livezone/openai_responses.go` | [`live_zone_responses.rs`](https://github.com/chopratejas/headroom/blob/main/crates/headroom-proxy/src/compression/live_zone_responses.rs) | Walks OpenAI Responses format (input items with function_call_output, reasoning, etc.). Different structure than Chat but same compression principles. |
| Live Zone types + outcome | ✅ | `core/transforms/livezone/types.go` | [`live_zone.rs`](https://github.com/chopratejas/headroom/blob/main/crates/headroom-core/src/transforms/live_zone.rs) | Shared types: LiveZoneOutcome, BlockAction, CompressionManifest, ExclusionReason. Used by all provider-specific walkers. |

### Pipeline Orchestrator

The pipeline dispatches reformat and offload transforms by content type. It runs reformats serially (they're lossless and cheap) and runs offload bloat estimators in parallel so large inputs don't pay sequential cost for the gating decision.

| Feature | Status | goheadroom | Rust Upstream | What It Does |
|---|---|---|---|---|
| Pipeline Orchestrator | ✅ | `core/transforms/pipeline/orchestrator.go` | [`orchestrator.rs`](https://github.com/chopratejas/headroom/blob/main/crates/headroom-core/src/transforms/pipeline/orchestrator.rs) | Main dispatcher. Detects content type, runs applicable reformats, estimates bloat per offload, and applies offloads that pass the threshold. |
| Pipeline Config (TOML) | ✅ | `core/transforms/pipeline/config.go` | [`config.rs`](https://github.com/chopratejas/headroom/blob/main/crates/headroom-core/src/transforms/pipeline/config.rs) | Configurable per-transform thresholds: bloat thresholds for JSON/log/diff/search offloads, diff noise settings, log template settings. Loaded from `pipeline.toml`. |
| Pipeline Traits (interfaces) | ✅ | `core/transforms/pipeline/traits.go` | [`traits.rs`](https://github.com/chopratejas/headroom/blob/main/crates/headroom-core/src/transforms/pipeline/traits.rs) | Defines the ReformatTransform and OffloadTransform interfaces that all pipeline transforms implement. |

### Reformat Transforms (lossless)

Reformats pack content denser without dropping any information. The output is semantically equivalent to the input, just smaller.

| Feature | Status | goheadroom | Rust Upstream | What It Does |
|---|---|---|---|---|
| JsonMinifier | ✅ | `core/transforms/pipeline/reformats/json_minifier.go` | [`json_minifier.rs`](https://github.com/chopratejas/headroom/blob/main/crates/headroom-core/src/transforms/pipeline/reformats/json_minifier.rs) | Strips all whitespace from JSON via parse-and-reserialize. `{"key": "value"}` becomes `{"key":"value"}`. Saves tokens on pretty-printed tool outputs. |
| LogTemplate (Drain-style) | ✅ | `core/transforms/pipeline/reformats/log_template.go` | [`log_template.rs`](https://github.com/chopratejas/headroom/blob/main/crates/headroom-core/src/transforms/pipeline/reformats/log_template.rs) | Mines repeated log patterns using the Drain algorithm. Replaces 1000 similar lines with `[Template T1: Connection from * on port *] (1000x)` plus a variant table. Fully reversible. |

### Offload Transforms (CCR-backed)

Offloads drop bytes from the wire but stash the original content in the CCR store. A retrieval marker replaces the dropped content so the LLM can fetch it back via a tool call if needed. Each offload has a domain-specific bloat estimator that decides whether the offload is worth the retrieval round-trip.

| Feature | Status | goheadroom | Rust Upstream | What It Does |
|---|---|---|---|---|
| DiffOffload | ✅ | `core/transforms/pipeline/offloads/diff_offload.go` | [`diff_offload.rs`](https://github.com/chopratejas/headroom/blob/main/crates/headroom-core/src/transforms/pipeline/offloads/diff_offload.rs) | Wraps DiffCompressor as a pipeline offload. Gates on context-to-change ratio -- only offloads when diffs are bloated with context lines relative to actual changes. |
| DiffNoise | ✅ | `core/transforms/pipeline/offloads/diff_noise.go` | [`diff_noise.rs`](https://github.com/chopratejas/headroom/blob/main/crates/headroom-core/src/transforms/pipeline/offloads/diff_noise.rs) | Drops lockfile hunks and whitespace-only changes via CCR. Runs alongside DiffOffload to catch a different shape of diff bloat. |
| JsonOffload (SmartCrusher) | ✅ | `core/transforms/pipeline/offloads/json_offload.go` | [`json_offload.rs`](https://github.com/chopratejas/headroom/blob/main/crates/headroom-core/src/transforms/pipeline/offloads/json_offload.rs) | Wraps SmartCrusher as a pipeline offload. Estimator counts row separators to gauge bloat; delegates to SmartCrusher for schema dedup, row sampling, and anchor-aware selection. |
| LogOffload | ✅ | `core/transforms/pipeline/offloads/log_offload.go` | [`log_offload.rs`](https://github.com/chopratejas/headroom/blob/main/crates/headroom-core/src/transforms/pipeline/offloads/log_offload.rs) | Wraps LogCompressor as a pipeline offload. Gates on per-line bloat heuristic -- only offloads when log output has high repetition or low-priority lines. |
| SearchOffload | ✅ | `core/transforms/pipeline/offloads/search_offload.go` | [`search_offload.rs`](https://github.com/chopratejas/headroom/blob/main/crates/headroom-core/src/transforms/pipeline/offloads/search_offload.rs) | Wraps SearchCompressor as a pipeline offload. Gates on how matches cluster across files. |
| CodeOffload | ⭐ | `core/transforms/pipeline/offloads/code_offload.go` | No upstream equivalent | Go-exclusive. Wraps CodeCompressor as a pipeline offload. Stashes elided function bodies in CCR while keeping signatures in the wire. Not present in upstream Rust or Python pipeline. |
| JsonStructureOffload | ⭐ | `core/transforms/pipeline/offloads/json_structure_offload.go` | No upstream equivalent | Go-exclusive. Wraps JsonCompressor as a pipeline offload for JSON objects/arrays. Preserves keys and structure while eliding long string values via CCR. Not in upstream. |

### Core Transforms & Detection

Supporting transforms used by compressors and the pipeline orchestrator. These handle content classification, safety checks, and pre/post-processing.

| Feature | Status | goheadroom | Rust Upstream | What It Does |
|---|---|---|---|---|
| Content Detector | ✅ | `core/transforms/contentdetector/` | [`content_detector.rs`](https://github.com/chopratejas/headroom/blob/main/crates/headroom-core/src/transforms/content_detector.rs) | Classifies raw text into content types (JSON array, source code, search results, build log, git diff, plain text, HTML) using heuristic pattern matching. Drives routing decisions. |
| Magika Detector (ONNX) | ✅ | `core/transforms/contentdetector/magika.go` | [`magika_detector.rs`](https://github.com/chopratejas/headroom/blob/main/crates/headroom-core/src/transforms/magika_detector.rs) | Google's Magika ML model for content type detection. Runs ONNX inference for higher-accuracy classification than heuristics alone. Used as a fallback/tiebreaker. |
| Unidiff Detector | ✅ | `core/transforms/unidiff.go` | [`unidiff_detector.rs`](https://github.com/chopratejas/headroom/blob/main/crates/headroom-core/src/transforms/unidiff_detector.rs) | Fast check for whether content is a unified diff format. Looks for `---`/`+++`/`@@` markers. |
| Content Detection (top-level) | ✅ | `core/transforms/detection.go` | [`detection.rs`](https://github.com/chopratejas/headroom/blob/main/crates/headroom-core/src/transforms/detection.rs) | Top-level detection API that combines heuristic and ML detectors. Entry point for content type classification. |
| Tag Protector | ✅ | `core/transforms/tagprotector.go` | [`tag_protector.rs`](https://github.com/chopratejas/headroom/blob/main/crates/headroom-core/src/transforms/tag_protector.rs) | Protects HTML/XML-like tags (e.g. `<tool_result>`, `<thinking>`) from being mangled during compression by replacing them with placeholders, then restoring them after. |
| Adaptive Sizer | ✅ | `core/transforms/adaptivesizer/` | [`adaptive_sizer.rs`](https://github.com/chopratejas/headroom/blob/main/crates/headroom-core/src/transforms/adaptive_sizer.rs) | Adjusts compression aggressiveness based on remaining token budget. When the context is nearly full, compresses harder; when there's headroom, compresses less. |
| Anchor Selector | ✅ | `core/transforms/anchorselector/` | [`anchor_selector.rs`](https://github.com/chopratejas/headroom/blob/main/crates/headroom-core/src/transforms/anchor_selector.rs) | Selects which items to preserve based on the user's query. Extracts query anchors (keywords, patterns) and matches them against candidate items to keep the most relevant ones. |
| Safety (tool pairs) | ✅ | `core/transforms/safety.go` | [`safety.rs`](https://github.com/chopratejas/headroom/blob/main/crates/headroom-core/src/transforms/safety.rs) | Ensures tool call/result pairs stay together during compression. If a tool_use block is kept, its corresponding tool_result must also be kept (and vice versa), or the LLM will hallucinate. |
| Recommendations | ✅ | `core/transforms/recommendations.go` | [`recommendations.rs`](https://github.com/chopratejas/headroom/blob/main/crates/headroom-core/src/transforms/recommendations.rs) | Loads and serves compression recommendations from a data file. Provides per-content-type guidance on optimal compression strategies. |
| Compression Summary | ❌ | -- | Generates human-readable summaries of what was compressed and by how much. Useful for debugging and user-facing transparency. |
| Read Lifecycle | ❌ | -- | Manages the lifecycle of read operations during compression -- tracks which messages have been read/processed and prevents double-processing. |
| Compression Units | ❌ | -- | Abstracts compression into discrete units (message, block, field) so the pipeline can reason about compression at different granularities. |

### CCR (Context Compression Retrieval)

CCR is the mechanism that makes offload transforms lossless. When content is dropped from the wire, it's stored in a key-value store. A retrieval marker replaces it inline. The LLM can call a tool to fetch the original content if it needs it. This turns "lossy compression" into "lazy loading."

| Feature | Status | goheadroom | Rust Upstream | What It Does |
|---|---|---|---|---|
| CCR Store interface | ✅ | `core/ccr/ccr.go` | [`ccr/mod.rs`](https://github.com/chopratejas/headroom/blob/main/crates/headroom-core/src/ccr/mod.rs) | Defines the CcrStore interface: Store(key, content), Retrieve(key), Delete(key). All backends implement this. |
| CCR Store (in-memory) | ✅ | `core/ccr/inmemory.go` | [`in_memory.rs`](https://github.com/chopratejas/headroom/blob/main/crates/headroom-core/src/ccr/backends/in_memory.rs) | Hash-map-backed store for single-process use. Fast, no external dependencies. Content is lost on process restart. |
| CCR Store (SQLite) | ✅ | `core/ccr/sqlite.go` | [`sqlite.rs`](https://github.com/chopratejas/headroom/blob/main/crates/headroom-core/src/ccr/backends/sqlite.rs) | SQLite-backed store for persistent single-node use. Survives restarts. Good for local development and single-server deployments. |
| CCR Store (Redis) | ✅ | `core/ccr/redis.go` | [`redis.rs`](https://github.com/chopratejas/headroom/blob/main/crates/headroom-core/src/ccr/backends/redis.rs) | Redis-backed store for distributed deployments. Multiple proxy instances can share the same CCR store. Supports TTL-based expiration. |
| CCR Config | ✅ | `core/ccr/config.go` | [`ccr/mod.rs`](https://github.com/chopratejas/headroom/blob/main/crates/headroom-core/src/ccr/mod.rs) | Configuration for CCR behavior: which backend to use, TTL, max entry size, marker format. |
| CCR Tool Injection | ❌ | -- | Injects a retrieval tool definition into the LLM request's tools array so the model knows it can call `headroom_retrieve(key)` to fetch dropped content. Without this, the LLM sees markers but has no way to act on them. |
| CCR Response Handler | ❌ | -- | Intercepts the LLM's response stream. When the model calls the retrieval tool, this handler serves the content from the CCR store instead of forwarding to user code. Closes the retrieval loop. |
| CCR Batch Processor | ❌ | -- | Processes multiple CCR store/retrieve operations in a single batch for efficiency. Reduces round-trips when many blocks are offloaded in one request. |
| CCR Context Tracker | ❌ | -- | Tracks which content has been offloaded across conversation turns. Prevents re-offloading content the LLM already retrieved, and ages out stale entries. |
| CCR MCP Server | ❌ | -- | Exposes the CCR store as an MCP (Model Context Protocol) server so external tools and agents can retrieve offloaded content. |

### Tokenizers

Tokenizers count tokens accurately per model so compression can make precise budget decisions. Different models use different tokenization schemes, so the library needs multiple backends.

| Feature | Status | goheadroom | Rust Upstream | What It Does |
|---|---|---|---|---|
| Tokenizer interface | ✅ | `core/tokenizer/tokenizer.go` | [`tokenizer/mod.rs`](https://github.com/chopratejas/headroom/blob/main/crates/headroom-core/src/tokenizer/mod.rs) | Common interface: Encode(text) returns token IDs, CountTokens(text) returns count. All backends implement this. |
| tiktoken | ✅ | `core/tokenizer/tiktoken.go` | [`tiktoken_impl.rs`](https://github.com/chopratejas/headroom/blob/main/crates/headroom-core/src/tokenizer/tiktoken_impl.rs) | Pure-Go implementation of OpenAI's tiktoken BPE tokenizer. Handles GPT-4, GPT-4o, o1, o3, and other OpenAI models. |
| tiktoken FFI (C binding) | ✅ | `core/tokenizer/tiktoken_ffi.go` | No Rust equivalent (Rust uses tiktoken natively) | Optional C FFI binding to the tiktoken-rs library for faster tokenization. Falls back to pure Go when the C library isn't available. |
| HuggingFace tokenizers | ✅ | `core/tokenizer/hf.go` | [`hf_impl.rs`](https://github.com/chopratejas/headroom/blob/main/crates/headroom-core/src/tokenizer/hf_impl.rs) | Wraps HuggingFace tokenizer models for Claude, Llama, Mistral, and other non-OpenAI models. |
| Token estimator | ✅ | `core/tokenizer/estimator.go` | [`estimator.rs`](https://github.com/chopratejas/headroom/blob/main/crates/headroom-core/src/tokenizer/estimator.rs) | Fast approximate token count without full tokenization. Uses character-to-token ratio heuristics. For when speed matters more than precision (e.g. bloat estimation). |
| Registry (model lookup) | ✅ | `core/tokenizer/registry.go` | [`registry.rs`](https://github.com/chopratejas/headroom/blob/main/crates/headroom-core/src/tokenizer/registry.rs) | Maps model names (e.g. "claude-sonnet-4-5-20250929", "gpt-4o") to the correct tokenizer backend. Handles aliases and version suffixes. |

### Relevance Scoring

Relevance scoring ranks message importance so the compressor knows what to keep and what to compress harder. Used by the context manager when deciding which messages to target.

| Feature | Status | goheadroom | Rust Upstream | What It Does |
|---|---|---|---|---|
| BM25 | ✅ | `core/relevance/bm25.go` | [`bm25.rs`](https://github.com/chopratejas/headroom/blob/main/crates/headroom-core/src/relevance/bm25.rs) | Classic term-frequency/inverse-document-frequency scoring. Fast, no ML needed. Ranks messages by keyword overlap with the user's query. |
| Embedding (ONNX) | ✅ | `core/relevance/embedding.go` | [`embedding.rs`](https://github.com/chopratejas/headroom/blob/main/crates/headroom-core/src/relevance/embedding.rs) | Computes semantic similarity between messages and the query using embedding vectors. Runs a small ONNX model. Catches conceptual relevance that keyword matching misses. |
| Hybrid (BM25 + Embedding) | ✅ | `core/relevance/hybrid.go` | [`hybrid.rs`](https://github.com/chopratejas/headroom/blob/main/crates/headroom-core/src/relevance/hybrid.rs) | Combines BM25 and embedding scores with configurable weights. Gets the best of both: keyword precision and semantic recall. |
| Cosine similarity | ✅ | `core/relevance/cosine.go` | [`base.rs`](https://github.com/chopratejas/headroom/blob/main/crates/headroom-core/src/relevance/base.rs) | Computes cosine similarity between embedding vectors. The math behind embedding-based relevance scoring. |

### Signals

Signals provide per-line importance scoring used by compressors to decide which lines to keep. They detect error indicators, important keywords, and structural significance.

| Feature | Status | goheadroom | Rust Upstream | What It Does |
|---|---|---|---|---|
| Keyword detector | ✅ | `core/signals/keyword.go` | [`keyword_detector.rs`](https://github.com/chopratejas/headroom/blob/main/crates/headroom-core/src/signals/keyword_detector.rs) | Detects important keywords in text using Aho-Corasick multi-pattern matching with ASCII word boundaries. Identifies error indicators, function names, variable references. |
| Line importance | ✅ | `core/signals/lineimportance.go` | [`line_importance.rs`](https://github.com/chopratejas/headroom/blob/main/crates/headroom-core/src/signals/line_importance.rs) | Scores individual lines by structural importance: error lines, stack traces, assertion failures, and key output markers get high scores. Used by LogCompressor and DiffCompressor. |
| Tiered signals | ✅ | `core/signals/tiered.go` | [`tiered.rs`](https://github.com/chopratejas/headroom/blob/main/crates/headroom-core/src/signals/tiered.rs) | Aggregates multiple signal sources into tiered importance levels. Combines keyword hits, line importance, and other signals into a final priority ranking. |

### Core Infrastructure

Foundational components that support compression decisions and testing.

| Feature | Status | goheadroom | Rust Upstream | What It Does |
|---|---|---|---|---|
| Cache Control | ✅ | `core/cachecontrol/` | [`cache_control.rs`](https://github.com/chopratejas/headroom/blob/main/crates/headroom-core/src/cache_control.rs) | Computes the frozen message count from explicit `cache_control` markers in requests. Determines the floor of the live zone -- messages below this index are in the provider's prompt cache and must not be modified. |
| Auth Mode | ✅ | `core/authmode/` | [`auth_mode.rs`](https://github.com/chopratejas/headroom/blob/main/crates/headroom-core/src/auth_mode.rs) | Classifies how the client authenticated: API key, GitHub Copilot token, or Bedrock IAM. Different auth modes have different compression policies (e.g. Copilot sessions get more aggressive compression). |
| Compression Policy | ✅ | `core/compressionpolicy/` | [`compression_policy.rs`](https://github.com/chopratejas/headroom/blob/main/crates/headroom-core/src/compression_policy.rs) | Policy engine that decides whether to compress a request and how aggressively. Takes auth mode, model, token count, and configuration into account. |
| Parity Test Harness | ✅ | `cmd/parity-check/` | [`headroom-parity/`](https://github.com/chopratejas/headroom/tree/main/crates/headroom-parity) | Runs goheadroom transforms against the same fixtures used by upstream Python/Rust and verifies byte-identical output. Ensures the Go port hasn't diverged. |
| Benchmark Harness | ✅ | `cmd/bench/` | [`benches/`](https://github.com/chopratejas/headroom/tree/main/crates/headroom-core/benches) | Benchmarks compression throughput and latency across all transforms. Measures tokens/sec, compression ratios, and memory usage. |

### Cache Stabilization

Cache stabilization prevents the compressor from accidentally busting the provider's prompt cache. LLM providers (Anthropic, OpenAI) cache the KV-cache for repeated prefixes, saving significant cost. If the compressor changes bytes that should be stable, the cache misses and the provider recomputes from scratch.

| Feature | Status | goheadroom Package | What It Does |
|---|---|---|---|
| Drift Detector | ❌ | -- | Per-session structural-hash LRU. Tracks the structure of each request across turns and detects when the structure changes unexpectedly. Prevents false cache hits when a session's tool definitions or message shape shifts. |
| Volatile Detector | ❌ | -- | Identifies fields that change every request (timestamps, request IDs, nonces) and excludes them from cache key computation. Without this, every request looks unique and the cache never hits. |
| Tool Definition Normalizer | ❌ | -- | Normalizes tool definitions (sorts keys, strips whitespace, canonicalizes schemas) so semantically identical tool definitions produce the same cache key even if formatted differently. |
| Anthropic Cache Control Stabilizer | ❌ | -- | Anthropic-specific logic for placing `cache_control` markers optimally. Ensures markers land at natural breakpoints so the cached prefix is maximally reusable across turns. |
| OpenAI Cache Key Stabilizer | ❌ | -- | OpenAI-specific cache key derivation. OpenAI's caching is implicit (prefix-based), so this ensures the prefix bytes are stable across turns by controlling serialization order and whitespace. |

### Caching Subsystems

Response-level and compression-level caching to avoid redundant work. These are distinct from the provider's built-in prompt cache (which caches KV-cache for identical prefixes).

| Feature | Status | goheadroom Package | What It Does |
|---|---|---|---|
| Semantic Cache | ❌ | -- | Caches complete LLM responses keyed by embedding similarity. When a new query is semantically similar to a previous one (cosine similarity > threshold), returns the cached response without calling the LLM. Saves entire API calls, not just tokens. |
| Provider Cache Optimizers | ❌ | -- | Per-provider strategies for maximizing prompt cache hit rates. Anthropic: optimal `cache_control` marker placement. OpenAI: stable prefix serialization. Google: equivalent for Gemini's caching model. |
| Compression Cache | ❌ | -- | Caches the output of compression transforms keyed by content hash. If the same tool output appears in multiple turns, the compressor returns the cached result instead of re-running SmartCrusher/DiffCompressor/etc. |
| Compression Feedback | ❌ | -- | Tracks whether compressed content led to good LLM responses. If compression was too aggressive and the LLM asked for clarification, future compression of similar content backs off. |
| Dynamic Detector | ❌ | -- | Detects content that changes every request (timestamps, counters, session IDs) and marks it so the cache doesn't treat each request as unique. |
| Prefix Tracker | ❌ | -- | Tracks the stable prefix of the conversation across turns. Knows which bytes have been sent before and can skip re-processing them. |
| Cache Registry | ❌ | -- | Registry of available cache backends (in-memory, disk, Redis). Manages lifecycle and provides a uniform interface. |

### Proxy Server & HTTP Layer

These features make up the standalone reverse proxy server. goheadroom is a library (not a server), so these are out of scope -- the consuming service (e.g. genai-api) provides its own HTTP layer.

| Feature | Status | Upstream Location | What It Does |
|---|---|---|---|
| HTTP reverse proxy server | ⬜ | [Rust: `headroom-proxy`](https://github.com/chopratejas/headroom/tree/main/crates/headroom-proxy), [Python: `server.py`](https://github.com/chopratejas/headroom/blob/main/headroom/proxy/server.py) | Standalone server that sits between client and LLM, intercepting and compressing requests transparently. |
| HTTP routing (axum / FastAPI) | ⬜ | [Rust: `proxy.rs`](https://github.com/chopratejas/headroom/blob/main/crates/headroom-proxy/src/proxy.rs), [Python: `server.py`](https://github.com/chopratejas/headroom/blob/main/headroom/proxy/server.py) | Routes requests to the correct handler based on path (`/v1/messages`, `/v1/chat/completions`, etc.). |
| SSE frame parsing (Anthropic, OpenAI) | ⬜ | [Rust: `sse/`](https://github.com/chopratejas/headroom/tree/main/crates/headroom-proxy/src/sse) | Parses Server-Sent Events frames from streaming LLM responses. Each provider has a different SSE format. |
| WebSocket pass-through | ⬜ | [Rust: `websocket.rs`](https://github.com/chopratejas/headroom/blob/main/crates/headroom-proxy/src/websocket.rs) | Transparently proxies WebSocket connections (used by some LLM clients for bidirectional streaming). |
| Prometheus metrics endpoint | ⬜ | [Rust: `prometheus.rs`](https://github.com/chopratejas/headroom/blob/main/crates/headroom-proxy/src/observability/prometheus.rs) | Serves `/metrics` in Prometheus text format for scraping by monitoring infrastructure. |
| Health check endpoints | ⬜ | [Rust: `health.rs`](https://github.com/chopratejas/headroom/blob/main/crates/headroom-proxy/src/health.rs) | Serves `/healthz` and `/healthz/upstream` for load balancer health checks and upstream connectivity verification. |
| Graceful shutdown | ⬜ | [Rust: `main.rs`](https://github.com/chopratejas/headroom/blob/main/crates/headroom-proxy/src/main.rs) | Drains in-flight requests on SIGTERM/SIGINT before exiting. Prevents dropped connections during deploys. |
| Request forwarding / headers | ⬜ | [Rust: `headers.rs`](https://github.com/chopratejas/headroom/blob/main/crates/headroom-proxy/src/headers.rs), [Python: `forwarded_headers.py`](https://github.com/chopratejas/headroom/blob/main/headroom/proxy/forwarded_headers.py) | Manages X-Forwarded-For, X-Request-ID, and other proxy headers. Strips internal headers before forwarding upstream. |

### Cloud Provider Integrations

Provider-specific auth and protocol handling for direct LLM access (not via API key). These are proxy-level features.

| Feature | Status | Upstream Location | What It Does |
|---|---|---|---|
| AWS Bedrock SigV4 signing | ⬜ | [Rust: `sigv4.rs`](https://github.com/chopratejas/headroom/blob/main/crates/headroom-proxy/src/bedrock/sigv4.rs) | Signs requests with AWS SigV4 so the proxy can forward to Bedrock without the client needing AWS credentials. |
| Bedrock InvokeModel handlers | ⬜ | [Rust: `invoke.rs`](https://github.com/chopratejas/headroom/blob/main/crates/headroom-proxy/src/bedrock/invoke.rs) | Handles Bedrock's InvokeModel and Converse API paths. Applies live-zone compression, then signs and forwards. |
| Bedrock EventStream to SSE | ⬜ | [Rust: `eventstream.rs`](https://github.com/chopratejas/headroom/blob/main/crates/headroom-proxy/src/bedrock/eventstream.rs) | Converts AWS EventStream binary framing (Bedrock's streaming format) into standard SSE so clients don't need AWS SDK. |
| Bedrock auth mode layer | ⬜ | [Rust: `auth_mode_layer.rs`](https://github.com/chopratejas/headroom/blob/main/crates/headroom-proxy/src/bedrock/auth_mode_layer.rs) | Scoped middleware that classifies Bedrock requests by IAM identity for per-identity compression policies. |
| Google Vertex ADC token source | ⬜ | [Rust: `adc.rs`](https://github.com/chopratejas/headroom/blob/main/crates/headroom-proxy/src/vertex/adc.rs) | Resolves GCP Application Default Credentials (gcloud user creds, GCE metadata, service account JSON, workload identity) for bearer token auth to Vertex AI. |
| Vertex rawPredict / streamRawPredict | ⬜ | [Rust: `raw_predict.rs`](https://github.com/chopratejas/headroom/blob/main/crates/headroom-proxy/src/vertex/raw_predict.rs) | Handles Vertex AI's rawPredict and streamRawPredict endpoints. Routes Anthropic-shaped bodies through Vertex's publisher path. |

### Proxy Middleware & Features

Application-level middleware that runs in the proxy server. Not applicable to a library.

| Feature | Status | Upstream Location | What It Does |
|---|---|---|---|
| Rate Limiter | ⬜ | [Python: `rate_limiter.py`](https://github.com/chopratejas/headroom/blob/main/headroom/proxy/rate_limiter.py) | Token bucket rate limiter. Prevents any single client from overwhelming the upstream LLM provider. Disableable via `--no-rate-limit`. |
| Budget Enforcement | ⬜ | [Python: `cost.py`](https://github.com/chopratejas/headroom/blob/main/headroom/proxy/cost.py) | Enforces daily USD spending limits. Rejects requests that would exceed the budget. Tracks cost using model pricing data. |
| Memory System | ⬜ | [Python: `proxy/memory_*.py`](https://github.com/chopratejas/headroom/tree/main/headroom/proxy) | Long-term memory across conversations. Injects relevant memories into context, queries memories by relevance, ranks by importance, and adapts to tool-based memory retrieval. |
| Savings Tracker | ⬜ | [Python: `savings_tracker.py`](https://github.com/chopratejas/headroom/blob/main/headroom/proxy/savings_tracker.py) | Tracks cumulative token savings across all requests. Persists to disk so savings survive restarts. Powers the savings dashboard. |
| Request Logger | ⬜ | [Python: `request_logger.py`](https://github.com/chopratejas/headroom/blob/main/headroom/proxy/request_logger.py) | Logs full request/response pairs in JSONL format for debugging, auditing, and replay. |
| Dashboard | ⬜ | [Python: `dashboard/`](https://github.com/chopratejas/headroom/tree/main/headroom/dashboard) | Web UI showing real-time compression stats, savings history, cache hit rates, and per-model breakdowns. |
| Probe Recorder | ⬜ | [Python: `probe_recorder.py`](https://github.com/chopratejas/headroom/blob/main/headroom/proxy/probe_recorder.py) | Records detailed compression diagnostics for debugging. Shows which transforms fired, what they compressed, and why. |
| Loopback Guard | ⬜ | [Python: `loopback_guard.py`](https://github.com/chopratejas/headroom/blob/main/headroom/proxy/loopback_guard.py) | Detects and rejects requests where the proxy would forward to itself, preventing infinite loops in misconfigured setups. |
| SSL Context | ⬜ | [Python: `ssl_context.py`](https://github.com/chopratejas/headroom/blob/main/headroom/proxy/ssl_context.py) | Configures TLS certificates for HTTPS termination. Handles self-signed certs for local development. |
| Warmup | ⬜ | [Python: `warmup.py`](https://github.com/chopratejas/headroom/blob/main/headroom/proxy/warmup.py) | Pre-loads ML models (Kompress, embeddings) and tokenizer data on startup so the first request doesn't pay cold-start latency. |
| Stage Timer | ⬜ | [Python: `stage_timer.py`](https://github.com/chopratejas/headroom/blob/main/headroom/proxy/stage_timer.py) | Measures latency at each pipeline stage (detection, compression, forwarding, response) for performance profiling. |
| WebSocket Session Registry | ⬜ | [Python: `ws_session_registry.py`](https://github.com/chopratejas/headroom/blob/main/headroom/proxy/ws_session_registry.py) | Tracks active WebSocket sessions. Maps session IDs to connection state for multi-turn WebSocket conversations. |
| Project Context | ⬜ | [Python: `project_context.py`](https://github.com/chopratejas/headroom/blob/main/headroom/proxy/project_context.py) | Injects project-level context (file trees, recent changes) into the LLM's context window for code-aware agents. |

### Pipeline Extension System

Plugin system for hooking into the request lifecycle. Allows third-party code to observe and mutate requests at defined stages.

| Feature | Status | Upstream Location | What It Does |
|---|---|---|---|
| Pipeline Extension Protocol | ❌ | [Python: `pipeline.py`](https://github.com/chopratejas/headroom/blob/main/headroom/pipeline.py) | Defines the `PipelineExtension` protocol with `on_pipeline_event()`. Extensions are discovered via Python entry points (`headroom.pipeline_extension` group) or registered programmatically. |
| Pipeline Event System | ❌ | [Python: `pipeline.py`](https://github.com/chopratejas/headroom/blob/main/headroom/pipeline.py) | Emits `PipelineEvent` objects at 11 lifecycle stages (SETUP through RESPONSE_RECEIVED). Extensions can mutate messages, tools, headers, or metadata at any stage. |

### Observability

Monitoring, metrics, and tracing for production deployments.

| Feature | Status | Upstream Location | What It Does |
|---|---|---|---|
| Prometheus metrics collection | ⬜ | [Rust: `observability/`](https://github.com/chopratejas/headroom/tree/main/crates/headroom-proxy/src/observability) | Records per-request metrics: total requests, tokens saved, compression ratios, latency histograms, cache hit rates. Exposed via `/metrics` endpoint. |
| OpenTelemetry tracing | ❌ | [Python: `observability/`](https://github.com/chopratejas/headroom/tree/main/headroom/observability) | Full distributed tracing with span-per-transform granularity. Traces flow through detection, compression, forwarding, and response handling. |
| Savings persistence | ❌ | [Python: `proxy_savings.json`](https://github.com/chopratejas/headroom/tree/main/headroom/proxy) | Writes cumulative savings data to `~/.headroom/proxy_savings.json`. Survives restarts. Configurable via `HEADROOM_SAVINGS_PATH`. |
| Stats history rollups | ⬜ | [Python: stats endpoint](https://github.com/chopratejas/headroom/blob/main/headroom/proxy/server.py) | Aggregates savings data into hourly/daily/weekly/monthly rollups for trend analysis and reporting. |

### Other Upstream Subsystems

Miscellaneous subsystems from the Python side. Most are application-level features of the proxy, not core compression.

| Feature | Status | Upstream Location | What It Does |
|---|---|---|---|
| Provider registry | ❌ | [Python: `providers/`](https://github.com/chopratejas/headroom/tree/main/headroom/providers) | Registry of LLM provider backends (Anthropic, OpenAI, Bedrock, Vertex, Azure, OpenRouter). Resolves API URLs and authentication per provider. |
| Pricing data | ❌ | [Python: `pricing/`](https://github.com/chopratejas/headroom/tree/main/headroom/pricing) | Model pricing tables (cost per input/output token per model). Used by budget enforcement and savings tracking to compute dollar amounts. |
| Agent savings profiles | ❌ | [Python: `agent_savings.py`](https://github.com/chopratejas/headroom/blob/main/headroom/agent_savings.py) | Named high-savings profiles (e.g. `agent-90` for Claude Code, Cursor, Codex). Pre-configured compression settings optimized for specific agent workloads. |
| Subscription management | ❌ | [Python: `subscription/`](https://github.com/chopratejas/headroom/tree/main/headroom/subscription) | License and subscription validation. Gates features behind plan tiers. |
| Reporting | ❌ | [Python: `reporting/`](https://github.com/chopratejas/headroom/tree/main/headroom/reporting) | Usage reporting for billing and analytics. Aggregates request counts, token usage, and savings by time period. |
| Integrations | ❌ | [Python: `integrations/`](https://github.com/chopratejas/headroom/tree/main/headroom/integrations) | Third-party framework integrations (LangChain, Agno, etc.). Wraps headroom compression into framework-specific middleware. |
| MCP Registry | ❌ | [Python: `mcp_registry/`](https://github.com/chopratejas/headroom/tree/main/headroom/mcp_registry) | Registry of MCP (Model Context Protocol) servers. Manages server discovery and connection lifecycle. |
| Evals framework | ❌ | [Python: `evals/`](https://github.com/chopratejas/headroom/tree/main/headroom/evals) | Compression quality evaluation. Measures whether compressed content leads to equivalent LLM outputs. Benchmarks compression strategies against ground truth. |
| Capture/replay | ❌ | [Python: `capture/`](https://github.com/chopratejas/headroom/tree/main/headroom/capture) | Records raw request/response pairs for deterministic replay. Useful for debugging compression issues without hitting the live LLM. |
| Graph | ❌ | [Python: `graph/`](https://github.com/chopratejas/headroom/tree/main/headroom/graph) | Dependency graph for compression planning. Maps relationships between messages, tools, and context blocks to inform compression order. |
| Learn | ❌ | [Python: `learn/`](https://github.com/chopratejas/headroom/tree/main/headroom/learn) | Training data collection and model fine-tuning utilities. Captures compression examples for improving Kompress and other ML models. |
| Prediction | ❌ | [Python: `prediction/`](https://github.com/chopratejas/headroom/tree/main/headroom/prediction) | Predicts optimal compression parameters before running compression. Estimates savings without doing the full transform. |
| RTK integration | ❌ | [Python: `rtk/`](https://github.com/chopratejas/headroom/tree/main/headroom/rtk) | Runtime Toolkit integration for context-aware tool output injection. |
| Lean context tool | ❌ | [Python: `lean_ctx/`](https://github.com/chopratejas/headroom/tree/main/headroom/lean_ctx) | Lean context management tool. Alternative to RTK for minimal-footprint context injection. |
| CLI | ❌ | [Python: `cli/`](https://github.com/chopratejas/headroom/tree/main/headroom/cli) | Full command-line interface with subcommands: `proxy` (start server), `wrap` (proxy CLI tools), `install` (set up integrations), `learn` (train models), `memory` (manage memories), `perf` (profile), `evals` (run evaluations). |

### Scorecard

| Category | ✅ | ⭐ | ❌ | ⬜ |
|---|---|---|---|---|
| Compressors | 6 | 1 | 4 | 0 |
| Live Zone | 5 | 0 | 0 | 0 |
| Pipeline | 3 | 0 | 0 | 0 |
| Reformats | 2 | 0 | 0 | 0 |
| Offloads | 5 | 2 | 0 | 0 |
| Core Transforms | 9 | 0 | 3 | 0 |
| CCR | 5 | 0 | 5 | 0 |
| Tokenizers | 6 | 0 | 0 | 0 |
| Relevance | 4 | 0 | 0 | 0 |
| Signals | 3 | 0 | 0 | 0 |
| Infrastructure | 5 | 0 | 0 | 0 |
| Cache Stabilization | 0 | 0 | 5 | 0 |
| Caching Subsystems | 0 | 0 | 7 | 0 |
| Proxy / HTTP | 0 | 0 | 0 | 8 |
| Cloud Providers | 0 | 0 | 0 | 6 |
| Proxy Middleware | 0 | 0 | 0 | 13 |
| Pipeline Extensions | 0 | 0 | 2 | 0 |
| Observability | 0 | 0 | 2 | 2 |
| Other Subsystems | 0 | 0 | 15 | 0 |
| **Total** | **53** | **3** | **43** | **29** |

### Key Gaps That Matter for a Go Consumer

1. **❌ Cache Stabilization** -- drift detector, volatile detector, tool-def normalizer protect prompt cache hit rates. Any Go service using live-zone compression would benefit from these.
2. **❌ CCR Tool Injection + Response Handler** -- without these the CCR store is write-only; the LLM cannot retrieve dropped content. The store backends are ported but the retrieval loop is not.
3. **❌ Content Router** -- goheadroom uses the pipeline orchestrator but lacks the higher-level mixed-content splitting and routing logic that auto-dispatches heterogeneous content.
4. **❌ Image Compression** -- no image token optimization in Go.
5. **❌ OpenTelemetry Tracing** -- no observability hooks for production monitoring of compression.
