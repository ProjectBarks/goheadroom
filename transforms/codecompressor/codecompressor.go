// Package codecompressor provides AST-based code compression using tree-sitter.
// It preserves imports, type declarations, and function/method signatures while
// eliding function bodies to reduce token count for LLM context windows.
package codecompressor

import (
	"fmt"
	"strings"
	"sync"
	"unsafe"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_c "github.com/tree-sitter/tree-sitter-c/bindings/go"
	tree_sitter_cpp "github.com/tree-sitter/tree-sitter-cpp/bindings/go"
	tree_sitter_go "github.com/tree-sitter/tree-sitter-go/bindings/go"
	tree_sitter_java "github.com/tree-sitter/tree-sitter-java/bindings/go"
	tree_sitter_javascript "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
	tree_sitter_python "github.com/tree-sitter/tree-sitter-python/bindings/go"
	tree_sitter_rust "github.com/tree-sitter/tree-sitter-rust/bindings/go"
	tree_sitter_typescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

// Language represents a programming language supported by the compressor.
type Language int

const (
	Unknown    Language = iota
	Python
	JavaScript
	TypeScript
	Go
	Rust
	Java
	C
	CPP
)

// String returns the language name.
func (l Language) String() string {
	switch l {
	case Python:
		return "Python"
	case JavaScript:
		return "JavaScript"
	case TypeScript:
		return "TypeScript"
	case Go:
		return "Go"
	case Rust:
		return "Rust"
	case Java:
		return "Java"
	case C:
		return "C"
	case CPP:
		return "C++"
	default:
		return "Unknown"
	}
}

// Result holds compression output.
type Result struct {
	Compressed     string
	Language       Language
	Confidence     float32
	SignaturesKept int
	BodiesRemoved  int
}

// langConfig holds per-language AST node types and detection hints.
type langConfig struct {
	lang      Language
	tsLang    unsafe.Pointer
	pool      sync.Pool
	imports   map[string]bool
	funcs     map[string]bool
	classes   map[string]bool
	types     map[string]bool
	bodyNodes map[string]bool
	pkgNode   string // e.g. "package_clause" for Go
	usesColon bool   // Python uses colon+indentation, not braces
	comment   string // comment prefix for elision marker
	hints     []string
}

func newSet(items ...string) map[string]bool {
	m := make(map[string]bool, len(items))
	for _, s := range items {
		m[s] = true
	}
	return m
}

var configs []langConfig

func init() {
	configs = []langConfig{
		{
			lang:      Python,
			tsLang:    tree_sitter_python.Language(),
			imports:   newSet("import_statement", "import_from_statement"),
			funcs:     newSet("function_definition"),
			classes:   newSet("class_definition"),
			types:     newSet(),
			bodyNodes: newSet("block"),
			usesColon: true,
			comment:   "#",
			hints:     []string{"def ", "import ", "class ", "from ", "self."},
		},
		{
			lang:      JavaScript,
			tsLang:    tree_sitter_javascript.Language(),
			imports:   newSet("import_statement"),
			funcs:     newSet("function_declaration", "method_definition"),
			classes:   newSet(),
			types:     newSet(),
			bodyNodes: newSet("statement_block"),
			comment:   "//",
			hints:     []string{"function ", "const ", "let ", "var ", "=>"},
		},
		{
			lang:      TypeScript,
			tsLang:    tree_sitter_typescript.LanguageTypescript(),
			imports:   newSet("import_statement"),
			funcs:     newSet("function_declaration", "method_definition"),
			classes:   newSet(),
			types:     newSet("interface_declaration", "type_alias_declaration"),
			bodyNodes: newSet("statement_block"),
			comment:   "//",
			hints:     []string{"interface ", "type ", ": string", ": number", "export "},
		},
		{
			lang:      Go,
			tsLang:    tree_sitter_go.Language(),
			imports:   newSet("import_declaration"),
			funcs:     newSet("function_declaration", "method_declaration"),
			classes:   newSet(),
			types:     newSet("type_declaration"),
			bodyNodes: newSet("block"),
			pkgNode:   "package_clause",
			comment:   "//",
			hints:     []string{"func ", "package ", "go ", ":= "},
		},
		{
			lang:      Rust,
			tsLang:    tree_sitter_rust.Language(),
			imports:   newSet("use_declaration"),
			funcs:     newSet("function_item"),
			classes:   newSet("impl_item"),
			types:     newSet("struct_item", "enum_item", "trait_item"),
			bodyNodes: newSet("block"),
			comment:   "//",
			hints:     []string{"fn ", "let mut", "pub fn", "use ", "impl "},
		},
		{
			lang:      Java,
			tsLang:    tree_sitter_java.Language(),
			imports:   newSet("import_declaration"),
			funcs:     newSet("method_declaration", "constructor_declaration"),
			classes:   newSet("class_declaration"),
			types:     newSet(),
			bodyNodes: newSet("block"),
			comment:   "//",
			hints:     []string{"public class", "void ", "private ", "public "},
		},
		{
			lang:      C,
			tsLang:    tree_sitter_c.Language(),
			imports:   newSet("preproc_include"),
			funcs:     newSet("function_definition"),
			classes:   newSet(),
			types:     newSet("struct_specifier", "type_definition"),
			bodyNodes: newSet("compound_statement"),
			comment:   "//",
			hints:     []string{"#include", "int main(", "void ", "sizeof("},
		},
		{
			lang:      CPP,
			tsLang:    tree_sitter_cpp.Language(),
			imports:   newSet("preproc_include"),
			funcs:     newSet("function_definition"),
			classes:   newSet("class_specifier"),
			types:     newSet("struct_specifier", "type_definition"),
			bodyNodes: newSet("compound_statement", "field_declaration_list"),
			comment:   "//",
			hints:     []string{"namespace ", "class ", "::", "std::", "cout"},
		},
	}

	// Initialize parser pools for each language.
	for i := range configs {
		cfg := &configs[i]
		lang := tree_sitter.NewLanguage(cfg.tsLang)
		cfg.pool = sync.Pool{
			New: func() interface{} {
				p := tree_sitter.NewParser()
				_ = p.SetLanguage(lang)
				return p
			},
		}
	}
}

// DetectLanguage scores content against hint strings and returns the best match.
func DetectLanguage(content string) Language {
	if len(content) == 0 {
		return Unknown
	}

	bestLang := Unknown
	bestScore := 0

	for i := range configs {
		cfg := &configs[i]
		score := 0
		for _, hint := range cfg.hints {
			score += strings.Count(content, hint)
		}
		if score > bestScore {
			bestScore = score
			bestLang = cfg.lang
		}
	}
	return bestLang
}

// CanHandle returns true if content looks like code (has enough code hints).
func CanHandle(content string) bool {
	if len(strings.TrimSpace(content)) == 0 {
		return false
	}
	lang := DetectLanguage(content)
	return lang != Unknown
}

// configFor returns the langConfig for a given language.
func configFor(lang Language) *langConfig {
	for i := range configs {
		if configs[i].lang == lang {
			return &configs[i]
		}
	}
	return nil
}

func estimateTokens(s string) int {
	n := len(s) / 4
	if n < 1 {
		return 1
	}
	return n
}

// Compress detects the language, parses with tree-sitter, and compresses by
// eliding function/method bodies while preserving imports, types, and signatures.
func Compress(content string) Result {
	if len(strings.TrimSpace(content)) == 0 {
		return Result{Compressed: content, Language: Unknown}
	}

	lang := DetectLanguage(content)
	if lang == Unknown {
		return Result{
			Compressed: content,
			Language:   Unknown,
			Confidence: 0,
		}
	}

	// Match Python's min_tokens_for_compression=100 threshold
	if estimateTokens(content) < 100 {
		return Result{Compressed: content, Language: lang}
	}

	cfg := configFor(lang)
	if cfg == nil {
		return Result{Compressed: content, Language: lang}
	}

	parser := cfg.pool.Get().(*tree_sitter.Parser)
	defer cfg.pool.Put(parser)

	src := []byte(content)
	tree := parser.Parse(src, nil)
	if tree == nil {
		return Result{Compressed: content, Language: lang, Confidence: 0}
	}
	defer tree.Close()

	root := tree.RootNode()
	if root == nil {
		return Result{Compressed: content, Language: lang, Confidence: 0}
	}

	var result strings.Builder
	result.Grow(len(content))

	signaturesKept := 0
	bodiesRemoved := 0

	processChildren(root, cfg, src, &result, &signaturesKept, &bodiesRemoved)

	return Result{
		Compressed:     result.String(),
		Language:       lang,
		Confidence:     0.95,
		SignaturesKept: signaturesKept,
		BodiesRemoved:  bodiesRemoved,
	}
}

// containerNodes are node types that contain code children to recurse into
// (e.g. namespace_definition's declaration_list).
var containerNodes = map[string]string{
	"namespace_definition": "declaration_list",
}

// processChildren walks the children of a node and compresses them.
func processChildren(node *tree_sitter.Node, cfg *langConfig, src []byte, result *strings.Builder, signaturesKept, bodiesRemoved *int) {
	lastEnd := node.StartByte()
	// For root node, start from 0.
	if node.Kind() == "translation_unit" || node.Kind() == "module" || node.Kind() == "source_file" || node.Kind() == "program" {
		lastEnd = 0
	}

	childCount := node.ChildCount()
	for i := uint(0); i < childCount; i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}

		nodeKind := child.Kind()
		startByte := child.StartByte()
		endByte := child.EndByte()

		// Preserve gap between nodes.
		if startByte > lastEnd {
			result.Write(src[lastEnd:startByte])
		}

		switch {
		case cfg.pkgNode != "" && nodeKind == cfg.pkgNode:
			result.Write(src[startByte:endByte])

		case cfg.imports[nodeKind]:
			result.Write(src[startByte:endByte])

		case cfg.types[nodeKind]:
			result.Write(src[startByte:endByte])

		case cfg.funcs[nodeKind]:
			*signaturesKept++
			compressed, removed := compressNode(child, cfg, src)
			result.WriteString(compressed)
			*bodiesRemoved += removed

		case cfg.classes[nodeKind]:
			*signaturesKept++
			compressed, removed := compressClassNode(child, cfg, src)
			result.WriteString(compressed)
			*bodiesRemoved += removed

		default:
			// Check if this is a container node (e.g. namespace_definition)
			// that we should recurse into.
			if bodyField, ok := containerNodes[nodeKind]; ok {
				// Write everything up to the body child, then recurse into it.
				bodyChild := findChildByKind(child, bodyField)
				if bodyChild != nil {
					// Write prefix (e.g. "namespace myns {")
					result.Write(src[startByte:bodyChild.StartByte()])
					processChildren(bodyChild, cfg, src, result, signaturesKept, bodiesRemoved)
					// Write suffix after body child.
					if bodyChild.EndByte() < endByte {
						result.Write(src[bodyChild.EndByte():endByte])
					}
				} else {
					result.Write(src[startByte:endByte])
				}
			} else {
				result.Write(src[startByte:endByte])
			}
		}

		lastEnd = endByte
	}

	// Preserve any trailing content within the parent node.
	nodeEnd := uint(len(src))
	if node.Kind() != "translation_unit" && node.Kind() != "module" && node.Kind() != "source_file" && node.Kind() != "program" {
		nodeEnd = node.EndByte()
	}
	if lastEnd < nodeEnd {
		result.Write(src[lastEnd:nodeEnd])
	}
}

// findChildByKind finds a direct child node with the given kind.
func findChildByKind(node *tree_sitter.Node, kind string) *tree_sitter.Node {
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child != nil && child.Kind() == kind {
			return child
		}
	}
	return nil
}

// compressNode compresses a function node by preserving the signature and
// eliding the body.
func compressNode(node *tree_sitter.Node, cfg *langConfig, src []byte) (string, int) {
	// Find the body child node.
	bodyNode := findBodyChild(node, cfg)
	if bodyNode == nil {
		// No body found - preserve the whole node.
		return string(src[node.StartByte():node.EndByte()]), 0
	}

	bodyStart := bodyNode.StartByte()
	bodyEnd := bodyNode.EndByte()
	bodyText := string(src[bodyStart:bodyEnd])
	bodyLines := strings.Count(bodyText, "\n")
	if bodyLines == 0 {
		bodyLines = 1
	}

	// Signature is everything before the body.
	signature := string(src[node.StartByte():bodyStart])

	if cfg.usesColon {
		// Python: check for docstring as first statement in body.
		docstring := extractDocstring(bodyNode, src)
		indent := detectIndent(bodyNode, src)
		if docstring != "" {
			return fmt.Sprintf("%s\n%s%s\n%s%s [%d lines omitted]\n%spass",
				strings.TrimRight(signature, " "), indent, docstring, indent, cfg.comment, bodyLines, indent), 1
		}
		return fmt.Sprintf("%s\n%s%s [%d lines omitted]\n%spass",
			strings.TrimRight(signature, " "), indent, cfg.comment, bodyLines, indent), 1
	}

	// Brace language: replace body content.
	return fmt.Sprintf("%s{ %s [%d lines omitted] }", signature, cfg.comment, bodyLines), 1
}

// compressClassNode compresses a class/impl node, preserving the class
// declaration and compressing methods inside it.
func compressClassNode(node *tree_sitter.Node, cfg *langConfig, src []byte) (string, int) {
	// Find the body of the class.
	bodyNode := findBodyChild(node, cfg)
	if bodyNode == nil {
		return string(src[node.StartByte():node.EndByte()]), 0
	}

	var result strings.Builder
	// Write everything before the body.
	result.Write(src[node.StartByte():bodyNode.StartByte()])

	if cfg.usesColon {
		// Python class body.
		return compressPythonClassBody(node, bodyNode, cfg, src, &result)
	}

	// Brace-based class body.
	return compressBraceClassBody(bodyNode, cfg, src, &result)
}

func compressPythonClassBody(classNode, bodyNode *tree_sitter.Node, cfg *langConfig, src []byte, result *strings.Builder) (string, int) {
	bodiesRemoved := 0
	lastEnd := bodyNode.StartByte()

	childCount := bodyNode.ChildCount()
	for i := uint(0); i < childCount; i++ {
		child := bodyNode.Child(i)
		if child == nil {
			continue
		}

		startByte := child.StartByte()
		if startByte > lastEnd {
			result.Write(src[lastEnd:startByte])
		}

		if cfg.funcs[child.Kind()] {
			compressed, removed := compressNode(child, cfg, src)
			result.WriteString(compressed)
			bodiesRemoved += removed
		} else {
			result.Write(src[startByte:child.EndByte()])
		}

		lastEnd = child.EndByte()
	}

	// Write trailing content of body node.
	if lastEnd < bodyNode.EndByte() {
		result.Write(src[lastEnd:bodyNode.EndByte()])
	}
	// Write content after body node to end of class node.
	if bodyNode.EndByte() < classNode.EndByte() {
		result.Write(src[bodyNode.EndByte():classNode.EndByte()])
	}

	return result.String(), bodiesRemoved
}

func compressBraceClassBody(bodyNode *tree_sitter.Node, cfg *langConfig, src []byte, result *strings.Builder) (string, int) {
	bodiesRemoved := 0
	lastEnd := bodyNode.StartByte()

	childCount := bodyNode.ChildCount()
	for i := uint(0); i < childCount; i++ {
		child := bodyNode.Child(i)
		if child == nil {
			continue
		}

		startByte := child.StartByte()
		if startByte > lastEnd {
			result.Write(src[lastEnd:startByte])
		}

		if cfg.funcs[child.Kind()] {
			compressed, removed := compressNode(child, cfg, src)
			result.WriteString(compressed)
			bodiesRemoved += removed
		} else {
			result.Write(src[startByte:child.EndByte()])
		}

		lastEnd = child.EndByte()
	}

	if lastEnd < bodyNode.EndByte() {
		result.Write(src[lastEnd:bodyNode.EndByte()])
	}

	return result.String(), bodiesRemoved
}

// findBodyChild finds the body/block child of a function/class node.
func findBodyChild(node *tree_sitter.Node, cfg *langConfig) *tree_sitter.Node {
	childCount := node.ChildCount()
	for i := uint(0); i < childCount; i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		if cfg.bodyNodes[child.Kind()] {
			return child
		}
	}
	// Also check by field name "body".
	body := node.ChildByFieldName("body")
	if body != nil {
		return body
	}
	return nil
}

// extractDocstring checks if the first statement in a Python body block
// is an expression_statement containing a string (docstring).
func extractDocstring(bodyNode *tree_sitter.Node, src []byte) string {
	if bodyNode.NamedChildCount() == 0 {
		return ""
	}
	first := bodyNode.NamedChild(0)
	if first == nil {
		return ""
	}
	if first.Kind() == "expression_statement" {
		if first.NamedChildCount() > 0 {
			expr := first.NamedChild(0)
			if expr != nil && expr.Kind() == "string" {
				return string(src[first.StartByte():first.EndByte()])
			}
		}
	}
	return ""
}

// detectIndent returns the indentation string for a body node.
func detectIndent(bodyNode *tree_sitter.Node, src []byte) string {
	// Look at the start of the body node's first child line.
	if bodyNode.NamedChildCount() > 0 {
		first := bodyNode.NamedChild(0)
		if first != nil {
			lineStart := first.StartByte()
			// Walk backwards to find the start of the line.
			for lineStart > 0 && src[lineStart-1] != '\n' {
				lineStart--
			}
			indent := src[lineStart:first.StartByte()]
			return string(indent)
		}
	}
	return "    "
}
