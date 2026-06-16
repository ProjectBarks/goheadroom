package codecompressor

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// padCode pads code to exceed the 100 estimated-token threshold (400+ chars)
// by appending comment lines, ensuring compression actually runs.
func padCode(code string) string {
	for estimateTokens(code) < 100 {
		code += "\n// padding line to reach min token threshold for compression"
	}
	return code
}

// --- DetectLanguage tests ---

func TestDetectLanguage_Python(t *testing.T) {
	code := `import os
def hello():
    print("hello")

class Foo:
    def bar(self):
        pass
`
	assert.Equal(t, Python, DetectLanguage(code))
}

func TestDetectLanguage_Go(t *testing.T) {
	code := `package main

func main() {
    fmt.Println("hello")
}
`
	assert.Equal(t, Go, DetectLanguage(code))
}

func TestDetectLanguage_Rust(t *testing.T) {
	code := `use std::io;

fn main() {
    let mut input = String::new();
    println!("hello");
}
`
	assert.Equal(t, Rust, DetectLanguage(code))
}

func TestDetectLanguage_C(t *testing.T) {
	code := `#include <stdio.h>

int main(int argc, char *argv[]) {
    printf("hello\n");
    return 0;
}
`
	assert.Equal(t, C, DetectLanguage(code))
}

func TestDetectLanguage_CPP(t *testing.T) {
	code := `#include <iostream>
namespace foo {
class Bar {
    std::string name;
};
}
`
	assert.Equal(t, CPP, DetectLanguage(code))
}

func TestDetectLanguage_Unknown(t *testing.T) {
	assert.Equal(t, Unknown, DetectLanguage("hello world this is prose"))
	assert.Equal(t, Unknown, DetectLanguage(""))
}

// --- CanHandle tests ---

func TestCanHandle_Code(t *testing.T) {
	assert.True(t, CanHandle("def hello():\n    pass"))
	assert.True(t, CanHandle("func main() {}"))
	assert.True(t, CanHandle("fn main() {}"))
}

func TestCanHandle_Prose(t *testing.T) {
	assert.False(t, CanHandle("This is just some English text about nothing in particular."))
}

func TestCanHandle_Empty(t *testing.T) {
	assert.False(t, CanHandle(""))
	assert.False(t, CanHandle("   "))
}

// --- BelowThreshold: small inputs should not be compressed ---

func TestCompress_BelowThreshold_ReturnUnchanged(t *testing.T) {
	code := "def foo():\n    return 42\n"
	r := Compress(code)
	assert.Equal(t, code, r.Compressed,
		"inputs below 100 estimated tokens should not be compressed")
}

// --- Python compression tests ---

func TestCompress_Python_SignaturePreserved(t *testing.T) {
	code := padCode(`def calculate(x, y):
    result = x + y
    if result > 10:
        result = 10
    return result
`)
	r := Compress(code)
	assert.Equal(t, Python, r.Language)
	assert.Contains(t, r.Compressed, "def calculate(x, y):")
	assert.Contains(t, r.Compressed, "pass")
	assert.Contains(t, r.Compressed, "omitted")
	assert.NotContains(t, r.Compressed, "result = x + y")
	assert.Equal(t, 1, r.SignaturesKept)
	assert.Equal(t, 1, r.BodiesRemoved)
}

func TestCompress_Python_ImportsPreserved(t *testing.T) {
	code := `import os
from pathlib import Path
import sys

def hello():
    print("hello")
`
	r := Compress(padCode(code))
	assert.Contains(t, r.Compressed, "import os")
	assert.Contains(t, r.Compressed, "from pathlib import Path")
	assert.Contains(t, r.Compressed, "import sys")
}

func TestCompress_Python_ClassPreserved(t *testing.T) {
	code := padCode(`class Animal:
    def __init__(self, name):
        self.name = name
        self.age = 0

    def speak(self):
        return "..."
`)
	r := Compress(code)
	assert.Contains(t, r.Compressed, "class Animal:")
	assert.Contains(t, r.Compressed, "def __init__(self, name):")
	assert.Contains(t, r.Compressed, "def speak(self):")
	assert.Contains(t, r.Compressed, "pass")
}

func TestCompress_Python_DocstringPreserved(t *testing.T) {
	code := padCode(`def greet(name):
    """Greet someone by name."""
    print(f"Hello, {name}!")
    return True
`)
	r := Compress(code)
	assert.Contains(t, r.Compressed, `"""Greet someone by name."""`)
	assert.Contains(t, r.Compressed, "def greet(name):")
}

func TestCompress_Python_SyntaxValidity(t *testing.T) {
	code := padCode(`def foo():
    x = 1
    y = 2
    return x + y
`)
	r := Compress(code)
	assert.True(t, strings.Contains(r.Compressed, "pass"),
		"Python compressed output should contain pass for syntax validity")
}

// --- Go compression tests ---

func TestCompress_Go_FuncBodyElided(t *testing.T) {
	code := padCode(`package main

import "fmt"

func greet(name string) string {
	greeting := fmt.Sprintf("Hello, %s!", name)
	return greeting
}
`)
	r := Compress(code)
	assert.Equal(t, Go, r.Language)
	assert.Contains(t, r.Compressed, "package main")
	assert.Contains(t, r.Compressed, `import "fmt"`)
	assert.Contains(t, r.Compressed, "func greet(name string) string")
	assert.Contains(t, r.Compressed, "omitted")
	assert.NotContains(t, r.Compressed, "greeting :=")
}

// --- Rust compression tests ---

func TestCompress_Rust_FnBodyElided(t *testing.T) {
	code := padCode(`use std::io;

fn calculate(x: i32, y: i32) -> i32 {
    let result = x + y;
    if result > 100 {
        return 100;
    }
    result
}
`)
	r := Compress(code)
	assert.Equal(t, Rust, r.Language)
	assert.Contains(t, r.Compressed, "use std::io;")
	assert.Contains(t, r.Compressed, "fn calculate(x: i32, y: i32) -> i32")
	assert.Contains(t, r.Compressed, "omitted")
	assert.NotContains(t, r.Compressed, "let result")
}

// --- JavaScript compression tests ---

func TestCompress_JavaScript_FunctionBodyElided(t *testing.T) {
	code := padCode(`function greet(name) {
    const message = "Hello, " + name;
    console.log(message);
    return message;
}
`)
	r := Compress(code)
	assert.Equal(t, JavaScript, r.Language)
	assert.Contains(t, r.Compressed, "function greet(name)")
	assert.Contains(t, r.Compressed, "omitted")
	assert.NotContains(t, r.Compressed, "const message")
}

// --- C compression tests ---

func TestCompress_C_FuncBodyElided(t *testing.T) {
	code := padCode(`#include <stdio.h>
#include <stdlib.h>

int main(int argc, char *argv[]) {
    printf("Hello, World!\n");
    return 0;
}
`)
	r := Compress(code)
	assert.Equal(t, C, r.Language)
	assert.Contains(t, r.Compressed, "#include <stdio.h>")
	assert.Contains(t, r.Compressed, "#include <stdlib.h>")
	assert.Contains(t, r.Compressed, "int main(")
	assert.Contains(t, r.Compressed, "omitted")
	assert.NotContains(t, r.Compressed, "printf")
}

// --- C++ compression tests ---

func TestCompress_CPP_ClassBodyElided(t *testing.T) {
	code := padCode(`#include <iostream>

namespace myns {

class Greeter {
public:
    void greet() {
        std::cout << "Hello!" << std::endl;
    }
};

}
`)
	r := Compress(code)
	assert.Equal(t, CPP, r.Language)
	assert.Contains(t, r.Compressed, "#include <iostream>")
	assert.Contains(t, r.Compressed, "class Greeter")
	assert.Contains(t, r.Compressed, "omitted")
}

func TestCompress_CPP_IncludePreserved(t *testing.T) {
	code := padCode(`#include <vector>
#include <string>

void process(std::vector<std::string> items) {
    for (auto& item : items) {
        std::cout << item << std::endl;
    }
}
`)
	r := Compress(code)
	assert.Contains(t, r.Compressed, "#include <vector>")
	assert.Contains(t, r.Compressed, "#include <string>")
}

// --- Brace language syntax validity ---

func TestCompress_BraceLanguage_ClosingBrace(t *testing.T) {
	code := padCode(`func main() {
	fmt.Println("hello")
	fmt.Println("world")
}
`)
	r := Compress(code)
	assert.True(t, strings.Contains(r.Compressed, "}"),
		"Brace language compressed output should contain closing brace")
}

// --- Empty content ---

func TestCompress_EmptyContent(t *testing.T) {
	r := Compress("")
	assert.Equal(t, Unknown, r.Language)
	assert.Equal(t, "", r.Compressed)
}

func TestCompress_WhitespaceOnly(t *testing.T) {
	r := Compress("   \n\t\n  ")
	assert.Equal(t, Unknown, r.Language)
}

// --- Confidence ---

func TestCompress_Confidence_TreeSitterParsed(t *testing.T) {
	code := padCode(`def foo():
    return 42
`)
	r := Compress(code)
	require.Equal(t, Python, r.Language)
	assert.InDelta(t, 0.95, float64(r.Confidence), 0.01,
		"Tree-sitter parsed code should have confidence 0.95")
}

// --- Multiple functions ---

func TestCompress_MultipleFunctions(t *testing.T) {
	code := padCode(`def foo():
    return 1

def bar():
    return 2

def baz():
    return 3
`)
	r := Compress(code)
	assert.Equal(t, 3, r.SignaturesKept)
	assert.Equal(t, 3, r.BodiesRemoved)
	assert.Contains(t, r.Compressed, "def foo():")
	assert.Contains(t, r.Compressed, "def bar():")
	assert.Contains(t, r.Compressed, "def baz():")
}
