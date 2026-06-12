package contentdetector

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMapMagikaLabelPython(t *testing.T) {
	ct := MapMagikaLabel("python")
	assert.Equal(t, SourceCode, ct)
}

func TestMapMagikaLabelJavascript(t *testing.T) {
	ct := MapMagikaLabel("javascript")
	assert.Equal(t, SourceCode, ct)
}

func TestMapMagikaLabelHtml(t *testing.T) {
	ct := MapMagikaLabel("html")
	assert.Equal(t, Html, ct)
}

func TestMapMagikaLabelJson(t *testing.T) {
	ct := MapMagikaLabel("json")
	assert.Equal(t, JsonArray, ct)
}

func TestMapMagikaLabelShell(t *testing.T) {
	ct := MapMagikaLabel("shell")
	assert.Equal(t, SourceCode, ct)
}

func TestMapMagikaLabelMarkdown(t *testing.T) {
	ct := MapMagikaLabel("markdown")
	assert.Equal(t, PlainText, ct)
}

func TestMapMagikaLabelTxt(t *testing.T) {
	ct := MapMagikaLabel("txt")
	assert.Equal(t, PlainText, ct)
}

func TestMapMagikaLabelUnknown(t *testing.T) {
	ct := MapMagikaLabel("unknown_format_xyz")
	assert.Equal(t, PlainText, ct)
}

func TestMapMagikaLabelCpp(t *testing.T) {
	ct := MapMagikaLabel("c")
	assert.Equal(t, SourceCode, ct)
}

func TestMapMagikaLabelRust(t *testing.T) {
	ct := MapMagikaLabel("rust")
	assert.Equal(t, SourceCode, ct)
}

func TestMapMagikaLabelGo(t *testing.T) {
	ct := MapMagikaLabel("go")
	assert.Equal(t, SourceCode, ct)
}

func TestMagikaDetectErrorNoModel(t *testing.T) {
	_, err := MagikaDetect("some text content", "/nonexistent/model.onnx")
	require.Error(t, err)
	var magikaErr *MagikaDetectorError
	assert.ErrorAs(t, err, &magikaErr)
}

func TestMapMagikaLabelAllCodeLanguages(t *testing.T) {
	codeLabels := []string{
		"python", "javascript", "typescript", "java", "c", "cpp",
		"rust", "go", "ruby", "php", "swift", "kotlin", "scala",
		"perl", "lua", "shell", "powershell", "sql", "css",
		"yaml", "toml", "dockerfile",
		"makefile", "cmake",
	}
	for _, label := range codeLabels {
		ct := MapMagikaLabel(label)
		assert.Equal(t, SourceCode, ct, "label %q should map to SourceCode", label)
	}
}

func TestMapMagikaLabelXmlMapsToHtml(t *testing.T) {
	ct := MapMagikaLabel("xml")
	assert.Equal(t, Html, ct)
}
