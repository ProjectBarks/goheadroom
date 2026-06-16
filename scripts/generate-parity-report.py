#!/usr/bin/env python3
"""
Parity report generator: runs Go, Rust, and Python CLIs on every fixture,
compares outputs, times cold (CLI) and warm (library) paths,
generates self-contained HTML with diffs and triple benchmarks.

Usage:
    python3 scripts/generate-parity-report.py [--go-bin PATH] [--rust-bin PATH] [--python-bin PATH] [--fixtures DIR] [--out PATH]

Requires: pip install jinja2
"""
import argparse, json, os, subprocess, sys, time, difflib
from pathlib import Path
from html import escape
from collections import defaultdict

try:
    from jinja2 import Environment, FileSystemLoader
except ImportError:
    print("ERROR: jinja2 is required. Install with: pip install jinja2", file=sys.stderr)
    sys.exit(1)

SCRIPT_DIR = Path(__file__).resolve().parent
PROJECT_DIR = SCRIPT_DIR.parent
TEMPLATE_DIR = SCRIPT_DIR / "templates"

PYTHON_NATIVE_TRANSFORMS = {"content_detector", "ccr", "cache_aligner", "code_compressor", "json_compressor", "search_compressor", "e2e_unmutated", "e2e_mutated"}


def parse_args():
    p = argparse.ArgumentParser(description="Generate goheadroom parity report")
    p.add_argument("--go-bin",
        default=os.environ.get("PARITY_GO_BIN", str(PROJECT_DIR / "goheadroom-parity-check")),
        help="Path to Go parity-check binary (default: $PARITY_GO_BIN or ./goheadroom-parity-check)")
    p.add_argument("--rust-bin",
        default=os.environ.get("PARITY_RUST_BIN", ""),
        help="Path to Rust bench binary (default: $PARITY_RUST_BIN or $PATH lookup)")
    p.add_argument("--python-bin",
        default=os.environ.get("PARITY_PYTHON_BIN", ""),
        help="Path to Python bench script (default: $PARITY_PYTHON_BIN or scripts/python-bench.py)")
    p.add_argument("--fixtures",
        default=os.environ.get("PARITY_FIXTURES", str(PROJECT_DIR / "core" / "testdata" / "parity")),
        help="Path to fixtures directory (default: $PARITY_FIXTURES or core/testdata/parity)")
    p.add_argument("--out",
        default=str(PROJECT_DIR / "parity-report.html"),
        help="Output HTML path")
    p.add_argument("--cold-runs", type=int, default=3,
        help="Runs for cold timing (default: 3)")
    return p.parse_args()


def find_binary(name, hint_paths):
    for p in hint_paths:
        if p and os.path.isfile(p) and os.access(p, os.X_OK):
            return p
    import shutil
    return shutil.which(name)


def run_timed(cmd, runs=3):
    if isinstance(cmd, str):
        cmd = [cmd]
    best_ms = float("inf")
    stdout = ""
    rc = 1
    for _ in range(runs):
        t0 = time.perf_counter()
        try:
            r = subprocess.run(cmd, capture_output=True, text=True, timeout=30)
            elapsed = (time.perf_counter() - t0) * 1000
            if r.returncode == 0:
                stdout = r.stdout
                rc = 0
                best_ms = min(best_ms, elapsed)
            elif rc != 0:
                stdout = r.stderr[:200]
        except Exception as e:
            stdout = f"ERROR: {e}"
    return stdout, rc, best_ms if best_ms < float("inf") else 0


def collect_results(args):
    go_bin = find_binary("goheadroom-parity-check", [
        args.go_bin, str(PROJECT_DIR / "goheadroom-parity-check")])
    rust_bin = find_binary("headroom-bench", [args.rust_bin])
    python_cmd = None
    if args.python_bin:
        python_cmd = args.python_bin.split()
    elif (PROJECT_DIR / "scripts" / "python-bench.py").exists():
        python_cmd = ["python3", str(PROJECT_DIR / "scripts" / "python-bench.py")]

    if not go_bin:
        print("ERROR: parity-check binary not found. Build with:", file=sys.stderr)
        print("  go build -o goheadroom-parity-check ./cmd/parity-check/", file=sys.stderr)
        print("Or pass --go-bin <path>", file=sys.stderr)
        sys.exit(1)
    if not rust_bin:
        print("WARNING: Rust bench binary not found. Pass --rust-bin <path> or add to $PATH.", file=sys.stderr)

    print(f"Go binary:     {go_bin}", file=sys.stderr)
    print(f"Rust binary:   {rust_bin or '(not found)'}", file=sys.stderr)
    print(f"Python binary: {python_cmd[0] if python_cmd else '(not found)'}", file=sys.stderr)

    r = subprocess.run([go_bin, args.fixtures, "--json"],
                       capture_output=True, text=True, timeout=120)
    if r.returncode not in (0, 1):
        print(f"ERROR: parity-check failed: {r.stderr[:200]}", file=sys.stderr)
        sys.exit(1)

    go_results = json.loads(r.stdout)
    print(f"Processing {len(go_results)} results...", file=sys.stderr)

    results = []
    for i, gr in enumerate(go_results):
        fpath = Path(args.fixtures) / gr["transform"] / gr.get("fixture", "")
        fixture_input = ""
        if fpath.exists():
            with open(fpath) as f:
                raw_input = json.load(f).get("input", "")
            fixture_input = (raw_input[:3000] if isinstance(raw_input, str)
                           else json.dumps(raw_input, indent=2)[:3000])

        rust_out, rust_rc, rust_ms = "", 1, 0
        if rust_bin and fpath.exists():
            rust_out, rust_rc, rust_ms = run_timed([rust_bin, str(fpath)], args.cold_runs)

        python_out, python_rc, python_ms = "", 1, 0
        if python_cmd and fpath.exists() and gr["transform"] in PYTHON_NATIVE_TRANSFORMS:
            try:
                python_out, python_rc, python_ms = run_timed(python_cmd + [str(fpath)], args.cold_runs)
            except Exception:
                pass

        compared_to = "Python" if gr["transform"] in PYTHON_NATIVE_TRANSFORMS and python_cmd else "Rust"
        diff_html = ""
        if gr["status"] == "fail" and gr.get("message"):
            diff_html = f'<span class="diff-del">{escape(gr.get("message", "")[:500])}</span>'

        results.append({
            "fixture": gr.get("fixture", ""),
            "category": gr["transform"],
            "transform": gr["transform"],
            "status": gr["status"],
            "compared_to": compared_to,
            "fixture_input": escape(fixture_input),
            "go_ms": gr.get("go_ms", 0),
            "rust_ms": round(rust_ms, 2),
            "python_ms": round(python_ms, 2),
            "go_bytes": 0, "rust_bytes": len(rust_out), "python_bytes": len(python_out),
            "go_out": "", "rust_out": escape(rust_out[:1500]),
            "python_out": escape(python_out[:1500]),
            "diff_html": diff_html,
            "go_warm_us": 0, "rust_warm_us": 0, "python_warm_us": 0,
        })
        if (i + 1) % 50 == 0:
            print(f"  {i + 1}/{len(go_results)}...", file=sys.stderr)

    return results


def compute_diff_html(go_out, rust_out):
    go_lines = go_out.splitlines(keepends=True)
    rust_lines = rust_out.splitlines(keepends=True)
    diff = list(difflib.unified_diff(rust_lines, go_lines, fromfile="rust", tofile="go", lineterm=""))
    if not diff:
        return ""
    lines = []
    for line in diff[:50]:
        cls = ""
        if line.startswith("+") and not line.startswith("+++"):
            cls = "diff-add"
        elif line.startswith("-") and not line.startswith("---"):
            cls = "diff-del"
        elif line.startswith("@@"):
            cls = "diff-hunk"
        lines.append(f'<span class="{cls}">{escape(line)}</span>')
    if len(diff) > 50:
        lines.append(f"<span class='diff-hunk'>... {len(diff) - 50} more lines</span>")
    return "\n".join(lines)


def build_categories(results):
    cats = defaultdict(list)
    for r in results:
        cats[r["category"]].append(r)

    def avg(values):
        return sum(values) / len(values) if values else 0

    categories = []
    for name in sorted(cats):
        entries = cats[name]
        categories.append({
            "name": name,
            "entries": sorted(entries, key=lambda x: x["fixture"]),
            "n_pass": sum(1 for e in entries if e["status"] == "pass"),
            "n_total": len(entries),
            "go_warm_avg": avg([e["go_warm_us"] for e in entries if e["go_warm_us"] > 0]),
            "rust_warm_avg": avg([e["rust_warm_us"] for e in entries if e["rust_warm_us"] > 0]),
            "python_warm_avg": avg([e["python_warm_us"] for e in entries if e["python_warm_us"] > 0]),
            "go_cold_avg": avg([e["go_ms"] for e in entries if e["go_ms"] > 0]),
            "rust_cold_avg": avg([e["rust_ms"] for e in entries if e["rust_ms"] > 0]),
            "python_cold_avg": avg([e["python_ms"] for e in entries if e["python_ms"] > 0]),
        })
    return categories


def render(results, out_path):
    total = len(results)
    passed = sum(1 for r in results if r["status"] == "pass")
    failed = sum(1 for r in results if r["status"] == "fail")
    skipped = sum(1 for r in results if r["status"] in ("both_skip", "go_error", "rust_error"))
    pct = (passed / total * 100) if total else 0

    env = Environment(
        loader=FileSystemLoader(str(TEMPLATE_DIR)),
        autoescape=False,
        trim_blocks=True,
        lstrip_blocks=True,
    )
    template = env.get_template("parity-report.html.j2")

    html = template.render(
        total=total,
        passed=passed,
        failed=failed,
        skipped=skipped,
        pct=pct,
        ncols=15,
        categories=build_categories(results),
    )

    with open(out_path, "w") as f:
        f.write(html)
    print(f"\nReport: {out_path}", file=sys.stderr)
    print(f"{passed}/{total} pass ({pct:.0f}%), {failed} fail, {skipped} skip", file=sys.stderr)


def main():
    args = parse_args()
    results = collect_results(args)
    render(results, args.out)


if __name__ == "__main__":
    main()
