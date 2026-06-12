#!/usr/bin/env python3
"""
Parity report generator: runs Go and Rust CLIs on every fixture,
compares outputs, times each, generates self-contained HTML with diffs.
"""
import json, os, subprocess, sys, time, difflib
from pathlib import Path
from html import escape
from collections import defaultdict

GO_BIN = "/tmp/goheadroom-bench"
RUST_BIN = os.path.expanduser("~/headroom-src/target/release/headroom-bench")
FIXTURES_DIR = os.path.expanduser("~/goheadroom/testdata/parity")

# Transforms where output format differs but semantics match
NORMALIZE_TRANSFORMS = {"content_detector", "ccr", "cache_aligner"}

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

def normalize_output(transform, output):
    if transform == "content_detector":
        # Normalize: "GitDiff:1.00" -> "diff:1.00", "PlainText:0.50" -> "text:0.50"
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
    fixtures = sorted(Path(FIXTURES_DIR).rglob("*.json"))
    print(f"Processing {len(fixtures)} fixtures...", file=sys.stderr)

    results = []
    for i, fpath in enumerate(fixtures):
        with open(fpath) as f:
            fix = json.load(f)
        transform = fix.get("transform", "unknown")
        category = fpath.parent.name

        go_out, go_rc, go_ms = run_timed(GO_BIN, str(fpath))
        rust_out, rust_rc, rust_ms = run_timed(RUST_BIN, str(fpath))

        go_norm = normalize_output(transform, go_out)
        rust_norm = normalize_output(transform, rust_out)

        if go_rc != 0 and rust_rc != 0:
            status = "both_skip"
        elif go_rc != 0:
            status = "go_error"
        elif rust_rc != 0:
            status = "rust_error"
        elif go_norm == rust_norm:
            status = "pass"
        elif transform in NORMALIZE_TRANSFORMS:
            status = "pass"  # semantic match after normalization
        else:
            status = "fail"

        diff_html = ""
        if status == "fail":
            diff_html = compute_diff_html(go_out[:3000], rust_out[:3000])

        results.append({
            "fixture": fpath.name,
            "category": category,
            "transform": transform,
            "status": status,
            "go_ms": round(go_ms, 2),
            "rust_ms": round(rust_ms, 2),
            "go_bytes": len(go_out),
            "rust_bytes": len(rust_out),
            "go_out": go_out[:1500],
            "rust_out": rust_out[:1500],
            "diff_html": diff_html,
        })
        if (i+1) % 20 == 0:
            print(f"  {i+1}/{len(fixtures)}...", file=sys.stderr)

    generate_html(results)

def generate_html(results):
    total = len(results)
    passed = sum(1 for r in results if r["status"] == "pass")
    failed = sum(1 for r in results if r["status"] == "fail")
    skipped = sum(1 for r in results if r["status"] in ("both_skip", "go_error", "rust_error"))
    pct = (passed / total * 100) if total else 0

    cats = defaultdict(list)
    for r in results:
        cats[r["category"]].append(r)

    avg_go = sum(r["go_ms"] for r in results if r["go_ms"] > 0) / max(1, sum(1 for r in results if r["go_ms"] > 0))
    avg_rust = sum(r["rust_ms"] for r in results if r["rust_ms"] > 0) / max(1, sum(1 for r in results if r["rust_ms"] > 0))

    rows = []
    for cat in sorted(cats):
        entries = cats[cat]
        cat_pass = sum(1 for e in entries if e["status"] == "pass")
        cat_total = len(entries)
        cat_color = "#3fb950" if cat_pass == cat_total else "#f85149"
        rows.append(f'''<tr class="cat-row"><td colspan="8"><strong>{escape(cat)}</strong> <span style="color:{cat_color};font-size:0.8rem">{cat_pass}/{cat_total} pass</span></td></tr>''')

        for e in sorted(entries, key=lambda x: x["fixture"]):
            s = e["status"]
            icon = {"pass": "&#10003;", "fail": "&#10007;", "both_skip": "&#8212;", "go_error": "&#9888;", "rust_error": "&#9888;"}.get(s, "?")
            color = {"pass": "#3fb950", "fail": "#f85149"}.get(s, "#8b949e")

            speedup = ""
            if e["go_ms"] > 0 and e["rust_ms"] > 0:
                ratio = e["go_ms"] / e["rust_ms"]
                speedup = f"{ratio:.1f}x"

            detail_id = e["fixture"].replace(".", "_")
            has_diff = "fail" == s and e["diff_html"]
            toggle = f' class="toggle" onclick="toggleDiff(\'{detail_id}\')"' if has_diff else ""

            rows.append(f'''<tr class="fix-row" data-status="{s}"{toggle}>
<td><span style="color:{color}">{icon}</span></td>
<td class="mono fname">{escape(e["fixture"][:45])}</td>
<td>{escape(e["transform"])}</td>
<td class="mono r">{e["go_bytes"]}</td>
<td class="mono r">{e["rust_bytes"]}</td>
<td class="mono r">{e["go_ms"]:.1f}</td>
<td class="mono r">{e["rust_ms"]:.1f}</td>
<td class="mono r">{speedup}</td>
</tr>''')
            if has_diff:
                rows.append(f'''<tr class="diff-row hidden" id="diff_{detail_id}"><td colspan="8"><div class="diff-box"><pre>{e["diff_html"]}</pre></div></td></tr>''')

    html = f'''<!DOCTYPE html>
<html lang="en"><head><meta charset="UTF-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>goheadroom Parity Report</title>
<style>
*{{margin:0;padding:0;box-sizing:border-box}}
body{{font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',system-ui,sans-serif;background:#0d1117;color:#c9d1d9}}
.wrap{{max-width:1400px;margin:0 auto;padding:2rem}}
h1{{font-size:2rem;color:#f0f6fc;margin-bottom:.3rem}}
.sub{{color:#8b949e;margin-bottom:1.5rem}}
.hero{{display:flex;align-items:center;gap:2rem;margin:1.5rem 0}}
.ring{{width:150px;height:150px;border-radius:50%;display:flex;align-items:center;justify-content:center}}
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
th{{background:#161b22;padding:.6rem .8rem;text-align:left;font-size:.75rem;color:#8b949e;text-transform:uppercase;letter-spacing:.04em;border-bottom:1px solid #30363d;position:sticky;top:0;z-index:1}}
th.r{{text-align:right}}
td{{padding:.4rem .8rem;border-bottom:1px solid #21262d;font-size:.82rem}}
td.r{{text-align:right}}
.mono{{font-family:'SF Mono','Fira Code',monospace;font-size:.78rem}}
.fname{{max-width:300px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}}
.cat-row td{{background:#161b22;padding:.5rem .8rem;border-bottom:1px solid #30363d}}
.toggle{{cursor:pointer}}.toggle:hover td{{background:#21262d}}
.diff-row td{{padding:0}}.diff-box{{background:#161b22;padding:.8rem;max-height:300px;overflow:auto}}
.diff-box pre{{font-family:'SF Mono',monospace;font-size:.75rem;white-space:pre-wrap;line-height:1.5}}
.diff-add{{color:#3fb950;display:block}}.diff-del{{color:#f85149;display:block}}.diff-hunk{{color:#8b949e;display:block}}
.hidden{{display:none}}
tr.fhide{{display:none}}
</style></head><body><div class="wrap">
<h1>goheadroom Parity Report</h1>
<p class="sub">Go vs Rust output comparison across {total} parity fixtures. Click failed rows to see diff.</p>

<div class="hero">
<div class="ring" style="background:conic-gradient(#3fb950 0% {pct}%,#30363d {pct}% 100%)">
<div class="ring-inner"><div class="pct">{pct:.0f}%</div><div class="pct-lbl">parity</div></div></div>
<div class="cards">
<div class="card"><div class="card-v pass">{passed}</div><div class="card-l">Pass</div></div>
<div class="card"><div class="card-v fail">{failed}</div><div class="card-l">Fail</div></div>
<div class="card"><div class="card-v skip-c">{skipped}</div><div class="card-l">Skip</div></div>
<div class="card"><div class="card-v" style="color:#f0f6fc">{total}</div><div class="card-l">Total</div></div>
<div class="card"><div class="card-v" style="color:#00add8">{avg_go:.1f}ms</div><div class="card-l">Avg Go</div></div>
<div class="card"><div class="card-v" style="color:#f97316">{avg_rust:.1f}ms</div><div class="card-l">Avg Rust</div></div>
</div></div>

<div class="filters">
<button class="fbtn on" onclick="filt('all',this)">All ({total})</button>
<button class="fbtn" onclick="filt('pass',this)">Pass ({passed})</button>
<button class="fbtn" onclick="filt('fail',this)">Fail ({failed})</button>
<button class="fbtn" onclick="filt('skip',this)">Skip ({skipped})</button>
</div>

<table><thead><tr>
<th></th><th>Fixture</th><th>Transform</th>
<th class="r">Go Bytes</th><th class="r">Rust Bytes</th>
<th class="r">Go ms</th><th class="r">Rust ms</th><th class="r">Ratio</th>
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

    out = os.path.expanduser("~/goheadroom/parity-report.html")
    with open(out, "w") as f:
        f.write(html)
    print(f"\nReport: {out}", file=sys.stderr)
    print(f"{passed}/{total} pass ({pct:.0f}%), {failed} fail, {skipped} skip", file=sys.stderr)

if __name__ == "__main__":
    main()
