package contentdetector

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectJsonArray(t *testing.T) {
	input := `[{"name": "Alice", "age": 30}, {"name": "Bob", "age": 25}]`
	result := DetectContentType(input)
	assert.Equal(t, JsonArray, result.ContentType)
	assert.True(t, result.Confidence > 0.8)
}

func TestDetectJsonArrayNested(t *testing.T) {
	input := `[{"user": {"name": "Alice"}, "scores": [1,2,3]}, {"user": {"name": "Bob"}, "scores": [4,5,6]}]`
	result := DetectContentType(input)
	assert.Equal(t, JsonArray, result.ContentType)
}

func TestDetectJsonArrayEmpty(t *testing.T) {
	input := `[]`
	result := DetectContentType(input)
	assert.Equal(t, JsonArray, result.ContentType)
}

func TestDetectJsonArraySingleElement(t *testing.T) {
	input := `[{"key": "value"}]`
	result := DetectContentType(input)
	assert.Equal(t, JsonArray, result.ContentType)
}

func TestDetectSourceCode(t *testing.T) {
	input := `package main

import "fmt"

func main() {
	fmt.Println("hello")
}

type Service struct{}

func (s *Service) Do() {}

func helper() {}
`
	result := DetectContentType(input)
	assert.Equal(t, SourceCode, result.ContentType)
}

func TestDetectSourceCodePython(t *testing.T) {
	input := `import os
from typing import Any

def process(data):
    return data

class Service:
    def __init__(self):
        pass

    @property
    def x(self):
        return 1

if __name__ == '__main__':
    process({})
`
	result := DetectContentType(input)
	assert.Equal(t, SourceCode, result.ContentType)
}

func TestDetectSearchResults(t *testing.T) {
	input := `src/main.py:42:def process():
src/util.py:13:    return None
lib/x.py:7:class X:`
	result := DetectContentType(input)
	assert.Equal(t, SearchResults, result.ContentType)
}

func TestDetectBuildOutput(t *testing.T) {
	input := `[INFO] Starting build
[INFO] Compiling 42 sources
[ERROR] Compilation failed
[WARN] Deprecated API
FAILED test_one
PASSED test_two
`
	result := DetectContentType(input)
	assert.Equal(t, BuildOutput, result.ContentType)
}

func TestDetectGitDiff(t *testing.T) {
	input := `diff --git a/src/main.rs b/src/main.rs
index 1234567..abcdefg 100644
--- a/src/main.rs
+++ b/src/main.rs
@@ -1,5 +1,6 @@
 fn main() {
     println!("hello");
+    println!("world");
 }
`
	result := DetectContentType(input)
	assert.Equal(t, GitDiff, result.ContentType)
}

func TestDetectHtml(t *testing.T) {
	input := `<!DOCTYPE html>
<html>
<head><title>Test</title></head>
<body>
<h1>Hello World</h1>
<p>This is a paragraph.</p>
</body>
</html>`
	result := DetectContentType(input)
	assert.Equal(t, Html, result.ContentType)
}

func TestDetectPlainText(t *testing.T) {
	input := `This is just a regular paragraph of text. It doesn't contain any special formatting or code. Just plain English sentences that describe something ordinary.`
	result := DetectContentType(input)
	assert.Equal(t, PlainText, result.ContentType)
}

func TestDetectPlainTextShort(t *testing.T) {
	input := `Hello world`
	result := DetectContentType(input)
	assert.Equal(t, PlainText, result.ContentType)
}

func TestDetectPlainTextEmpty(t *testing.T) {
	input := ``
	result := DetectContentType(input)
	assert.Equal(t, PlainText, result.ContentType)
}

func TestIsJsonArrayOfDictsValid(t *testing.T) {
	input := `[{"a": 1}, {"b": 2}]`
	assert.True(t, IsJsonArrayOfDicts(input))
}

func TestIsJsonArrayOfDictsInvalid(t *testing.T) {
	input := `[1, 2, 3]`
	assert.False(t, IsJsonArrayOfDicts(input))
}

func TestIsJsonArrayOfDictsNotArray(t *testing.T) {
	input := `{"a": 1}`
	assert.False(t, IsJsonArrayOfDicts(input))
}

func TestIsJsonArrayOfDictsNotJson(t *testing.T) {
	input := `not json at all`
	assert.False(t, IsJsonArrayOfDicts(input))
}

func TestDetectContentTypeConfidenceRange(t *testing.T) {
	inputs := []string{
		`[{"a":1}]`,
		`def foo(): pass`,
		`just plain text`,
	}
	for _, input := range inputs {
		result := DetectContentType(input)
		assert.True(t, result.Confidence >= 0.0 && result.Confidence <= 1.0,
			"confidence should be in [0,1], got %f for input %q", result.Confidence, input)
	}
}

func TestDetectBuildOutputWithWarnings(t *testing.T) {
	input := `warning: unused import
 --> src/lib.rs:3:5
  |
3 | use std::io;
  |     ^^^^^^^
  |
  = note: #[warn(unused_imports)] on by default

warning: 1 warning emitted
`
	result := DetectContentType(input)
	require.Contains(t, []ContentType{BuildOutput, SourceCode}, result.ContentType)
}
