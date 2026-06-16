package contentdetector

import "fmt"

// MagikaDetectorError represents errors from Magika ONNX detection.
type MagikaDetectorError struct {
	Kind    string
	Message string
}

func (e *MagikaDetectorError) Error() string {
	return fmt.Sprintf("magika detector error (%s): %s", e.Kind, e.Message)
}

// MapMagikaLabel maps a Magika output label string to a ContentType.
// Port of Rust map_magika_label() with 30+ label mappings.
func MapMagikaLabel(label string) ContentType {
	switch label {
	// JSON
	case "json", "jsonl":
		return JsonArray

	// Diffs
	case "diff":
		return GitDiff

	// HTML / XML
	case "html", "xml":
		return Html

	// Source code
	case "rust", "python", "javascript", "typescript", "go", "java", "c", "cpp", "cs",
		"php", "ruby", "swift", "kotlin", "scala", "haskell", "lua", "dart", "perl",
		"shell", "powershell", "batch", "sql", "css", "vue", "groovy", "clojure",
		"asm", "cmake", "dockerfile", "makefile", "yaml", "toml", "ini", "hcl",
		"jinja", "r", "scss", "less", "gradle", "maven":
		return SourceCode

	// Plain text
	case "markdown", "rst", "latex", "txt", "empty", "unknown", "undefined":
		return PlainText

	// Default: passthrough
	default:
		return PlainText
	}
}
