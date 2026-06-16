package reformats

import (
	"fmt"
	"strings"

	"github.com/projectbarks/goheadroom/core/transforms/contentdetector"
	"github.com/projectbarks/goheadroom/core/transforms/pipeline"
)

const logTemplateName = "log_template"

// Wildcard is the sentinel for variable positions in template strings.
const Wildcard = "<*>"

// LogTemplate is a Drain-inspired log-template miner that collapses
// runs of consecutive same-template lines into a template header plus
// a compact variant table. Order-preserving and lossless.
type LogTemplate struct {
	config pipeline.LogTemplateConfig
}

// NewLogTemplate creates a LogTemplate with the given config.
func NewLogTemplate(config pipeline.LogTemplateConfig) *LogTemplate {
	return &LogTemplate{config: config}
}

func (l *LogTemplate) Name() string { return logTemplateName }

func (l *LogTemplate) AppliesTo() []contentdetector.ContentType {
	return []contentdetector.ContentType{contentdetector.BuildOutput}
}

func (l *LogTemplate) Apply(content string) (*pipeline.ReformatOutput, error) {
	if content == "" {
		return nil, pipeline.Skipped(logTemplateName, "empty input")
	}
	lines := strings.Split(content, "\n")
	// Remove trailing empty element from Split if content ends with \n
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) < l.config.MinLines {
		return nil, pipeline.Skipped(logTemplateName, "input below min_lines")
	}

	tokenized := make([][]string, len(lines))
	for i, line := range lines {
		tokenized[i] = tokenize(line)
	}

	var output strings.Builder
	output.Grow(len(content))
	nextTemplateID := 1
	var run *logRun

	for i, tokens := range tokenized {
		if len(tokens) == 0 {
			// Blank line breaks any active run.
			if run != nil {
				l.flushRun(run, lines, tokenized, &nextTemplateID, &output)
				run = nil
			}
			output.WriteString(lines[i])
			output.WriteByte('\n')
			continue
		}
		if run != nil && extendsRun(run, tokens, l.config.SimilarityThreshold) {
			run.indices = append(run.indices, i)
			mergeIntoTemplate(run.template, tokens)
		} else {
			if run != nil {
				l.flushRun(run, lines, tokenized, &nextTemplateID, &output)
			}
			run = newLogRun(i, tokens)
		}
	}
	if run != nil {
		l.flushRun(run, lines, tokenized, &nextTemplateID, &output)
	}

	result := output.String()

	// Trailing newline: if the input had one, restore it.
	// Otherwise drop our final '\n' so we don't grow the output.
	if strings.HasSuffix(content, "\n") {
		// Output already ends in '\n' from the last push; nothing to do.
	} else if strings.HasSuffix(result, "\n") {
		result = result[:len(result)-1]
	}

	// Defensive: never inflate. Fall back to original.
	if len(result) >= len(content) {
		out := pipeline.ReformatOutputFromLengths(len(content), content)
		return &out, nil
	}

	out := pipeline.ReformatOutputFromLengths(len(content), result)
	return &out, nil
}

func (l *LogTemplate) flushRun(run *logRun, lines []string, tokenized [][]string, nextTemplateID *int, out *strings.Builder) {
	constantCount := 0
	for _, slot := range run.template {
		if slot != nil {
			constantCount++
		}
	}
	varyingCount := len(run.template) - constantCount

	collapse := len(run.indices) >= l.config.MinRun &&
		constantCount >= l.config.MinConstantTokens &&
		varyingCount > 0

	if !collapse {
		for _, idx := range run.indices {
			out.WriteString(lines[idx])
			out.WriteByte('\n')
		}
		return
	}

	// Emit "[Template T<id>: TOKEN <*> TOKEN ...] (N occurrences)"
	templateID := *nextTemplateID
	*nextTemplateID++
	out.WriteString("[Template T")
	out.WriteString(fmt.Sprintf("%d", templateID))
	out.WriteString(": ")
	for pos, slot := range run.template {
		if pos > 0 {
			out.WriteByte(' ')
		}
		if slot != nil {
			out.WriteString(*slot)
		} else {
			out.WriteString(Wildcard)
		}
	}
	out.WriteString("] (")
	out.WriteString(fmt.Sprintf("%d", len(run.indices)))
	out.WriteString(" occurrences)\n")

	// Variant table: per line, emit only the variable-position tokens.
	for _, idx := range run.indices {
		toks := tokenized[idx]
		first := true
		for pos, slot := range run.template {
			if slot == nil {
				if !first {
					out.WriteByte(' ')
				}
				out.WriteString(toks[pos])
				first = false
			}
		}
		out.WriteByte('\n')
	}
}

// logRun tracks an in-flight collapse candidate.
type logRun struct {
	indices  []int
	template []*string // nil = wildcard (variable position)
}

func newLogRun(idx int, tokens []string) *logRun {
	template := make([]*string, len(tokens))
	for i, tok := range tokens {
		s := tok
		template[i] = &s
	}
	return &logRun{
		indices:  []int{idx},
		template: template,
	}
}

// extendsRun checks if tokens match the run's template at >= threshold.
func extendsRun(run *logRun, tokens []string, simThreshold float32) bool {
	if len(tokens) != len(run.template) {
		return false
	}
	length := float32(len(tokens))
	var matches float32
	for pos, tok := range tokens {
		switch {
		case run.template[pos] != nil && *run.template[pos] == tok:
			matches++
		case run.template[pos] == nil:
			matches++ // already a wildcard; counts as match
		}
	}
	return (matches / length) >= simThreshold
}

// mergeIntoTemplate updates template in place: positions where tokens
// differ become wildcards (nil).
func mergeIntoTemplate(template []*string, tokens []string) {
	for pos, tok := range tokens {
		if template[pos] != nil && *template[pos] != tok {
			template[pos] = nil
		}
	}
}

// tokenize splits a line on whitespace. Empty result = blank line.
func tokenize(line string) []string {
	return strings.Fields(line)
}

// Verify interface compliance at compile time.
var _ pipeline.ReformatTransform = (*LogTemplate)(nil)
