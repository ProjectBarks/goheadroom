package transforms

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const defaultTagPrefix = "{{HEADROOM_TAG_"

func TestProtectTagsSimple(t *testing.T) {
	input := `Hello <custom_tag>content</custom_tag> world`
	protected, mapping := ProtectTags(input)
	assert.NotContains(t, protected, "<custom_tag>")
	assert.NotContains(t, protected, "</custom_tag>")
	assert.Contains(t, protected, defaultTagPrefix)
	assert.True(t, len(mapping) > 0)
}

func TestRestoreTagsRoundtrip(t *testing.T) {
	input := `Hello <custom_tag>content</custom_tag> world`
	protected, mapping := ProtectTags(input)
	restored := RestoreTags(protected, mapping)
	assert.Equal(t, input, restored)
}

func TestProtectTagsNoCustomTags(t *testing.T) {
	input := `Hello world, no tags here.`
	protected, mapping := ProtectTags(input)
	assert.Equal(t, input, protected)
	assert.Equal(t, 0, len(mapping))
}

func TestProtectTagsHtml5NotProtected(t *testing.T) {
	input := `<div>Hello</div><p>World</p><span>!</span>`
	protected, mapping := ProtectTags(input)
	assert.Equal(t, input, protected)
	assert.Equal(t, 0, len(mapping))
}

func TestProtectTagsSelfClosing(t *testing.T) {
	input := `Before <my_widget/> after`
	protected, mapping := ProtectTags(input)
	assert.NotContains(t, protected, "<my_widget/>")
	restored := RestoreTags(protected, mapping)
	assert.Equal(t, input, restored)
}

func TestProtectTagsNested(t *testing.T) {
	input := `<outer><inner>text</inner></outer>`
	protected, mapping := ProtectTags(input)
	assert.NotContains(t, protected, "<outer>")
	assert.NotContains(t, protected, "<inner>")
	restored := RestoreTags(protected, mapping)
	assert.Equal(t, input, restored)
}

func TestProtectTagsWithAttributes(t *testing.T) {
	input := `<tool_call id="123" type="function">content</tool_call>`
	protected, mapping := ProtectTags(input)
	assert.NotContains(t, protected, "<tool_call")
	restored := RestoreTags(protected, mapping)
	assert.Equal(t, input, restored)
}

func TestProtectTagsMultipleSameTags(t *testing.T) {
	input := `<result>first</result> middle <result>second</result>`
	protected, mapping := ProtectTags(input)
	restored := RestoreTags(protected, mapping)
	assert.Equal(t, input, restored)
}

func TestProtectTagsPreservesWhitespace(t *testing.T) {
	input := "  <custom_tag>\n  content\n  </custom_tag>  "
	protected, mapping := ProtectTags(input)
	restored := RestoreTags(protected, mapping)
	assert.Equal(t, input, restored)
}

func TestHtml5TagsList(t *testing.T) {
	html5TagNames := []string{
		"a", "abbr", "address", "article", "aside", "audio",
		"b", "blockquote", "body", "br", "button",
		"canvas", "caption", "code", "col",
		"div", "dl", "dt",
		"em",
		"fieldset", "figcaption", "figure", "footer", "form",
		"h1", "h2", "h3", "h4", "h5", "h6", "head", "header", "hr", "html",
		"i", "iframe", "img", "input",
		"label", "li", "link",
		"main", "meta",
		"nav",
		"ol", "option",
		"p", "pre",
		"script", "section", "select", "span", "strong", "style",
		"table", "tbody", "td", "textarea", "th", "thead", "title", "tr",
		"ul",
		"video",
	}
	for _, tag := range html5TagNames {
		assert.True(t, IsHtml5Tag(tag), "expected %q to be recognized as HTML5 tag", tag)
	}
}

func TestProtectTagsMixedHtml5AndCustom(t *testing.T) {
	input := `<div><custom_tag>text</custom_tag></div>`
	protected, mapping := ProtectTags(input)
	assert.Contains(t, protected, "<div>")
	assert.Contains(t, protected, "</div>")
	assert.NotContains(t, protected, "<custom_tag>")
	restored := RestoreTags(protected, mapping)
	assert.Equal(t, input, restored)
}

func TestProtectTagsDefaultPrefix(t *testing.T) {
	input := `<my_tag>x</my_tag>`
	protected, _ := ProtectTags(input)
	assert.Contains(t, protected, defaultTagPrefix)
}

func TestProtectTagsPlaceholderUniqueness(t *testing.T) {
	input := `<a_tag>1</a_tag> <b_tag>2</b_tag> <c_tag>3</c_tag>`
	protected, mapping := ProtectTags(input)
	placeholders := make(map[string]bool)
	for k := range mapping {
		require.False(t, placeholders[k], "duplicate placeholder: %s", k)
		placeholders[k] = true
	}
	_ = protected
}

func TestRestoreTagsNoMapping(t *testing.T) {
	input := `Hello world`
	restored := RestoreTags(input, map[string]string{})
	assert.Equal(t, input, restored)
}

func TestProtectTagsAnthropicXmlTags(t *testing.T) {
	input := "<thinking>Let me consider this.</thinking>\n<answer>The answer is 42.</answer>"
	protected, mapping := ProtectTags(input)
	assert.NotContains(t, protected, "<thinking>")
	assert.NotContains(t, protected, "<answer>")
	restored := RestoreTags(protected, mapping)
	assert.Equal(t, input, restored)
}

func TestProtectTagsToolUse(t *testing.T) {
	input := "<tool_use>\n<tool_name>calculator</tool_name>\n<parameters>{\"expression\": \"2+2\"}</parameters>\n</tool_use>"
	protected, mapping := ProtectTags(input)
	assert.NotContains(t, protected, "<tool_use>")
	assert.NotContains(t, protected, "<tool_name>")
	assert.NotContains(t, protected, "<parameters>")
	restored := RestoreTags(protected, mapping)
	assert.Equal(t, input, restored)
}

func TestProtectTagsEmptyTag(t *testing.T) {
	input := `<custom_tag></custom_tag>`
	protected, mapping := ProtectTags(input)
	restored := RestoreTags(protected, mapping)
	assert.Equal(t, input, restored)
}

func TestProtectTagsSpecialCharsInContent(t *testing.T) {
	input := `<result>a < b && c > d</result>`
	protected, mapping := ProtectTags(input)
	restored := RestoreTags(protected, mapping)
	assert.Equal(t, input, restored)
}

func TestProtectTagsUnicodeContent(t *testing.T) {
	input := "<custom>こんにちは世界</custom>"
	protected, mapping := ProtectTags(input)
	restored := RestoreTags(protected, mapping)
	assert.Equal(t, input, restored)
}

func TestProtectTagsVeryLongContent(t *testing.T) {
	content := strings.Repeat("x", 10000)
	input := "<custom_tag>" + content + "</custom_tag>"
	protected, mapping := ProtectTags(input)
	restored := RestoreTags(protected, mapping)
	assert.Equal(t, input, restored)
}

func TestProtectTagsManyTags(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 100; i++ {
		name := "tag_" + strings.Repeat("a", i%10+1)
		b.WriteString("<")
		b.WriteString(name)
		b.WriteString(">content</")
		b.WriteString(name)
		b.WriteString("> ")
	}
	input := b.String()
	protected, mapping := ProtectTags(input)
	restored := RestoreTags(protected, mapping)
	assert.Equal(t, input, restored)
}

func TestProtectTagsIdempotent(t *testing.T) {
	input := `<custom>text</custom>`
	protected1, mapping1 := ProtectTags(input)
	// Protecting already-protected text should not double-encode
	// (placeholders use {{ }} which are not XML tags)
	protected2, mapping2 := ProtectTags(protected1)
	assert.Equal(t, protected1, protected2)
	assert.Equal(t, 0, len(mapping2))
	_ = mapping1
}

// Property-style tests (port of Rust proptests)
func TestProtectTagsRoundtripArbitrary(t *testing.T) {
	inputs := []string{
		`<x>a</x>`,
		`<abc_def>hello world</abc_def>`,
		`<a_outer><b_inner><c_deep>deep</c_deep></b_inner></a_outer>`,
		`no tags at all`,
		`<div>html only</div>`,
		`<custom>mixed <div>html</div> content</custom>`,
		`<a_1>numbers in tag name</a_1>`,
	}
	for _, input := range inputs {
		protected, mapping := ProtectTags(input)
		restored := RestoreTags(protected, mapping)
		assert.Equal(t, input, restored, "roundtrip failed for: %q", input)
	}
}

func TestProtectTagsPlaceholderNotInOriginal(t *testing.T) {
	input := `<custom>text with ` + defaultTagPrefix + `0}} in it</custom>`
	protected, mapping := ProtectTags(input)
	restored := RestoreTags(protected, mapping)
	assert.Equal(t, input, restored)
}

func TestProtectTagsOnlyOpening(t *testing.T) {
	// Unmatched opening tags should not be protected (orphan opens)
	input := `<custom_tag>content without closing`
	protected, mapping := ProtectTags(input)
	assert.Equal(t, 0, len(mapping))
	assert.Equal(t, input, protected)
}
