#!/usr/bin/env python3
"""Generate search_compressor parity fixtures using Go bench as SOT.

Rust bench does not support search_compressor, so Go is the reference
implementation. This script:
  1. Generates realistic grep/ripgrep output strings
  2. Runs each through the Go bench binary to get compressed output
  3. Writes fixtures to testdata/parity/search_compressor/
"""

import json
import os
import subprocess
import sys
import tempfile

SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))
PROJECT_ROOT = os.path.dirname(SCRIPT_DIR)
FIXTURE_DIR = os.path.join(PROJECT_ROOT, "testdata", "parity", "search_compressor")
GO_BENCH = os.path.join(PROJECT_ROOT, "goheadroom-bench")


def build_go_bench():
    """Build the Go bench binary if needed."""
    if not os.path.exists(GO_BENCH):
        print("Building Go bench binary...")
        subprocess.check_call(
            ["/opt/homebrew/Cellar/go/1.24.2/libexec/bin/go", "build",
             "-o", GO_BENCH, "./cmd/bench/"],
            cwd=PROJECT_ROOT,
            env={**os.environ, "CGO_LDFLAGS": f"-L{PROJECT_ROOT}"},
        )


def run_go_bench(input_text: str) -> str:
    """Run Go bench with search_compressor and return compressed output."""
    fixture = {"transform": "search_compressor", "input": input_text, "output": {}}
    with tempfile.NamedTemporaryFile(mode="w", suffix=".json", delete=False) as f:
        json.dump(fixture, f)
        f.flush()
        tmp_path = f.name

    try:
        result = subprocess.run(
            [GO_BENCH, tmp_path],
            capture_output=True, text=True, timeout=30,
        )
        if result.returncode != 0:
            print(f"  Go bench error: {result.stderr.strip()}", file=sys.stderr)
            return None
        return result.stdout
    finally:
        os.unlink(tmp_path)


# ── Fixture generators ──────────────────────────────────────────────

def gen_simple_python():
    """Simple Python grep output: 3 files, ~10 matches."""
    lines = []
    for fn in ["app/models.py", "app/views.py", "app/utils.py"]:
        for i in range(1, 5):
            lineno = i * 10 + 2
            lines.append(f"{fn}:{lineno}:    def process_{i}(self, data):")
    return "\n".join(lines) + "\n"


def gen_mixed_filetypes():
    """Mixed file types: Python, Go, JS, YAML."""
    lines = []
    # Python
    for i in range(1, 4):
        lines.append(f"src/handler.py:{i*15}:    result = process(data_{i})")
    # Go
    for i in range(1, 4):
        lines.append(f"pkg/server/server.go:{i*20}:\tfmt.Println(\"processing\", item{i})")
    # JavaScript
    for i in range(1, 4):
        lines.append(f"src/components/App.jsx:{i*8}:  const handle{i} = () => {{ processData({i}); }}")
    # Config
    lines.append("config/settings.yaml:5:  process_timeout: 30")
    lines.append("config/settings.yaml:12:  max_retries: 3")
    return "\n".join(lines) + "\n"


def gen_medium_python():
    """Medium Python grep: 5 files, ~25 matches."""
    lines = []
    files = [
        "src/api/endpoints.py",
        "src/api/middleware.py",
        "src/core/engine.py",
        "src/core/pipeline.py",
        "src/utils/helpers.py",
    ]
    for fn in files:
        for i in range(1, 6):
            lineno = i * 12 + 3
            content = [
                f"    async def handle_request_{i}(self, req):",
                f"        return await self.process(req, timeout={i*10})",
                f"    logger.info(\"Processing step {i}\")",
                f"    if not validate(data_{i}):",
                f"        raise ValueError(\"Invalid data at step {i}\")",
            ][i - 1]
            lines.append(f"{fn}:{lineno}:{content}")
    return "\n".join(lines) + "\n"


def gen_context_separators():
    """Grep output with -- context separators between match groups."""
    lines = []
    lines.append("server/main.go:10:func startServer(port int) error {")
    lines.append("server/main.go:11:\tlog.Printf(\"Starting server on :%d\", port)")
    lines.append("server/main.go:12:\treturn http.ListenAndServe(fmt.Sprintf(\":%d\", port), nil)")
    lines.append("--")
    lines.append("server/main.go:45:func handleRequest(w http.ResponseWriter, r *http.Request) {")
    lines.append("server/main.go:46:\tlog.Printf(\"Handling %s %s\", r.Method, r.URL.Path)")
    lines.append("--")
    lines.append("server/handler.go:20:func processPayload(data []byte) (Result, error) {")
    lines.append("server/handler.go:21:\tvar req Request")
    lines.append("server/handler.go:22:\tif err := json.Unmarshal(data, &req); err != nil {")
    lines.append("server/handler.go:23:\t\treturn Result{}, fmt.Errorf(\"unmarshal: %w\", err)")
    lines.append("--")
    lines.append("server/handler.go:55:func validateInput(req Request) error {")
    lines.append("server/handler.go:56:\tif req.Name == \"\" {")
    lines.append("server/handler.go:57:\t\treturn errors.New(\"name is required\")")
    return "\n".join(lines) + "\n"


def gen_large_go():
    """Large Go grep: 10 files, 50+ matches."""
    lines = []
    pkgs = ["api", "auth", "cache", "config", "db", "handler", "metrics", "middleware", "router", "service"]
    for pkg in pkgs:
        fn = f"internal/{pkg}/{pkg}.go"
        for i in range(1, 7):
            lineno = i * 15 + 5
            variants = [
                f"func (s *{pkg.title()}) Init(ctx context.Context) error {{",
                f"\tlog.Info(\"initializing {pkg}\")",
                f"\tif err := s.validate(); err != nil {{",
                f"\t\treturn fmt.Errorf(\"{pkg} init: %w\", err)",
                f"func (s *{pkg.title()}) Process(req *Request) (*Response, error) {{",
                f"\tmetrics.Inc(\"{pkg}_requests_total\")",
            ]
            lines.append(f"{fn}:{lineno}:{variants[i-1]}")
    return "\n".join(lines) + "\n"


def gen_very_large():
    """Very large grep output: 15+ files, 100+ matches."""
    lines = []
    dirs = ["cmd", "internal", "pkg", "api", "services"]
    for d in dirs:
        for j in range(1, 4):
            fn = f"{d}/module{j}/handler.go"
            for i in range(1, 9):
                lineno = i * 10 + j * 3
                lines.append(f"{fn}:{lineno}:\tfmt.Printf(\"step %d in {d}/module{j}\", {i})")
    # Add some Python files too
    for j in range(1, 4):
        fn = f"scripts/tool{j}.py"
        for i in range(1, 6):
            lineno = i * 8
            lines.append(f"{fn}:{lineno}:    print(f\"Processing tool{j} step {i}\")")
    return "\n".join(lines) + "\n"


def gen_error_heavy():
    """Grep output heavy on error/exception patterns (should score higher)."""
    lines = []
    lines.append("app/service.py:10:    def run(self):")
    lines.append("app/service.py:15:        try:")
    lines.append("app/service.py:16:            result = self.process()")
    lines.append("app/service.py:17:        except ConnectionError as e:")
    lines.append("app/service.py:18:            logger.error(f\"Connection failed: {e}\")")
    lines.append("app/service.py:19:            raise ServiceError(\"upstream unavailable\") from e")
    lines.append("app/service.py:25:        except TimeoutError:")
    lines.append("app/service.py:26:            logger.error(\"Request timed out\")")
    lines.append("app/service.py:30:        except Exception as e:")
    lines.append("app/service.py:31:            logger.critical(f\"Unexpected error: {e}\")")
    lines.append("app/service.py:32:            traceback.print_exc()")
    lines.append("app/retry.py:5:class RetryError(Exception):")
    lines.append("app/retry.py:8:    def __init__(self, message, attempts):")
    lines.append("app/retry.py:15:        raise RetryError(f\"Failed after {max_retries} attempts\")")
    return "\n".join(lines) + "\n"


def gen_ripgrep_style():
    """ripgrep-style output with file headers (no filename: prefix per line)."""
    # Actually ripgrep still uses file:line:content format by default.
    # But with --heading it uses separate headers. Let's do standard format
    # with typical rg patterns including column numbers.
    lines = []
    lines.append("src/auth/token.rs:15:5:    pub fn validate(&self) -> Result<Claims, TokenError> {")
    lines.append("src/auth/token.rs:16:9:        let decoded = decode(&self.raw, &self.key)?;")
    lines.append("src/auth/token.rs:22:9:        if decoded.exp < Utc::now().timestamp() {")
    lines.append("src/auth/token.rs:23:13:            return Err(TokenError::Expired);")
    lines.append("src/auth/middleware.rs:8:5:    pub async fn authenticate(req: &Request) -> Result<User, AuthError> {")
    lines.append("src/auth/middleware.rs:12:9:        let token = req.headers().get(\"Authorization\")")
    lines.append("src/auth/middleware.rs:15:9:        match Token::new(token).validate() {")
    lines.append("src/auth/middleware.rs:16:13:            Ok(claims) => Ok(User::from(claims)),")
    lines.append("src/auth/middleware.rs:17:13:            Err(e) => Err(AuthError::InvalidToken(e)),")
    return "\n".join(lines) + "\n"


def gen_duplicate_patterns():
    """Many similar/duplicate matches across files (tests CCR dedup)."""
    lines = []
    for i in range(1, 13):
        fn = f"tests/test_module{i}.py"
        lines.append(f"{fn}:5:    def setUp(self):")
        lines.append(f"{fn}:6:        self.client = TestClient(app)")
        lines.append(f"{fn}:10:    def test_basic(self):")
        lines.append(f"{fn}:11:        response = self.client.get(\"/api/v1/health\")")
        lines.append(f"{fn}:12:        self.assertEqual(response.status_code, 200)")
    return "\n".join(lines) + "\n"


def gen_single_file_many_matches():
    """Single file with many matches (tests per-file limit)."""
    lines = []
    fn = "monolith/legacy_handler.py"
    for i in range(1, 35):
        lineno = i * 5
        lines.append(f"{fn}:{lineno}:    def handler_{i}(self, request):")
    return "\n".join(lines) + "\n"


def gen_sparse_matches():
    """Few matches spread across many files."""
    lines = []
    for i in range(1, 20):
        fn = f"src/pkg{i}/init.go"
        lineno = 3 + (i * 7) % 50
        lines.append(f"{fn}:{lineno}:func init() {{ register(\"{f'pkg{i}'}\") }}")
    return "\n".join(lines) + "\n"


def gen_empty_like():
    """Minimal grep output: only 2 matches."""
    return "README.md:1:# My Project\nREADME.md:3:A simple tool.\n"


def gen_config_files():
    """Matches in config/infrastructure files."""
    lines = []
    lines.append("docker-compose.yml:5:  db:")
    lines.append("docker-compose.yml:6:    image: postgres:15")
    lines.append("docker-compose.yml:10:  redis:")
    lines.append("docker-compose.yml:11:    image: redis:7-alpine")
    lines.append("Makefile:15:build:")
    lines.append("Makefile:16:\tgo build -o bin/server ./cmd/server")
    lines.append("Makefile:20:test:")
    lines.append("Makefile:21:\tgo test ./...")
    lines.append(".github/workflows/ci.yml:12:    - run: go test ./...")
    lines.append(".github/workflows/ci.yml:15:    - run: go build ./...")
    lines.append("terraform/main.tf:8:  instance_type = \"t3.medium\"")
    lines.append("terraform/main.tf:12:  ami = \"ami-0123456789abcdef0\"")
    return "\n".join(lines) + "\n"


GENERATORS = [
    ("simple_python", gen_simple_python),
    ("mixed_filetypes", gen_mixed_filetypes),
    ("medium_python", gen_medium_python),
    ("context_separators", gen_context_separators),
    ("large_go", gen_large_go),
    ("very_large", gen_very_large),
    ("error_heavy", gen_error_heavy),
    ("ripgrep_style", gen_ripgrep_style),
    ("duplicate_patterns", gen_duplicate_patterns),
    ("single_file_many", gen_single_file_many_matches),
    ("sparse_matches", gen_sparse_matches),
    ("empty_like", gen_empty_like),
    ("config_files", gen_config_files),
]


def main():
    build_go_bench()
    os.makedirs(FIXTURE_DIR, exist_ok=True)

    created = 0
    for name, gen_fn in GENERATORS:
        input_text = gen_fn()
        print(f"Generating {name} ({len(input_text)} bytes input)...")

        compressed = run_go_bench(input_text)
        if compressed is None:
            print(f"  SKIP: Go bench failed for {name}")
            continue

        fixture = {
            "transform": "search_compressor",
            "input": input_text,
            "output": {"compressed": compressed},
        }

        out_path = os.path.join(FIXTURE_DIR, f"{name}.json")
        with open(out_path, "w") as f:
            json.dump(fixture, f, indent=2)
        print(f"  Wrote {out_path} ({len(compressed)} bytes compressed)")
        created += 1

    print(f"\nDone: {created} fixtures created in {FIXTURE_DIR}")


if __name__ == "__main__":
    main()
