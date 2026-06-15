#!/usr/bin/env python3
"""Generate parity fixtures for json_compressor and code_compressor."""
import hashlib
import json
import sys
from datetime import datetime, timezone
from pathlib import Path

sys.path.insert(0, "/tmp/headroom-chopratejas")

from headroom.compression.handlers.json_handler import JSONStructureHandler
from headroom.transforms.code_compressor import CodeAwareCompressor


def sha256_hex(s: str) -> str:
    return hashlib.sha256(s.encode()).hexdigest()[:16]


def make_fixture(transform, config, input_val, output):
    return {
        "transform": transform,
        "config": config,
        "input": input_val,
        "output": output,
        "input_sha256": sha256_hex(input_val),
        "recorded_at": datetime.now(timezone.utc).isoformat(),
    }


def structural_chars(content, mask):
    return "".join(c for i, c in enumerate(content) if i < len(mask) and mask[i])


def generate_json_fixtures():
    handler = JSONStructureHandler()
    cfg = {"short_value_threshold": 20, "entropy_threshold": 0.85,
           "max_array_items_full": 3, "max_number_digits": 10}

    cases = [
        '{"name": "Alice", "age": 30}',
        '{"active": true, "deleted": false, "value": null}',
        '{"id": "550e8400-e29b-41d4-a716-446655440000", "name": "Bob"}',
        '{"description": "' + "x" * 100 + '", "status": "ok"}',
        '[{"id": 1, "val": "a"}, {"id": 2, "val": "b"}, {"id": 3, "val": "c"}, {"id": 4, "val": "d"}]',
        '{"nested": {"inner_key": "short", "inner_long": "' + "y" * 80 + '"}}',
        '{"count": 42, "ratio": 3.14159}',
        '{"items": [true, false, null]}',
        '{}',
        '{"a": "b"}',
    ]

    results = []
    for content in cases:
        result = handler.get_mask(content)
        compressed = structural_chars(content, result.mask.mask)
        output = {
            "compressed": compressed,
            "preservation_ratio": round(result.mask.preservation_ratio, 4),
        }
        results.append(make_fixture("json_compressor", cfg, content, output))
    return results


def generate_code_fixtures():
    compressor = CodeAwareCompressor()
    cfg = {}

    cases = [
        "def hello(name: str) -> str:\n    message = f'Hello, {name}!'\n    return message\n",
        "import os\nimport sys\n\nx = 1\ny = 2\n",
        "class MyClass:\n    def __init__(self):\n        self.x = 1\n        self.y = 2\n",
        "func Add(a int, b int) int {\n\treturn a + b\n}\n",
        "pub fn add(a: i32, b: i32) -> i32 {\n    a + b\n}\n",
        "function greet(name) {\n  const msg = `Hello ${name}`;\n  return msg;\n}\n",
        "interface Shape {\n  area(): number;\n}\n",
        "import os\n\ndef foo():\n    pass\n\ndef bar(x):\n    return x * 2\n",
        "#include <stdio.h>\n\nint main() {\n    printf(\"hello\");\n    return 0;\n}\n",
        "#include <iostream>\n\nnamespace ns {\nclass Foo {\n  void bar() {\n    std::cout << 1;\n  }\n};\n}\n",
        "",
        "plain text without any code indicators\n",
    ]

    results = []
    for content in cases:
        result = compressor.compress(content)
        lang = "unknown"
        if hasattr(result, 'language') and result.language:
            lang = result.language.value if hasattr(result.language, 'value') else str(result.language)
        output = {
            "compressed": result.compressed,
            "language": lang,
        }
        results.append(make_fixture("code_compressor", cfg, content, output))
    return results


def write_fixtures(outdir, items):
    outdir.mkdir(parents=True, exist_ok=True)
    for item in items:
        path = outdir / f"{item['input_sha256']}.json"
        with open(path, "w") as f:
            json.dump(item, f, indent=2, ensure_ascii=False)
            f.write("\n")
        print(f"  {path.name}")


if __name__ == "__main__":
    root = Path(__file__).parent.parent
    print("json_compressor:")
    write_fixtures(root / "testdata/parity/json_compressor", generate_json_fixtures())
    print("code_compressor:")
    write_fixtures(root / "testdata/parity/code_compressor", generate_code_fixtures())
    print("Done.")
