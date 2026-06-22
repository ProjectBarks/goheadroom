#!/usr/bin/env python3
"""Python bench wrapper for parity report.

Same CLI interface as Go/Rust bench binaries:
    python3 scripts/python-bench.py <fixture.json> [--bench N]

Outputs compressed text to stdout.
With --bench N: runs N iterations and prints ns/op to stderr.
"""
import json
import sys
import time

# Ensure headroom is importable (set PYTHONPATH or install)
try:
    from headroom.transforms.code_compressor import CodeAwareCompressor
    from headroom.compression.handlers.json_handler import JSONStructureHandler
    from headroom.transforms.smart_crusher import SmartCrusher, SmartCrusherConfig
    from headroom.transforms.diff_compressor import DiffCompressor, DiffCompressorConfig
    from headroom.transforms.log_compressor import LogCompressor, LogCompressorConfig
    from headroom.transforms.content_detector import detect_content_type as _detect_content_type
except ImportError as e:
    print(f"ERROR: {e}. Set PYTHONPATH to headroom repo root.", file=sys.stderr)
    sys.exit(1)


def _make_config(cls, config: dict):
    """Construct a typed config object, ignoring unknown keys."""
    import inspect
    valid = inspect.signature(cls.__init__).parameters
    kwargs = {k: v for k, v in config.items() if k in valid and v is not None}
    return cls(**kwargs)


def _e2e_compress(text: str):
    """Replicate Go's livezone.CompressText routing.

    Returns compressed text if compression succeeded and saved tokens,
    or None if no compression was applied.
    """
    MIN_COMPRESSIBLE_BYTES = 512

    if len(text) < MIN_COMPRESSIBLE_BYTES:
        return None

    result = _detect_content_type(text)
    ct = result.content_type.value

    compressed_text = None

    if ct == "json_array":
        sc = SmartCrusher()
        cr = sc.crush(text)
        if cr.was_modified:
            compressed_text = cr.compressed
    elif ct == "build":
        lc = LogCompressor()
        lr = lc.compress(text, bias=0.0)
        if lr.compressed != text:
            compressed_text = lr.compressed
    elif ct == "search":
        from headroom.transforms.search_compressor import SearchCompressor
        sc = SearchCompressor()
        sr = sc.compress(text, bias=0.0)
        if sr.compressed != text:
            compressed_text = sr.compressed
    elif ct == "diff":
        dc = DiffCompressor()
        dr = dc.compress(text)
        if dr.compressed != text:
            compressed_text = dr.compressed
    else:
        # PlainText, SourceCode, Html - no compressor available
        return None

    if compressed_text is None:
        return None

    # Token-validated rejection gate: accept only when compressed tokens < original
    from headroom.providers.openai import OpenAITokenCounter
    tok = OpenAITokenCounter("gpt-4o")
    orig_tokens = tok.count_text(text)
    comp_tokens = tok.count_text(compressed_text)
    if comp_tokens >= orig_tokens:
        return None

    return compressed_text


def run_fixture(fix: dict, raw_text: str = "") -> str:
    """Run the appropriate Python transform on a fixture and return output."""
    transform = fix["transform"]
    inp = fix["input"]
    config = fix.get("config", {})

    if transform == "diff_compressor":
        cfg = _make_config(DiffCompressorConfig, config)
        dc = DiffCompressor(config=cfg)
        return dc.compress(inp).compressed

    if transform == "log_compressor":
        cfg = _make_config(LogCompressorConfig, config)
        lc = LogCompressor(config=cfg)
        return lc.compress(inp).compressed

    if transform == "smart_crusher":
        content = inp.get("content", inp) if isinstance(inp, dict) else inp
        query = inp.get("query", "") if isinstance(inp, dict) else ""
        bias = inp.get("bias", 1.0) if isinstance(inp, dict) else 1.0
        cfg = _make_config(SmartCrusherConfig, config)
        sc = SmartCrusher(config=cfg)
        return sc.crush(content, query=query, bias=bias).compressed

    if transform == "tokenizer":
        from headroom.tokenizer import Tokenizer
        tok = Tokenizer.for_model("gpt-4o")
        return str(tok.count_text(inp))

    if transform == "content_detector":
        result = _detect_content_type(inp)
        return f"{result.content_type.value}:{result.confidence:.4f}"

    if transform == "json_compressor":
        handler = JSONStructureHandler(
            short_value_threshold=config.get("short_value_threshold", 20),
            entropy_threshold=config.get("entropy_threshold", 0.85),
            max_array_items_full=config.get("max_array_items_full", 3),
            max_number_digits=config.get("max_number_digits", 10),
        )
        result = handler.get_mask(inp)
        return "".join(c for i, c in enumerate(inp) if i < len(result.mask.mask) and result.mask.mask[i])

    if transform == "code_compressor":
        compressor = CodeAwareCompressor()
        return compressor.compress(inp).compressed

    if transform == "ccr":
        # Use compact JSON to match Go's json.Marshal (no spaces)
        raw = json.dumps(inp, separators=(",", ":"), sort_keys=True).encode()
        store = {}
        import hashlib
        key = hashlib.sha256(raw).hexdigest()[:24]
        store[key] = raw
        got = store.get(key, b"")
        if len(got) == len(raw):
            return f"roundtrip:{len(raw)}"
        return "FAIL"

    if transform == "cache_aligner":
        from headroom.transforms.cache_aligner import CacheAligner, CacheAlignerConfig
        from headroom.tokenizer import Tokenizer
        from headroom.tokenizers.tiktoken_counter import TiktokenCounter
        from headroom.utils import compute_short_hash
        cfg = CacheAlignerConfig(**{k: v for k, v in config.items() if k in CacheAlignerConfig.__dataclass_fields__})
        aligner = CacheAligner(cfg)
        tokenizer_inst = Tokenizer(TiktokenCounter("gpt-4o"))
        result = aligner.apply(inp, tokenizer_inst)
        system_texts = [m.get("content", "") for m in inp if m.get("role") == "system" and isinstance(m.get("content"), str)]
        bench_hash = compute_short_hash("\n---\n".join(system_texts))
        cm = result.cache_metrics
        out = {
            "bench_hash": bench_hash,
            "cache_metrics": {
                "prefix_changed": cm.prefix_changed,
                "previous_hash": cm.previous_hash,
                "stable_prefix_bytes": cm.stable_prefix_bytes,
                "stable_prefix_hash": cm.stable_prefix_hash,
                "stable_prefix_tokens_est": cm.stable_prefix_tokens_est,
            } if cm else {},
            "diff_artifact": None,
            "markers_inserted": result.markers_inserted,
            "messages": result.messages,
            "timing": result.timing,
            "tokens_after": result.tokens_after,
            "tokens_before": result.tokens_before,
            "transforms_applied": result.transforms_applied,
            "warnings": [],
            "waste_signals": None,
        }
        return json.dumps(out, separators=(",", ":"), sort_keys=True)

    if transform == "search_compressor":
        from headroom.transforms.search_compressor import SearchCompressor
        sc = SearchCompressor()
        return sc.compress(inp).compressed

    if transform == "e2e_unmutated":
        compressed = _e2e_compress(inp)
        if compressed is None or compressed == inp:
            return "UNMUTATED"
        return "MUTATED:" + compressed[:50]

    if transform == "e2e_mutated":
        compressed = _e2e_compress(inp)
        if compressed is None or compressed == inp:
            return "NOT_COMPRESSED"
        return compressed

    return f"SKIP:{transform}"


def main():
    if len(sys.argv) < 2:
        print("usage: python-bench.py <fixture.json> [--bench N]", file=sys.stderr)
        sys.exit(1)

    with open(sys.argv[1]) as f:
        raw_text = f.read()
    fix = json.loads(raw_text)

    bench_n = 0
    for i in range(2, len(sys.argv)):
        if sys.argv[i] == "--bench" and i + 1 < len(sys.argv):
            bench_n = int(sys.argv[i + 1])

    # Single run for output
    output = run_fixture(fix, raw_text)
    print(output, end="")

    # Warm benchmark
    if bench_n > 0:
        t0 = time.perf_counter_ns()
        for _ in range(bench_n):
            run_fixture(fix, raw_text)
        elapsed = time.perf_counter_ns() - t0
        ns_per_op = elapsed // bench_n
        print(ns_per_op, file=sys.stderr)


if __name__ == "__main__":
    main()
