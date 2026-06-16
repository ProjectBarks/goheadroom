#!/usr/bin/env python3
"""
Parity report generator: runs Go, Rust, and Python CLIs on every fixture,
compares outputs, times cold (CLI) and warm (library) paths,
generates self-contained HTML with diffs and triple benchmarks.

Usage:
    python3 scripts/generate-parity-report.py [--go-bin PATH] [--rust-bin PATH] [--python-bin PATH] [--fixtures DIR] [--out PATH]
"""
import argparse, json, os, subprocess, sys, time, difflib
from pathlib import Path
from html import escape
from collections import defaultdict

SCRIPT_DIR = Path(__file__).resolve().parent
PROJECT_DIR = SCRIPT_DIR.parent

PYTHON_NATIVE_TRANSFORMS = {"content_detector", "ccr", "cache_aligner", "code_compressor", "json_compressor"}

def parse_args():
    p = argparse.ArgumentParser(description="Generate goheadroom parity report")
    p.add_argument("--go-bin", default=os.environ.get("GO_BENCH_BIN", str(PROJECT_DIR / "goheadroom-bench")),
                    help="Path to Go bench binary (default: $GO_BENCH_BIN or ./goheadroom-bench)")
    p.add_argument("--rust-bin", default=os.environ.get("RUST_BENCH_BIN", ""),
                    help="Path to Rust bench binary (default: $RUST_BENCH_BIN)")
    p.add_argument("--python-bin", default=os.environ.get("PYTHON_BENCH_BIN", ""),
                    help="Path to Python bench script (default: $PYTHON_BENCH_BIN)")
    p.add_argument("--fixtures", default=str(PROJECT_DIR / "testdata" / "parity"),
                    help="Path to parity fixtures directory")
    p.add_argument("--out", default=str(PROJECT_DIR / "parity-report.html"),
                    help="Output HTML path")
    p.add_argument("--warm-iters", type=int, default=50,
                    help="Iterations for warm benchmarks (default: 50)")
    p.add_argument("--cold-runs", type=int, default=3,
                    help="Runs for cold timing (default: 3)")
    return p.parse_args()

def find_binary(name, hint_paths):
    """Find a binary by checking hint paths, then PATH."""
    for p in hint_paths:
        if p and os.path.isfile(p) and os.access(p, os.X_OK):
            return p
    # Try PATH
    import shutil
    found = shutil.which(name)
    return found

def run_timed(binary, fixture_path, runs=3):
    best_ms = float("inf")
    stdout = ""
    rc = 1
    for _ in range(runs):
        t0 = time.perf_counter()
        try:
            r = subprocess.run([binary, fixture_path], capture_output=True, text=True, timeout=10)
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

def run_timed_list(cmd_list, runs=3):
    best_ms = float("inf")
    stdout = ""
    rc = 1
    for _ in range(runs):
        t0 = time.perf_counter()
        try:
            r = subprocess.run(cmd_list, capture_output=True, text=True, timeout=30)
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

def run_warm(binary, fixture_path, iterations=50):
    """Run binary with --bench N, return ns/op from stderr."""
    try:
        r = subprocess.run(
            [binary, fixture_path, "--bench", str(iterations)],
            capture_output=True, text=True, timeout=60
        )
        if r.returncode == 0:
            for line in r.stderr.strip().splitlines():
                line = line.strip()
                if line.isdigit():
                    return int(line)
    except Exception:
        pass
    return 0

def run_warm_list(cmd_list, fixture_path, iterations=50):
    """Run command list with --bench N, return ns/op from stderr."""
    try:
        r = subprocess.run(
            cmd_list + [fixture_path, "--bench", str(iterations)],
            capture_output=True, text=True, timeout=60
        )
        if r.returncode == 0:
            for line in r.stderr.strip().splitlines():
                line = line.strip()
                if line.isdigit():
                    return int(line)
    except Exception:
        pass
    return 0

def normalize_output(transform, output):
    if transform == "content_detector":
        mapping = {
            "plaintext": "text", "gitdiff": "diff", "jsonarray": "json",
            "sourcecode": "code", "searchresults": "search", "buildoutput": "build",
        }
        parts = output.split(":", 1)
        if len(parts) == 2:
            key = parts[0].lower().replace("_", "")
            return mapping.get(key, parts[0].lower()) + ":" + parts[1]
    return output

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
        lines.append(f"<span class='diff-hunk'>... {len(diff)-50} more lines</span>")
    return "\n".join(lines)

def main():
    args = parse_args()

    go_bin = find_binary("goheadroom-bench", [
        args.go_bin,
        "/tmp/goheadroom-bench",
        str(PROJECT_DIR / "goheadroom-bench"),
    ])
    rust_bin = find_binary("headroom-bench", [
        args.rust_bin,
        os.path.expanduser("~/headroom-src/target/release/headroom-bench"),
    ])

    if not go_bin:
        print("ERROR: Go bench binary not found. Build with: go build -o goheadroom-bench ./cmd/bench/", file=sys.stderr)
        sys.exit(1)
    if not rust_bin:
        print("WARNING: Rust bench binary not found. Rust columns will be empty.", file=sys.stderr)

    python_bin = args.python_bin
    if not python_bin:
        candidate = PROJECT_DIR / "scripts" / "python-bench.py"
        if candidate.exists():
            python_bin = f"python3 {candidate}"
    if python_bin:
        print(f"Python binary: {python_bin}", file=sys.stderr)

    print(f"Go binary:   {go_bin}", file=sys.stderr)
    print(f"Rust binary: {rust_bin or '(not found)'}", file=sys.stderr)

    fixtures_dir = Path(args.fixtures)
    fixtures = sorted(fixtures_dir.rglob("*.json"))
    print(f"Processing {len(fixtures)} fixtures...", file=sys.stderr)

    warm_iters = args.warm_iters
    cold_runs = args.cold_runs

    results = []
    for i, fpath in enumerate(fixtures):
        with open(fpath) as f:
            fix = json.load(f)
        transform = fix.get("transform", "unknown")
        category = fpath.parent.name

        # Store fixture input for the expandable detail view
        raw_input = fix.get("input", "")
        if isinstance(raw_input, str):
            fixture_input = raw_input[:3000]
        else:
            fixture_input = json.dumps(raw_input, indent=2)[:3000]

        go_out, go_rc, go_ms = run_timed(go_bin, str(fpath), cold_runs)

        rust_out, rust_rc, rust_ms = "", 1, 0
        if rust_bin:
            rust_out, rust_rc, rust_ms = run_timed(rust_bin, str(fpath), cold_runs)

        python_out, python_rc, python_ms = "", 1, 0
        if python_bin:
            try:
                python_cmd = python_bin.split() + [str(fpath)]
                python_out, python_rc, python_ms = run_timed_list(python_cmd, cold_runs)
            except Exception:
                pass

        go_norm = normalize_output(transform, go_out)
        rust_norm = normalize_output(transform, rust_out)

        # Determine comparison target: Python for native transforms, Rust for Rust-backed ones
        use_python = transform in PYTHON_NATIVE_TRANSFORMS and python_bin and python_rc == 0

        if use_python:
            python_norm = normalize_output(transform, python_out)
            if go_rc != 0:
                status = "go_error"
                compared_to = "-"
            elif go_norm == python_norm:
                status = "pass"
                compared_to = "Python"
            else:
                status = "fail"
                compared_to = "Python"
        elif not rust_bin:
            status = "pass" if go_rc == 0 else "go_error"
            compared_to = "Go-only"
        elif go_rc != 0 and rust_rc != 0:
            status = "both_skip"
            compared_to = "-"
        elif go_rc != 0:
            status = "go_error"
            compared_to = "-"
        elif rust_rc != 0:
            status = "pass" if go_rc == 0 else "rust_error"
            compared_to = "Go-only"
        elif go_norm == rust_norm:
            status = "pass"
            compared_to = "Rust"
        else:
            status = "fail"
            compared_to = "Rust"

        diff_html = ""
        if status == "fail":
            compare_out = python_out if use_python else rust_out
            diff_html = compute_diff_html(go_out[:3000], compare_out[:3000])

        # Warm benchmarks via --bench (library-only, no startup)
        # Skip for transforms that aren't actually exercised (cache_aligner)
        skip_warm = go_out.startswith("SKIP:")
        iters = warm_iters
        if transform in ("tokenizer",):
            iters = max(10, warm_iters // 5)
        go_warm_ns = run_warm(go_bin, str(fpath), iters) if go_rc == 0 and not skip_warm else 0
        rust_warm_ns = run_warm(rust_bin, str(fpath), iters) if rust_bin and rust_rc == 0 and not skip_warm else 0
        python_warm_ns = 0
        if python_bin and python_rc == 0 and not skip_warm:
            python_warm_ns = run_warm_list(python_bin.split(), str(fpath), iters)

        results.append({
            "fixture": fpath.name,
            "category": category,
            "transform": transform,
            "status": status,
            "compared_to": compared_to,
            "fixture_input": fixture_input,
            "go_ms": round(go_ms, 2),
            "rust_ms": round(rust_ms, 2),
            "python_ms": round(python_ms, 2) if python_ms else 0,
            "go_bytes": len(go_out),
            "rust_bytes": len(rust_out),
            "python_bytes": len(python_out),
            "go_out": go_out[:1500],
            "rust_out": rust_out[:1500],
            "python_out": python_out[:1500],
            "diff_html": diff_html,
            "go_warm_us": round(go_warm_ns / 1000, 1) if go_warm_ns else 0,
            "rust_warm_us": round(rust_warm_ns / 1000, 1) if rust_warm_ns else 0,
            "python_warm_us": round(python_warm_ns / 1000, 1) if python_warm_ns else 0,
        })
        if (i+1) % 20 == 0:
            print(f"  {i+1}/{len(fixtures)}...", file=sys.stderr)

    generate_html(results, args.out)

def fmt_us(us):
    if us == 0:
        return "-"
    if us >= 1000:
        return f"{us/1000:.1f}ms"
    return f"{us:.0f}us"

def fmt_ratio(go_val, other_val, unit=""):
    """Format a ratio as 'Nx faster/slower' with color."""
    if go_val <= 0 or other_val <= 0:
        return '<span style="color:#8b949e">-</span>'
    ratio = other_val / go_val
    if ratio >= 1.0:
        color = "#3fb950"
        return f'<span style="color:{color}">{ratio:.1f}x faster</span>'
    else:
        color = "#f85149"
        return f'<span style="color:{color}">{1/ratio:.1f}x slower</span>'

def generate_html(results, out_path):
    total = len(results)
    passed = sum(1 for r in results if r["status"] == "pass")
    failed = sum(1 for r in results if r["status"] == "fail")
    skipped = sum(1 for r in results if r["status"] in ("both_skip", "go_error", "rust_error"))
    pct = (passed / total * 100) if total else 0
    NCOLS = 15

    cats = defaultdict(list)
    for r in results:
        cats[r["category"]].append(r)

    # Per-category summary
    summary_rows = []
    for cat in sorted(cats):
        entries = cats[cat]
        go_warm = [e["go_warm_us"] for e in entries if e["go_warm_us"] > 0]
        rust_warm = [e["rust_warm_us"] for e in entries if e["rust_warm_us"] > 0]
        python_warm = [e["python_warm_us"] for e in entries if e["python_warm_us"] > 0]

        go_w_avg = sum(go_warm) / len(go_warm) if go_warm else 0
        rust_w_avg = sum(rust_warm) / len(rust_warm) if rust_warm else 0
        python_w_avg = sum(python_warm) / len(python_warm) if python_warm else 0

        go_cold = [e["go_ms"] for e in entries if e["go_ms"] > 0]
        rust_cold = [e["rust_ms"] for e in entries if e["rust_ms"] > 0]
        python_cold = [e["python_ms"] for e in entries if e["python_ms"] > 0]
        go_c_avg = sum(go_cold) / len(go_cold) if go_cold else 0
        rust_c_avg = sum(rust_cold) / len(rust_cold) if rust_cold else 0
        python_c_avg = sum(python_cold) / len(python_cold) if python_cold else 0

        n_pass = sum(1 for e in entries if e["status"] == "pass")
        n_total = len(entries)
        parity_badge = f'<span style="color:#3fb950">{n_pass}/{n_total}</span>' if n_pass == n_total else f'<span style="color:#f85149">{n_pass}/{n_total}</span>'

        summary_rows.append(
            f'<tr>'
            f'<td><strong>{escape(cat)}</strong></td>'
            f'<td class="mono r">{parity_badge}</td>'
            f'<td class="mono r go">{fmt_us(go_w_avg)}</td>'
            f'<td class="mono r go">{go_c_avg:.1f}ms</td>'
            f'<td class="mono r rust">{fmt_us(rust_w_avg)}</td>'
            f'<td class="mono r rust">{rust_c_avg:.1f}ms</td>'
            f'<td class="mono r python">{fmt_us(python_w_avg)}</td>'
            f'<td class="mono r python">{python_c_avg:.1f}ms</td>'
            f'<td class="mono r">{fmt_ratio(go_w_avg, rust_w_avg)}</td>'
            f'<td class="mono r">{fmt_ratio(go_w_avg, python_w_avg)}</td>'
            f'</tr>'
        )

    rows = []
    for cat in sorted(cats):
        entries = cats[cat]
        cat_pass = sum(1 for e in entries if e["status"] == "pass")
        cat_total = len(entries)
        cat_color = "#3fb950" if cat_pass == cat_total else "#f85149"
        rows.append(f'''<tr class="cat-row"><td colspan="{NCOLS}"><strong>{escape(cat)}</strong> <span style="color:{cat_color};font-size:0.8rem">{cat_pass}/{cat_total} pass</span></td></tr>''')

        for e in sorted(entries, key=lambda x: x["fixture"]):
            s = e["status"]
            icon = {"pass": "&#10003;", "fail": "&#10007;", "both_skip": "&#8212;", "go_error": "&#9888;", "rust_error": "&#9888;"}.get(s, "?")
            color = {"pass": "#3fb950", "fail": "#f85149"}.get(s, "#8b949e")

            vs_rust = fmt_ratio(e["go_warm_us"], e["rust_warm_us"])
            vs_python = fmt_ratio(e["go_warm_us"], e["python_warm_us"])

            detail_id = e["fixture"].replace(".", "_")

            rows.append(f'''<tr class="fix-row toggle" data-status="{s}" onclick="toggleDetail('{detail_id}')">
<td><span style="color:{color}">{icon}</span></td>
<td class="mono fname">{escape(e["fixture"][:40])}</td>
<td class="mono" style="font-size:.65rem;color:#8b949e">{escape(e["compared_to"])}</td>
<td class="ghcol mono r">{e["go_bytes"]}</td>
<td class="ghcol mono r go">{fmt_us(e["go_warm_us"])}</td>
<td class="ghcol mono r go">{e["go_ms"]:.1f}ms</td>
<td class="hrcol mono r">{e["rust_bytes"]}</td>
<td class="hrcol mono r rust">{fmt_us(e["rust_warm_us"])}</td>
<td class="hrcol mono r rust">{e["rust_ms"]:.1f}ms</td>
<td class="hrcol mono r">{e["python_bytes"]}</td>
<td class="hrcol mono r python">{fmt_us(e["python_warm_us"])}</td>
<td class="hrcol mono r python">{e["python_ms"]:.1f}ms</td>
<td class="mono r">{vs_rust}</td>
<td class="mono r">{vs_python}</td>
</tr>''')

            # Expandable detail row with input + outputs
            inp_html = escape(e.get("fixture_input", "")[:2000])
            go_html = escape(e.get("go_out", "")[:2000])
            rust_html = escape(e.get("rust_out", "")[:2000])
            python_html = escape(e.get("python_out", "")[:2000])

            cold_info = (
                f'<span class="go">Go {e["go_ms"]:.1f}ms</span>'
                f' &middot; <span class="rust">Rust {e["rust_ms"]:.1f}ms</span>'
                f' &middot; <span class="python">Python {e["python_ms"]:.1f}ms</span>'
            )

            rows.append(f'''<tr class="detail-row hidden" id="detail_{detail_id}"><td colspan="{NCOLS}">
<div class="detail-box">
<div class="detail-meta">Cold startup: {cold_info}</div>
<div class="detail-grid">
<div class="detail-panel">
<div class="detail-label">Input</div>
<pre class="detail-pre">{inp_html}</pre>
</div>
<div class="detail-panel go-panel">
<div class="detail-label go">goheadroom output <span class="detail-bytes">{e["go_bytes"]} bytes</span></div>
<pre class="detail-pre">{go_html}</pre>
</div>
<div class="detail-panel rust-panel">
<div class="detail-label rust">Rust output <span class="detail-bytes">{e["rust_bytes"]} bytes</span></div>
<pre class="detail-pre">{rust_html}</pre>
</div>
<div class="detail-panel python-panel">
<div class="detail-label python">Python output <span class="detail-bytes">{e["python_bytes"]} bytes</span></div>
<pre class="detail-pre">{python_html}</pre>
</div>
</div>
</div>
</td></tr>''')

    html = f'''<!DOCTYPE html>
<html lang="en"><head><meta charset="UTF-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>goheadroom Parity Report</title>
<style>
*{{margin:0;padding:0;box-sizing:border-box}}
body{{font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',system-ui,sans-serif;background:#0d1117;color:#c9d1d9}}
.wrap{{max-width:1600px;margin:0 auto;padding:2rem}}
h1{{font-size:2rem;color:#f0f6fc;margin-bottom:.3rem}}
h2{{font-size:1.2rem;color:#f0f6fc;margin:1.5rem 0 .8rem}}
.sub{{color:#8b949e;margin-bottom:1.5rem}}
.hero{{display:flex;align-items:center;gap:2rem;margin:1.5rem 0;flex-wrap:wrap}}
.ring{{width:150px;height:150px;border-radius:50%;display:flex;align-items:center;justify-content:center;flex-shrink:0}}
.ring-inner{{width:120px;height:120px;border-radius:50%;background:#0d1117;display:flex;align-items:center;justify-content:center;flex-direction:column}}
.pct{{font-size:2.8rem;font-weight:800;color:#f0f6fc}}
.pct-lbl{{font-size:.8rem;color:#8b949e}}
.cards{{display:flex;gap:1rem;flex-wrap:wrap}}
.card{{background:#161b22;border:1px solid #30363d;border-radius:8px;padding:1rem 1.5rem;text-align:center;min-width:100px}}
.card-v{{font-size:1.6rem;font-weight:700}}.card-l{{font-size:.75rem;color:#8b949e;margin-top:.2rem}}
.pass{{color:#3fb950}}.fail{{color:#f85149}}.skip-c{{color:#8b949e}}
.filters{{margin:1.5rem 0;display:flex;gap:.5rem}}
.fbtn{{background:#21262d;border:1px solid #30363d;color:#c9d1d9;padding:.4rem 1rem;border-radius:6px;cursor:pointer;font-size:.85rem}}
.fbtn.on{{background:#388bfd20;border-color:#388bfd;color:#58a6ff}}
table{{width:100%;border-collapse:collapse}}
th{{background:#161b22;padding:.5rem .6rem;text-align:left;font-size:.65rem;color:#8b949e;text-transform:uppercase;letter-spacing:.04em;border-bottom:1px solid #30363d;position:sticky;z-index:2}}
th.r{{text-align:right}}
td{{padding:.35rem .6rem;border-bottom:1px solid #21262d;font-size:.78rem}}
td.r{{text-align:right}}
.mono{{font-family:'SF Mono','Fira Code',monospace;font-size:.72rem}}
.fname{{max-width:220px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}}
.cat-row td{{background:#161b22;padding:.5rem .6rem;border-bottom:1px solid #30363d}}
.toggle{{cursor:pointer}}.toggle:hover td{{background:#161b2280}}
.hidden{{display:none}}
tr.fhide{{display:none}}
td.go,.go{{color:#8db9e8}}
td.rust,.rust{{color:#f0a870}}
td.python,.python{{color:#c0a0e0}}
th.go{{color:#00add8!important}}
th.rust{{color:#f97316!important}}
th.python{{color:#a855f7!important}}
.summary-table{{width:100%;margin-bottom:1.5rem}}
.summary-table td,.summary-table th{{padding:.5rem .8rem}}
.th-group{{text-align:center!important;font-size:.7rem;padding:.4rem .6rem;font-weight:700}}
.th-group-gh{{border-bottom:3px solid #00add8;color:#00add8}}
.th-group-hr{{border-bottom:3px solid #f97316;color:#f0a870}}
.th-sub{{text-align:center!important;font-size:.6rem;padding:.2rem .6rem;font-weight:600}}
.th-sub-rust{{border-bottom:2px solid #f9731640;color:#f97316}}
.th-sub-python{{border-bottom:2px solid #a855f740;color:#a855f7}}
.ghcol{{background:#00add80a}}
.hrcol{{background:#f9731606}}
.detail-row td{{padding:0;border-bottom:1px solid #30363d}}
.detail-box{{background:#0d1117;border:1px solid #30363d;border-radius:6px;margin:.2rem .5rem .6rem;padding:1rem}}
.detail-meta{{font-size:.75rem;color:#8b949e;margin-bottom:.8rem}}
.detail-grid{{display:grid;grid-template-columns:1fr 1fr 1fr 1fr;gap:.6rem}}
.detail-panel{{background:#161b22;border:1px solid #21262d;border-radius:4px;overflow:hidden}}
.detail-label{{font-size:.7rem;font-weight:600;padding:.4rem .6rem;background:#21262d;border-bottom:1px solid #30363d;color:#8b949e}}
.detail-label.go{{color:#00add8}}.detail-label.rust{{color:#f97316}}.detail-label.python{{color:#a855f7}}
.detail-bytes{{font-weight:400;opacity:.7;margin-left:.5rem}}
.detail-pre{{font-family:'SF Mono','Fira Code',monospace;font-size:.7rem;padding:.5rem .6rem;max-height:250px;overflow:auto;white-space:pre-wrap;word-break:break-all;line-height:1.4;color:#c9d1d9;margin:0}}
@media(max-width:1000px){{.detail-grid{{grid-template-columns:1fr}}}}
</style></head><body><div class="wrap">
<h1>goheadroom Parity Report</h1>
<p class="sub">goheadroom vs headroom &mdash; parity and benchmark comparison across {total} fixtures.<br>
Compares Go to Rust (underlying implementation) and Python (native reference).</p>

<div class="hero">
<div class="ring" style="background:conic-gradient(#3fb950 0% {pct}%,#30363d {pct}% 100%)">
<div class="ring-inner"><div class="pct">{pct:.0f}%</div><div class="pct-lbl">parity</div></div></div>
<div class="cards">
<div class="card"><div class="card-v pass">{passed}</div><div class="card-l">Pass</div></div>
<div class="card"><div class="card-v fail">{failed}</div><div class="card-l">Fail</div></div>
<div class="card"><div class="card-v skip-c">{skipped}</div><div class="card-l">Skip</div></div>
<div class="card"><div class="card-v" style="color:#f0f6fc">{total}</div><div class="card-l">Total</div></div>
</div></div>

<h2>Summary by Transform</h2>
<table class="summary-table"><thead>
<tr>
<th rowspan="2">Transform</th>
<th rowspan="2" class="r">Parity</th>
<th colspan="2" class="th-group th-group-gh">goheadroom</th>
<th colspan="4" class="th-group th-group-hr">headroom</th>
<th colspan="2" rowspan="2" style="text-align:center;font-size:.65rem;border-bottom:1px solid #30363d">Comparison</th>
</tr>
<tr>
<th class="r go" style="font-size:.6rem">Warm</th><th class="r go" style="font-size:.6rem">Cold</th>
<th class="r rust th-sub th-sub-rust" colspan="2">Rust</th>
<th class="r python th-sub th-sub-python" colspan="2">Python</th>
</tr>
<tr style="font-size:.55rem">
<th></th><th></th>
<th class="r go"></th><th class="r go"></th>
<th class="r rust">Warm</th><th class="r rust">Cold</th>
<th class="r python">Warm</th><th class="r python">Cold</th>
<th class="r">vs Rust</th><th class="r">vs Python</th>
</tr>
</thead><tbody>
{"".join(summary_rows)}
</tbody></table>

<h2>Per-Fixture Results</h2>
<p style="color:#8b949e;font-size:.8rem;margin-bottom:.8rem">Click any row to see input and output from all three implementations.</p>

<div class="filters">
<button class="fbtn on" onclick="filt('all',this)">All ({total})</button>
<button class="fbtn" onclick="filt('pass',this)">Pass ({passed})</button>
<button class="fbtn" onclick="filt('fail',this)">Fail ({failed})</button>
<button class="fbtn" onclick="filt('skip',this)">Skip ({skipped})</button>
</div>

<table><thead>
<tr>
<th rowspan="2"></th><th rowspan="2">Fixture</th><th rowspan="2">Compared to</th>
<th colspan="3" class="th-group th-group-gh">goheadroom</th>
<th colspan="6" class="th-group th-group-hr">headroom</th>
<th colspan="2" rowspan="2" style="text-align:center;font-size:.65rem;border-bottom:1px solid #30363d">Comparison</th>
</tr>
<tr>
<th class="r go" style="font-size:.6rem">Bytes</th><th class="r go" style="font-size:.6rem">Warm</th><th class="r go" style="font-size:.6rem">Cold</th>
<th class="r rust th-sub th-sub-rust">Bytes</th><th class="r rust th-sub th-sub-rust">Warm</th><th class="r rust th-sub th-sub-rust">Cold</th>
<th class="r python th-sub th-sub-python">Bytes</th><th class="r python th-sub th-sub-python">Warm</th><th class="r python th-sub th-sub-python">Cold</th>
<th class="r" style="font-size:.6rem">vs Rust</th><th class="r" style="font-size:.6rem">vs Python</th>
</tr></thead><tbody>
{"".join(rows)}
</tbody></table>
</div>
<script>
document.querySelectorAll('thead').forEach(thead=>{{
const rows=thead.querySelectorAll('tr');
let top=0;
rows.forEach(r=>{{
r.querySelectorAll('th').forEach(th=>{{th.style.top=top+'px'}});
top+=r.offsetHeight;
}});
}});
function filt(f,btn){{document.querySelectorAll('.fbtn').forEach(b=>b.classList.remove('on'));btn.classList.add('on');
document.querySelectorAll('.fix-row').forEach(r=>{{const s=r.dataset.status;
if(f==='all')r.classList.remove('fhide');
else if(f==='pass')r.classList.toggle('fhide',s!=='pass');
else if(f==='fail')r.classList.toggle('fhide',s!=='fail');
else if(f==='skip')r.classList.toggle('fhide',!['both_skip','go_error','rust_error'].includes(s));
}});document.querySelectorAll('.detail-row').forEach(r=>r.classList.add('hidden'))}}
function toggleDetail(id){{const r=document.getElementById('detail_'+id);if(r)r.classList.toggle('hidden')}}
</script></body></html>'''

    with open(out_path, "w") as f:
        f.write(html)
    print(f"\nReport: {out_path}", file=sys.stderr)
    print(f"{passed}/{total} pass ({pct:.0f}%), {failed} fail, {skipped} skip", file=sys.stderr)

if __name__ == "__main__":
    main()
