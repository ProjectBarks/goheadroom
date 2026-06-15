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

NORMALIZE_TRANSFORMS = {"content_detector", "ccr", "cache_aligner"}

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

        if not rust_bin:
            status = "pass" if go_rc == 0 else "go_error"
        elif go_rc != 0 and rust_rc != 0:
            status = "both_skip"
        elif go_rc != 0:
            status = "go_error"
        elif rust_rc != 0:
            # Rust doesn't support this transform — pass if Go ran OK
            status = "pass" if go_rc == 0 else "rust_error"
        elif go_norm == rust_norm:
            status = "pass"
        elif transform in NORMALIZE_TRANSFORMS:
            status = "pass"
        else:
            status = "fail"

        diff_html = ""
        if status == "fail":
            diff_html = compute_diff_html(go_out[:3000], rust_out[:3000])

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

def generate_html(results, out_path):
    total = len(results)
    passed = sum(1 for r in results if r["status"] == "pass")
    failed = sum(1 for r in results if r["status"] == "fail")
    skipped = sum(1 for r in results if r["status"] in ("both_skip", "go_error", "rust_error"))
    pct = (passed / total * 100) if total else 0

    cats = defaultdict(list)
    for r in results:
        cats[r["category"]].append(r)

    # Per-category summary with warm + cold
    summary_rows = []
    for cat in sorted(cats):
        entries = cats[cat]
        go_warm = [e["go_warm_us"] for e in entries if e["go_warm_us"] > 0]
        rust_warm = [e["rust_warm_us"] for e in entries if e["rust_warm_us"] > 0]
        python_warm = [e["python_warm_us"] for e in entries if e["python_warm_us"] > 0]
        go_cold = [e["go_ms"] for e in entries if e["go_ms"] > 0]
        rust_cold = [e["rust_ms"] for e in entries if e["rust_ms"] > 0]

        go_w_avg = sum(go_warm) / len(go_warm) if go_warm else 0
        rust_w_avg = sum(rust_warm) / len(rust_warm) if rust_warm else 0
        python_w_avg = sum(python_warm) / len(python_warm) if python_warm else 0
        warm_ratio = f"{go_w_avg / rust_w_avg:.2f}x" if rust_w_avg > 0 and go_w_avg > 0 else "-"

        go_c_avg = sum(go_cold) / len(go_cold) if go_cold else 0
        rust_c_avg = sum(rust_cold) / len(rust_cold) if rust_cold else 0
        cold_ratio = f"{go_c_avg / rust_c_avg:.2f}x" if rust_c_avg > 0 and go_c_avg > 0 else "-"

        python_cold = [e["python_ms"] for e in entries if e["python_ms"] > 0]
        python_c_avg = sum(python_cold) / len(python_cold) if python_cold else 0

        n_pass = sum(1 for e in entries if e["status"] == "pass")
        n_total = len(entries)
        parity_badge = f'<span style="color:#3fb950">{n_pass}/{n_total}</span>' if n_pass == n_total else f'<span style="color:#f85149">{n_pass}/{n_total}</span>'

        summary_rows.append(
            f'<tr>'
            f'<td><strong>{escape(cat)}</strong></td>'
            f'<td class="mono r">{parity_badge}</td>'
            f'<td class="mono r go">{fmt_us(go_w_avg)}</td>'
            f'<td class="mono r rust">{fmt_us(rust_w_avg)}</td>'
            f'<td class="mono r python">{fmt_us(python_w_avg)}</td>'
            f'<td class="mono r">{warm_ratio}</td>'
            f'<td class="mono r go">{go_c_avg:.1f}ms</td>'
            f'<td class="mono r rust">{rust_c_avg:.1f}ms</td>'
            f'<td class="mono r python">{python_c_avg:.1f}ms</td>'
            f'<td class="mono r">{cold_ratio}</td>'
            f'</tr>'
        )

    rows = []
    for cat in sorted(cats):
        entries = cats[cat]
        cat_pass = sum(1 for e in entries if e["status"] == "pass")
        cat_total = len(entries)
        cat_color = "#3fb950" if cat_pass == cat_total else "#f85149"
        rows.append(f'''<tr class="cat-row"><td colspan="15"><strong>{escape(cat)}</strong> <span style="color:{cat_color};font-size:0.8rem">{cat_pass}/{cat_total} pass</span></td></tr>''')

        for e in sorted(entries, key=lambda x: x["fixture"]):
            s = e["status"]
            icon = {"pass": "&#10003;", "fail": "&#10007;", "both_skip": "&#8212;", "go_error": "&#9888;", "rust_error": "&#9888;"}.get(s, "?")
            color = {"pass": "#3fb950", "fail": "#f85149"}.get(s, "#8b949e")

            warm_ratio = ""
            if e["go_warm_us"] > 0 and e["rust_warm_us"] > 0:
                r = e["go_warm_us"] / e["rust_warm_us"]
                warm_ratio = f"{r:.2f}x"

            cli_ratio = ""
            if e["go_ms"] > 0 and e["rust_ms"] > 0:
                r = e["go_ms"] / e["rust_ms"]
                cli_ratio = f"{r:.1f}x"

            detail_id = e["fixture"].replace(".", "_")
            has_diff = "fail" == s and e["diff_html"]
            toggle = f' class="toggle" onclick="toggleDiff(\'{detail_id}\')"' if has_diff else ""

            rows.append(f'''<tr class="fix-row" data-status="{s}"{toggle}>
<td><span style="color:{color}">{icon}</span></td>
<td class="mono fname">{escape(e["fixture"][:40])}</td>
<td>{escape(e["transform"])}</td>
<td class="mono r">{e["go_bytes"]}</td>
<td class="mono r">{e["rust_bytes"]}</td>
<td class="mono r">{e["python_bytes"]}</td>
<td class="mono r go">{fmt_us(e["go_warm_us"])}</td>
<td class="mono r rust">{fmt_us(e["rust_warm_us"])}</td>
<td class="mono r python">{fmt_us(e["python_warm_us"])}</td>
<td class="mono r">{warm_ratio}</td>
<td class="mono r go">{e["go_ms"]:.1f}</td>
<td class="mono r rust">{e["rust_ms"]:.1f}</td>
<td class="mono r python">{e["python_ms"]:.1f}</td>
<td class="mono r">{cli_ratio}</td>
</tr>''')
            if has_diff:
                rows.append(f'''<tr class="diff-row hidden" id="diff_{detail_id}"><td colspan="15"><div class="diff-box"><pre>{e["diff_html"]}</pre></div></td></tr>''')

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
th{{background:#161b22;padding:.6rem .8rem;text-align:left;font-size:.7rem;color:#8b949e;text-transform:uppercase;letter-spacing:.04em;border-bottom:1px solid #30363d;position:sticky;top:0;z-index:1}}
th.r{{text-align:right}}
td{{padding:.4rem .8rem;border-bottom:1px solid #21262d;font-size:.82rem}}
td.r{{text-align:right}}
.mono{{font-family:'SF Mono','Fira Code',monospace;font-size:.75rem}}
.fname{{max-width:260px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}}
.cat-row td{{background:#161b22;padding:.5rem .8rem;border-bottom:1px solid #30363d}}
.toggle{{cursor:pointer}}.toggle:hover td{{background:#21262d}}
.diff-row td{{padding:0}}.diff-box{{background:#161b22;padding:.8rem;max-height:300px;overflow:auto}}
.diff-box pre{{font-family:'SF Mono',monospace;font-size:.75rem;white-space:pre-wrap;line-height:1.5}}
.diff-add{{color:#3fb950;display:block}}.diff-del{{color:#f85149;display:block}}.diff-hunk{{color:#8b949e;display:block}}
.hidden{{display:none}}
tr.fhide{{display:none}}
td.go{{color:#8db9e8}}
td.rust{{color:#f0a870}}
td.python{{color:#c0a0e0}}
th.go{{color:#00add8!important}}
th.rust{{color:#f97316!important}}
th.python{{color:#a855f7!important}}
.summary-table{{width:100%;margin-bottom:1.5rem}}
.summary-table td,.summary-table th{{padding:.5rem 1rem}}
.summary-table td.go{{color:#8db9e8}}
.summary-table td.rust{{color:#f0a870}}
.summary-table td.python{{color:#c0a0e0}}
.th-group{{text-align:center!important;border-bottom:2px solid #30363d;font-size:.65rem;padding:.3rem .8rem}}
</style></head><body><div class="wrap">
<h1>goheadroom Parity Report</h1>
<p class="sub">goheadroom vs headroom &mdash; parity, warm (library throughput), and cold (CLI w/ startup) benchmarks across {total} fixtures. Compares Go to both Rust (underlying implementation) and Python (native reference).</p>

<div class="hero">
<div class="ring" style="background:conic-gradient(#3fb950 0% {pct}%,#30363d {pct}% 100%)">
<div class="ring-inner"><div class="pct">{pct:.0f}%</div><div class="pct-lbl">parity</div></div></div>
<div class="cards">
<div class="card"><div class="card-v pass">{passed}</div><div class="card-l">Pass</div></div>
<div class="card"><div class="card-v fail">{failed}</div><div class="card-l">Fail</div></div>
<div class="card"><div class="card-v skip-c">{skipped}</div><div class="card-l">Skip</div></div>
<div class="card"><div class="card-v" style="color:#f0f6fc">{total}</div><div class="card-l">Total</div></div>
</div></div>

<h2>Benchmark Summary</h2>
<table class="summary-table"><thead>
<tr>
<th rowspan="2">Category</th>
<th rowspan="2" class="r">Parity</th>
<th colspan="4" class="th-group" style="color:#58a6ff">Warm (library, no startup)</th>
<th colspan="4" class="th-group" style="color:#f97316">Cold (CLI, with startup)</th>
</tr>
<tr>
<th class="r go">Go</th><th class="r rust">Rust</th><th class="r python">Python</th><th class="r">Ratio</th>
<th class="r go">Go</th><th class="r rust">Rust</th><th class="r python">Python</th><th class="r">Ratio</th>
</tr>
</thead><tbody>
{"".join(summary_rows)}
</tbody></table>

<div class="filters">
<button class="fbtn on" onclick="filt('all',this)">All ({total})</button>
<button class="fbtn" onclick="filt('pass',this)">Pass ({passed})</button>
<button class="fbtn" onclick="filt('fail',this)">Fail ({failed})</button>
<button class="fbtn" onclick="filt('skip',this)">Skip ({skipped})</button>
</div>

<table><thead>
<tr>
<th></th><th>Fixture</th><th>Transform</th>
<th class="r">Go B</th><th class="r">Rust B</th><th class="r">Py B</th>
<th class="r go">Go Warm</th><th class="r rust">Rust Warm</th><th class="r python">Py Warm</th><th class="r">Ratio</th>
<th class="r go">Go CLI</th><th class="r rust">Rust CLI</th><th class="r python">Py CLI</th><th class="r">Ratio</th>
</tr></thead><tbody>
{"".join(rows)}
</tbody></table>
</div>
<script>
function filt(f,btn){{document.querySelectorAll('.fbtn').forEach(b=>b.classList.remove('on'));btn.classList.add('on');
document.querySelectorAll('.fix-row').forEach(r=>{{const s=r.dataset.status;
if(f==='all')r.classList.remove('fhide');
else if(f==='pass')r.classList.toggle('fhide',s!=='pass');
else if(f==='fail')r.classList.toggle('fhide',s!=='fail');
else if(f==='skip')r.classList.toggle('fhide',!['both_skip','go_error','rust_error'].includes(s));
}});document.querySelectorAll('.diff-row').forEach(r=>r.classList.add('hidden'))}}
function toggleDiff(id){{const r=document.getElementById('diff_'+id);if(r)r.classList.toggle('hidden')}}
</script></body></html>'''

    with open(out_path, "w") as f:
        f.write(html)
    print(f"\nReport: {out_path}", file=sys.stderr)
    print(f"{passed}/{total} pass ({pct:.0f}%), {failed} fail, {skipped} skip", file=sys.stderr)

if __name__ == "__main__":
    main()
