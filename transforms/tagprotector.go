package transforms

import (
	"fmt"
	"regexp"
	"strings"
)

// DefaultPrefix is the default placeholder prefix for protected tags.
const DefaultPrefix = "{{HEADROOM_TAG_"

// DefaultSuffix is the placeholder suffix.
const DefaultSuffix = "}}"

// html5Tags is the set of standard HTML5 tag names that should NOT be protected.
// Matches the Rust/Python KNOWN_HTML_TAGS set element-for-element.
var html5Tags = map[string]bool{
	// Main root
	"html": true,
	// Document metadata
	"base": true, "head": true, "link": true, "meta": true, "style": true, "title": true,
	// Sectioning root
	"body": true,
	// Content sectioning
	"address": true, "article": true, "aside": true, "footer": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"header": true, "hgroup": true, "main": true, "nav": true, "section": true, "search": true,
	// Text content
	"blockquote": true, "dd": true, "div": true, "dl": true, "dt": true,
	"figcaption": true, "figure": true, "hr": true, "li": true, "menu": true,
	"ol": true, "p": true, "pre": true, "ul": true,
	// Inline text semantics
	"a": true, "abbr": true, "b": true, "bdi": true, "bdo": true, "br": true,
	"cite": true, "code": true, "data": true, "dfn": true, "em": true, "i": true,
	"kbd": true, "mark": true, "q": true, "rp": true, "rt": true, "ruby": true,
	"s": true, "samp": true, "small": true, "span": true, "strong": true,
	"sub": true, "sup": true, "time": true, "u": true, "var": true, "wbr": true,
	// Image and multimedia
	"area": true, "audio": true, "img": true, "map": true, "track": true, "video": true,
	// Embedded content
	"embed": true, "iframe": true, "object": true, "param": true,
	"picture": true, "portal": true, "source": true,
	// SVG and MathML
	"svg": true, "math": true,
	// Scripting
	"canvas": true, "noscript": true, "script": true,
	// Demarcating edits
	"del": true, "ins": true,
	// Table content
	"caption": true, "col": true, "colgroup": true, "table": true,
	"tbody": true, "td": true, "tfoot": true, "th": true, "thead": true, "tr": true,
	// Forms
	"button": true, "datalist": true, "fieldset": true, "form": true,
	"input": true, "label": true, "legend": true, "meter": true,
	"optgroup": true, "option": true, "output": true, "progress": true,
	"select": true, "textarea": true,
	// Interactive
	"details": true, "dialog": true, "summary": true,
	// Web Components
	"slot": true, "template": true,
}

// IsHtml5Tag returns true if the tag name is a standard HTML5 tag.
func IsHtml5Tag(name string) bool {
	return html5Tags[strings.ToLower(name)]
}

// tagRegex matches opening, closing, and self-closing tags.
var tagRegex = regexp.MustCompile(`<(/?)([a-zA-Z_][\w\-.:]*)((?:\s+[^>]*?)?)(/?)>`)

// ProtectTags replaces custom XML-like tags with placeholders, leaving HTML5 tags intact.
// Returns the modified text and a mapping of placeholder -> original tag.
func ProtectTags(text string) (string, map[string]string) {
	if text == "" || !strings.Contains(text, "<") {
		return text, map[string]string{}
	}

	prefix := DefaultPrefix
	if strings.Contains(text, DefaultPrefix) {
		// Collision avoidance: find a salted prefix
		for salt := 0; salt < 16; salt++ {
			candidate := fmt.Sprintf("{{HEADROOM_TAG_%d_", salt)
			if !strings.Contains(text, candidate) {
				prefix = candidate
				break
			}
		}
	}

	// Phase 1: identify spans to protect using a tag-stack walker
	type openTag struct {
		nameLower string
		startPos  int
	}

	type span struct {
		start int
		end   int
	}

	var spans []span
	var stack []openTag
	bytes := []byte(text)
	n := len(bytes)

	i := 0
	for i < n {
		if bytes[i] != '<' {
			i++
			continue
		}

		// Try to parse a tag at position i
		tagMatch := tagRegex.FindIndex(bytes[i:])
		if tagMatch == nil || tagMatch[0] != 0 {
			i++
			continue
		}

		fullTag := string(bytes[i : i+tagMatch[1]])
		submatch := tagRegex.FindStringSubmatch(fullTag)
		if submatch == nil {
			i++
			continue
		}

		isClose := submatch[1] == "/"
		tagName := submatch[2]
		isSelfClosing := submatch[4] == "/"
		tagEnd := i + tagMatch[1]

		// Skip HTML5 tags
		if IsHtml5Tag(tagName) {
			i = tagEnd
			continue
		}

		if isSelfClosing && !isClose {
			// Self-closing custom tag
			spans = append(spans, span{start: i, end: tagEnd})
			i = tagEnd
			continue
		}

		if isClose {
			// Closing tag -- find matching open on stack
			nameLower := strings.ToLower(tagName)
			matchIdx := -1
			for j := len(stack) - 1; j >= 0; j-- {
				if stack[j].nameLower == nameLower {
					matchIdx = j
					break
				}
			}
			if matchIdx >= 0 {
				openStart := stack[matchIdx].startPos
				// Remove inner spans subsumed by this block
				filtered := spans[:0]
				for _, s := range spans {
					if s.start < openStart {
						filtered = append(filtered, s)
					}
				}
				spans = filtered
				spans = append(spans, span{start: openStart, end: tagEnd})
				stack = stack[:matchIdx]
			}
			// Orphan close -- emit verbatim
			i = tagEnd
			continue
		}

		// Opening custom tag -- push to stack
		stack = append(stack, openTag{
			nameLower: strings.ToLower(tagName),
			startPos:  i,
		})
		i = tagEnd
	}

	// Phase 2: build output with placeholders
	if len(spans) == 0 {
		return text, map[string]string{}
	}

	mapping := map[string]string{}
	var out strings.Builder
	out.Grow(len(text))
	cursor := 0
	counter := 0

	for _, s := range spans {
		if s.start < cursor {
			continue
		}
		out.WriteString(text[cursor:s.start])
		placeholder := fmt.Sprintf("%s%d%s", prefix, counter, DefaultSuffix)
		original := text[s.start:s.end]
		mapping[placeholder] = original
		out.WriteString(placeholder)
		cursor = s.end
		counter++
	}
	out.WriteString(text[cursor:])

	return out.String(), mapping
}

// RestoreTags replaces placeholders back with their original tags.
func RestoreTags(text string, mapping map[string]string) string {
	if len(mapping) == 0 {
		return text
	}
	result := text
	for placeholder, original := range mapping {
		if strings.Contains(result, placeholder) {
			result = strings.Replace(result, placeholder, original, 1)
		}
	}
	return result
}
