#!/usr/bin/env python3
"""Generate E2E parity fixtures for mutated and unmutated pipeline cases.

Constructs realistic payloads, runs Python's content detection,
and (for mutated cases) runs the Rust bench binary to get SOT compressed output.
"""
import json
import os
import hashlib
import subprocess
import sys
from pathlib import Path

SCRIPT_DIR = Path(__file__).resolve().parent
PROJECT_DIR = SCRIPT_DIR.parent

sys.path.insert(0, os.environ.get("HEADROOM_PATH", "/tmp/headroom-chopratejas"))
from headroom.transforms.content_detector import detect_content_type

RUST_BENCH = os.path.expanduser("~/headroom-src/target/release/headroom-bench")
UNMUTATED_DIR = PROJECT_DIR / "testdata" / "parity" / "e2e_unmutated"
MUTATED_DIR = PROJECT_DIR / "testdata" / "parity" / "e2e_mutated"


def sha_name(text):
    return hashlib.sha256(text.encode()).hexdigest()[:16] + ".json"


def detect(text):
    r = detect_content_type(text)
    return r.content_type.value, r.confidence


def rust_compress(transform, text, config=None):
    """Run the Rust bench binary on a temporary fixture to get SOT output."""
    if transform == "smart_crusher":
        inp = {"content": text, "query": "", "bias": 0.5}
    else:
        inp = text
    fix = {"transform": transform, "input": inp, "output": {}, "config": config or {}}
    tmp = "/tmp/_e2e_fixture.json"
    with open(tmp, "w") as f:
        json.dump(fix, f)
    try:
        r = subprocess.run([RUST_BENCH, tmp], capture_output=True, text=True, timeout=10)
        if r.returncode == 0 and r.stdout:
            return r.stdout, True
        return r.stderr[:200] if r.stderr else f"rc={r.returncode}", False
    except Exception as e:
        return str(e), False


# ============================================================
# UNMUTATED CASES
# ============================================================

unmutated_cases = []

# 1. Plain text, 200 bytes — below minCompressibleBytes (512)
unmutated_cases.append({
    "label": "plain_text_below_threshold",
    "input": "This is a simple plain text message that doesn't need compression. " * 2,
    "reason": "below_byte_threshold",
})

# 2. JSON array, 3 items, under 512 bytes
unmutated_cases.append({
    "label": "json_array_small",
    "input": json.dumps([{"id": i, "name": f"item_{i}", "status": "ok"} for i in range(3)]),
    "reason": "below_byte_threshold",
})

# 3. Git diff, 1 file, under 512 bytes
unmutated_cases.append({
    "label": "diff_small",
    "input": "diff --git a/foo.py b/foo.py\nindex abc..def 100644\n--- a/foo.py\n+++ b/foo.py\n@@ -1,3 +1,3 @@\n-old line\n+new line\n context\n",
    "reason": "below_byte_threshold",
})

# 4. Source code — no code compressor in livezone pipeline
unmutated_cases.append({
    "label": "source_code_passthrough",
    "input": "\n".join([
        "import os",
        "import sys",
        "import json",
        "",
        "def process_data(items):",
        "    results = []",
        "    for item in items:",
        "        if item.get('status') == 'active':",
        "            results.append(item)",
        "    return results",
        "",
        "def validate_input(data):",
        "    if not isinstance(data, dict):",
        "        raise ValueError('Expected dict')",
        "    required = ['name', 'id', 'type']",
        "    for key in required:",
        "        if key not in data:",
        "            raise KeyError(f'Missing {key}')",
        "    return True",
        "",
        "class DataProcessor:",
        "    def __init__(self, config):",
        "        self.config = config",
        "        self.cache = {}",
        "",
        "    def run(self, items):",
        "        validated = [x for x in items if validate_input(x)]",
        "        return process_data(validated)",
    ]) + "\n",
    "reason": "no_compressor_for_content_type",
})

# 5. HTML page — no HTML compressor in pipeline
unmutated_cases.append({
    "label": "html_passthrough",
    "input": "<!DOCTYPE html>\n<html lang=\"en\">\n<head><title>Test Page</title></head>\n<body>\n" +
             "<h1>Welcome</h1>\n" + "<p>Paragraph content here.</p>\n" * 30 +
             "</body>\n</html>\n",
    "reason": "no_compressor_for_content_type",
})

# 6. Plain text, 2000 bytes — above threshold but no compressor for text
unmutated_cases.append({
    "label": "plain_text_large_passthrough",
    "input": ("The quick brown fox jumps over the lazy dog. " * 10 + "\n") * 5,
    "reason": "no_compressor_for_content_type",
})

# 7. JSON object (not array) — content_detector returns text, not json_array
unmutated_cases.append({
    "label": "json_object_not_array",
    "input": json.dumps({"users": [{"id": i, "name": f"user_{i}"} for i in range(20)],
                         "total": 20, "page": 1}, indent=2),
    "reason": "no_compressor_for_content_type",
})

# 8. JSON array of primitives (not dicts) — not detected as json_array
unmutated_cases.append({
    "label": "json_array_primitives",
    "input": json.dumps(list(range(200))),
    "reason": "no_compressor_for_content_type",
})

# 9. Short code block
unmutated_cases.append({
    "label": "code_below_token_threshold",
    "input": "def add(a, b):\n    return a + b\n\ndef sub(a, b):\n    return a - b\n",
    "reason": "below_byte_threshold",
})

# 10. Nearly-empty diff
unmutated_cases.append({
    "label": "diff_trivial",
    "input": "diff --git a/x b/x\n--- a/x\n+++ b/x\n@@ -1 +1 @@\n-a\n+b\n",
    "reason": "below_byte_threshold",
})


# ============================================================
# MUTATED CASES
# ============================================================

mutated_cases = []

# 1. Large JSON array — SmartCrusher
items_100 = [{"id": i, "status": "active" if i % 3 else "inactive",
              "message": f"Processing item {i} with result code {200 + (i % 5)}",
              "timestamp": f"2026-01-{(i % 28) + 1:02d}T10:00:00Z",
              "score": round(0.5 + (i % 10) * 0.05, 2)} for i in range(100)]
mutated_cases.append({
    "label": "json_array_100_items",
    "input": json.dumps(items_100),
    "expected_transform": "smart_crusher",
})

# 2. JSON array with duplicates
dup_items = [{"id": i % 5, "type": "health_check", "status": "ok", "latency_ms": 42} for i in range(50)]
mutated_cases.append({
    "label": "json_array_50_duplicates",
    "input": json.dumps(dup_items),
    "expected_transform": "smart_crusher",
})

# 3. JSON array, 30 items high variance
varied = [{"id": i, "type": ["error", "warn", "info", "debug"][i % 4],
           "msg": f"{'ERROR: connection refused' if i % 4 == 0 else 'OK'} server-{i}",
           "code": i * 7 % 100} for i in range(30)]
mutated_cases.append({
    "label": "json_array_30_varied",
    "input": json.dumps(varied),
    "expected_transform": "smart_crusher",
})

# 4. Large git diff, 15 files
diff_lines = []
for f_idx in range(15):
    fname = f"src/module_{f_idx}/handler.py"
    diff_lines.append(f"diff --git a/{fname} b/{fname}")
    diff_lines.append(f"index {'a' * 7}..{'b' * 7} 100644")
    diff_lines.append(f"--- a/{fname}")
    diff_lines.append(f"+++ b/{fname}")
    for h in range(3):
        start = 10 + h * 20
        diff_lines.append(f"@@ -{start},5 +{start},5 @@ def handle_{h}():")
        diff_lines.append(f"     context line {h}")
        diff_lines.append(f"-    old_value_{h} = compute({h})")
        diff_lines.append(f"+    new_value_{h} = compute({h} + 1)")
        diff_lines.append(f"     more context")
        diff_lines.append(f"     trailing context")
mutated_cases.append({
    "label": "diff_15_files",
    "input": "\n".join(diff_lines) + "\n",
    "expected_transform": "diff_compressor",
})

# 5. Diff with lockfile
lockfile_diff = []
lockfile_diff.append("diff --git a/package-lock.json b/package-lock.json")
lockfile_diff.append("index aaa..bbb 100644")
lockfile_diff.append("--- a/package-lock.json")
lockfile_diff.append("+++ b/package-lock.json")
for i in range(50):
    lockfile_diff.append(f'@@ -{i*10},3 +{i*10},3 @@')
    lockfile_diff.append(f'-    "version": "1.{i}.0"')
    lockfile_diff.append(f'+    "version": "1.{i}.1"')
lockfile_diff.append("diff --git a/src/app.js b/src/app.js")
lockfile_diff.append("index ccc..ddd 100644")
lockfile_diff.append("--- a/src/app.js")
lockfile_diff.append("+++ b/src/app.js")
lockfile_diff.append("@@ -5,3 +5,3 @@")
lockfile_diff.append("-const VERSION = '1.0';")
lockfile_diff.append("+const VERSION = '1.1';")
lockfile_diff.append(" module.exports = app;")
mutated_cases.append({
    "label": "diff_with_lockfile",
    "input": "\n".join(lockfile_diff) + "\n",
    "expected_transform": "diff_compressor",
})

# 6. Pytest output
pytest_lines = ["=" * 60, "FAILED tests session starts", "=" * 60,
                "platform linux -- Python 3.11.5, pytest-7.4.0",
                "collected 50 items", ""]
for i in range(45):
    pytest_lines.append(f"tests/test_module.py::test_case_{i} PASSED")
pytest_lines.extend([
    f"tests/test_module.py::test_case_45 FAILED",
    "",
    "FAILURES",
    "=" * 60,
    "_ test_case_45 _",
    "",
    "    def test_case_45():",
    "        result = process(data)",
    ">       assert result == expected",
    "E       AssertionError: assert {'status': 'error'} == {'status': 'ok'}",
    "",
    "tests/test_module.py:123: AssertionError",
    "=" * 60,
    "1 failed, 45 passed in 3.21s",
])
mutated_cases.append({
    "label": "pytest_output",
    "input": "\n".join(pytest_lines) + "\n",
    "expected_transform": "log_compressor",
})

# 7. npm build output
npm_lines = [
    "> my-app@1.0.0 build",
    "> webpack --mode production",
    "",
]
for i in range(30):
    npm_lines.append(f"  asset chunk_{i}.js 125 KiB [emitted]")
npm_lines.extend([
    "",
    "WARNING in ./src/legacy.js",
    "Module Warning (from ./node_modules/eslint-loader):",
    "  'foo' is defined but never used",
    "",
    "ERROR in ./src/broken.js",
    "Module Error (from ./node_modules/babel-loader):",
    "  SyntaxError: Unexpected token (42:0)",
    "",
    "webpack compiled with 1 error and 1 warning",
])
mutated_cases.append({
    "label": "npm_build_output",
    "input": "\n".join(npm_lines) + "\n",
    "expected_transform": "log_compressor",
})

# 8. Grep output, many matches
grep_lines = []
for f_idx in range(20):
    fname = f"src/handlers/handler_{f_idx}.py"
    for match in range(5):
        line_no = 10 + match * 15
        grep_lines.append(f"{fname}:{line_no}:    def process_{match}(self, request):")
mutated_cases.append({
    "label": "grep_100_matches",
    "input": "\n".join(grep_lines) + "\n",
    "expected_transform": "search_compressor",
})

# 9. JSON array with nested objects
nested = [{"id": i, "config": {"retry": {"max": 3, "delay": i * 100},
           "timeout": 30000, "headers": {"X-Request-Id": f"req-{i:04d}"}},
           "metadata": {"created": f"2026-06-{(i % 28)+1:02d}", "tags": [f"tag_{i%5}"]}}
          for i in range(40)]
mutated_cases.append({
    "label": "json_array_40_nested",
    "input": json.dumps(nested),
    "expected_transform": "smart_crusher",
})

# 10. Large diff, 30+ files
big_diff_lines = []
for f_idx in range(30):
    fname = f"pkg/service_{f_idx // 5}/file_{f_idx % 5}.go"
    big_diff_lines.append(f"diff --git a/{fname} b/{fname}")
    big_diff_lines.append(f"index {'0' * 7}..{'1' * 7} 100644")
    big_diff_lines.append(f"--- a/{fname}")
    big_diff_lines.append(f"+++ b/{fname}")
    big_diff_lines.append(f"@@ -1,4 +1,4 @@")
    big_diff_lines.append(f" package service_{f_idx // 5}")
    big_diff_lines.append(f"-var old_{f_idx} = true")
    big_diff_lines.append(f"+var new_{f_idx} = false")
    big_diff_lines.append(f" // end")
mutated_cases.append({
    "label": "diff_30_files",
    "input": "\n".join(big_diff_lines) + "\n",
    "expected_transform": "diff_compressor",
})


def main():
    UNMUTATED_DIR.mkdir(parents=True, exist_ok=True)
    MUTATED_DIR.mkdir(parents=True, exist_ok=True)

    # Generate unmutated fixtures
    print(f"Generating {len(unmutated_cases)} unmutated fixtures...")
    for case in unmutated_cases:
        ct, conf = detect(case["input"])
        fixture = {
            "transform": "e2e_unmutated",
            "input": case["input"],
            "output": {
                "mutated": False,
                "content_type": ct,
                "confidence": round(conf, 4),
                "reason": case["reason"],
            },
            "label": case["label"],
        }
        fname = sha_name(case["input"])
        path = UNMUTATED_DIR / fname
        with open(path, "w") as f:
            json.dump(fixture, f, indent=2)
        print(f"  {case['label']}: {ct} ({conf:.2f}) → {fname}")

    # Generate mutated fixtures
    print(f"\nGenerating {len(mutated_cases)} mutated fixtures...")
    generated_mutated = 0
    for case in mutated_cases:
        ct, conf = detect(case["input"])
        transform = case["expected_transform"]

        # Get SOT compressed output from Rust
        compressed, ok = rust_compress(transform, case["input"])
        if not ok:
            print(f"  SKIP: {case['label']}: Rust bench failed for {transform}: {compressed[:80]}")
            continue

        # For npm_build_output: if compressed == input, Rust didn't actually compress
        if compressed == case["input"]:
            print(f"  SKIP: {case['label']}: Rust returned input unchanged (no compression)")
            continue

        fixture = {
            "transform": "e2e_mutated",
            "input": case["input"],
            "output": {
                "mutated": True,
                "content_type": ct,
                "strategy": transform,
                "compressed": compressed,
            },
            "label": case["label"],
        }
        fname = sha_name(case["input"])
        path = MUTATED_DIR / fname
        with open(path, "w") as f:
            json.dump(fixture, f, indent=2)
        generated_mutated += 1
        print(f"  {case['label']}: {ct} → {transform} ({len(case['input'])} → {len(compressed)} bytes) → {fname}")

    print(f"\nDone. {len(unmutated_cases)} unmutated + {generated_mutated} mutated fixtures generated.")


if __name__ == "__main__":
    main()
