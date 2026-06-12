package transforms

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uber/goheadroom/transforms/contentdetector"
)

func TestDetectPlainTextChain(t *testing.T) {
	result := Detect("Hello, this is a simple message.", "")
	assert.Equal(t, contentdetector.PlainText, result.ContentType)
}

func TestDetectDiffContent(t *testing.T) {
	input := "diff --git a/file.txt b/file.txt\n--- a/file.txt\n+++ b/file.txt\n@@ -1,3 +1,4 @@\n line1\n+added\n line3\n"
	result := Detect(input, "")
	assert.Equal(t, contentdetector.GitDiff, result.ContentType)
}

func TestDetectJsonArrayContent(t *testing.T) {
	input := `[{"name": "Alice"}, {"name": "Bob"}]`
	result := Detect(input, "")
	assert.Equal(t, contentdetector.JsonArray, result.ContentType)
}

func TestDetectSourceCodeContent(t *testing.T) {
	input := `import os
from typing import Any

def factorial(n):
    if n <= 1:
        return 1
    return n * factorial(n - 1)

class Calculator:
    def compute(self):
        pass

@property
def value(self):
    return 42
`
	result := Detect(input, "")
	assert.Equal(t, contentdetector.SourceCode, result.ContentType)
}

func TestDetectHtmlContent(t *testing.T) {
	input := "<!DOCTYPE html>\n<html><head><title>Test</title></head>\n<body><p>Hello</p></body></html>"
	result := Detect(input, "")
	assert.Equal(t, contentdetector.Html, result.ContentType)
}

func TestDetectTierPriority(t *testing.T) {
	// Diff detection should take priority even if content looks code-like
	input := "diff --git a/main.py b/main.py\n--- a/main.py\n+++ b/main.py\n@@ -1,3 +1,4 @@\n def main():\n+    print(\"hello\")\n     pass\n"
	result := Detect(input, "")
	assert.Equal(t, contentdetector.GitDiff, result.ContentType)
}

func TestDetectEmptyString(t *testing.T) {
	result := Detect("", "")
	assert.Equal(t, contentdetector.PlainText, result.ContentType)
}

func TestDetectSearchResultsChain(t *testing.T) {
	input := "src/main.py:42:def process():\nsrc/util.py:13:    return None\nlib/x.py:7:class X:"
	result := Detect(input, "")
	assert.Equal(t, contentdetector.SearchResults, result.ContentType)
}

func TestDetectBuildOutputChain(t *testing.T) {
	input := "[INFO] Starting build\n[INFO] Compiling 42 sources\n[ERROR] Compilation failed\n[WARN] Deprecated API\nFAILED test_one\nPASSED test_two\n"
	result := Detect(input, "")
	assert.Equal(t, contentdetector.BuildOutput, result.ContentType)
}

func TestDetectWithModelPath(t *testing.T) {
	// When model path is provided but invalid, should fall through to lower tiers
	result := Detect("just plain text", "/nonexistent/model.onnx")
	assert.Equal(t, contentdetector.PlainText, result.ContentType)
}

func TestDetectReturnsTier(t *testing.T) {
	result := Detect("regular text here", "")
	require.NotNil(t, result)
	assert.True(t, result.Tier >= 1 && result.Tier <= 3,
		"tier should be 1-3, got %d", result.Tier)
}
