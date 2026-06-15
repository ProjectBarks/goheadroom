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


def run_fixture(fix: dict) -> str:
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
        tok = Tokenizer.for_model("gpt-4o-mini")
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
        import hashlib
        h = hashlib.sha256(json.dumps(inp).encode()).hexdigest()[:24]
        return h

    return f"SKIP:{transform}"


def main():
    if len(sys.argv) < 2:
        print("usage: python-bench.py <fixture.json> [--bench N]", file=sys.stderr)
        sys.exit(1)

    with open(sys.argv[1]) as f:
        fix = json.load(f)

    bench_n = 0
    for i in range(2, len(sys.argv)):
        if sys.argv[i] == "--bench" and i + 1 < len(sys.argv):
            bench_n = int(sys.argv[i + 1])

    # Single run for output
    output = run_fixture(fix)
    print(output, end="")

    # Warm benchmark
    if bench_n > 0:
        t0 = time.perf_counter_ns()
        for _ in range(bench_n):
            run_fixture(fix)
        elapsed = time.perf_counter_ns() - t0
        ns_per_op = elapsed // bench_n
        print(ns_per_op, file=sys.stderr)


if __name__ == "__main__":
    main()
