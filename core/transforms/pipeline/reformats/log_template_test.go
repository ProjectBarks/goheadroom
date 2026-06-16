package reformats

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/projectbarks/goheadroom/core/transforms/contentdetector"
	"github.com/projectbarks/goheadroom/core/transforms/pipeline"
)

func defaultLogTemplateCfg() pipeline.LogTemplateConfig {
	cfg, _ := pipeline.DefaultPipelineConfig()
	return cfg.Reformat.LogTemplate
}

func logTemplate() *LogTemplate {
	return NewLogTemplate(defaultLogTemplateCfg())
}

func TestLogTemplateNameAndAppliesTo(t *testing.T) {
	lt := logTemplate()
	assert.Equal(t, "log_template", lt.Name())
	assert.Equal(t, []contentdetector.ContentType{contentdetector.BuildOutput}, lt.AppliesTo())
}

func TestLogTemplateEmptyInputSkipped(t *testing.T) {
	_, err := logTemplate().Apply("")
	require.Error(t, err)
	te, ok := err.(pipeline.TransformError)
	require.True(t, ok)
	assert.Equal(t, pipeline.ErrorSkipped, te.Kind)
}

func TestLogTemplateBelowMinLinesSkipped(t *testing.T) {
	log := "INFO a\nINFO b\nINFO c\n"
	_, err := logTemplate().Apply(log)
	require.Error(t, err)
	te, ok := err.(pipeline.TransformError)
	require.True(t, ok)
	assert.Equal(t, pipeline.ErrorSkipped, te.Kind)
}

func TestLogTemplateTemplatedRunCollapses(t *testing.T) {
	// 50 INFO lines with varying timestamp + worker + job
	var b strings.Builder
	for i := 0; i < 50; i++ {
		b.WriteString(fmt.Sprintf("2025-01-15T12:34:%02d INFO worker-%d processing job %d\n", i, i, 100+i))
	}
	r, err := logTemplate().Apply(b.String())
	require.NoError(t, err)
	assert.Greater(t, r.BytesSaved, 0)
	assert.Contains(t, r.Output, "[Template T1:")
	assert.Contains(t, r.Output, "(50 occurrences)")
	assert.Contains(t, r.Output, "worker-7")
}

func TestLogTemplateOrderPreservedAcrossTwoTemplates(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 12; i++ {
		b.WriteString(fmt.Sprintf("INFO worker-%d starting\n", i))
	}
	for i := 0; i < 12; i++ {
		b.WriteString(fmt.Sprintf("WARN cache key-%d expired\n", i))
	}
	r, err := logTemplate().Apply(b.String())
	require.NoError(t, err)
	t1Pos := strings.Index(r.Output, "[Template T1:")
	t2Pos := strings.Index(r.Output, "[Template T2:")
	require.NotEqual(t, -1, t1Pos, "T1 header missing")
	require.NotEqual(t, -1, t2Pos, "T2 header missing")
	assert.Less(t, t1Pos, t2Pos, "templates must be in input order")
	t1Line := r.Output[t1Pos : strings.Index(r.Output[t1Pos:], "\n")+t1Pos]
	assert.Contains(t, t1Line, "INFO")
	assert.Contains(t, t1Line, "starting")
}

func TestLogTemplateLosslessRoundTrip(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 25; i++ {
		b.WriteString(fmt.Sprintf("TOK1 TOK2 var%d TOK3\n", i))
	}
	r, err := logTemplate().Apply(b.String())
	require.NoError(t, err)
	// Verify template header exists
	assert.Contains(t, r.Output, "[Template T1:")
	// All variant values must survive
	for i := 0; i < 25; i++ {
		assert.Contains(t, r.Output, fmt.Sprintf("var%d", i))
	}
}

func TestLogTemplateShortRunBelowMinRunEmittedVerbatim(t *testing.T) {
	var b strings.Builder
	// Heterogeneous prefix
	for i := 0; i < 10; i++ {
		var toks []string
		for j := 0; j < (i+1)%5+2; j++ {
			toks = append(toks, fmt.Sprintf("p%dq%d", i, j))
		}
		b.WriteString(strings.Join(toks, " "))
		b.WriteByte('\n')
	}
	// The 2-line "would-be template"
	b.WriteString("AAA worker-1 BBB\n")
	b.WriteString("AAA worker-2 BBB\n")
	// Heterogeneous suffix
	for i := 0; i < 10; i++ {
		var toks []string
		for j := 0; j < (i+1)%4+2; j++ {
			toks = append(toks, fmt.Sprintf("s%dt%d", i, j))
		}
		b.WriteString(strings.Join(toks, " "))
		b.WriteByte('\n')
	}
	r, err := logTemplate().Apply(b.String())
	require.NoError(t, err)
	assert.Contains(t, r.Output, "AAA worker-1 BBB")
	assert.Contains(t, r.Output, "AAA worker-2 BBB")
}

func TestLogTemplateAllUniqueLinesSurvive(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 25; i++ {
		b.WriteString(fmt.Sprintf("event-%d type-%d status-%d\n", i, i, i))
	}
	r, err := logTemplate().Apply(b.String())
	require.NoError(t, err)
	for i := 0; i < 25; i++ {
		assert.Contains(t, r.Output, fmt.Sprintf("event-%d", i))
	}
}

func TestLogTemplateBlankLinesBreakRuns(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 5; i++ {
		b.WriteString(fmt.Sprintf("INFO worker-%d ready\n", i))
	}
	b.WriteByte('\n')
	for i := 0; i < 5; i++ {
		b.WriteString(fmt.Sprintf("INFO worker-%d ready\n", i))
	}
	// Pad to clear min_lines
	for i := 0; i < 15; i++ {
		b.WriteString(fmt.Sprintf("misc-%d\n", i))
	}
	r, err := logTemplate().Apply(b.String())
	require.NoError(t, err)
	t1Count := strings.Count(r.Output, "[Template T1:")
	t2Count := strings.Count(r.Output, "[Template T2:")
	if t1Count == 1 && t2Count == 0 {
		assert.NotContains(t, r.Output, "(10 occurrences)",
			"must not bridge the blank line")
	}
}

func TestLogTemplateNeverInflatesOutput(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 30; i++ {
		b.WriteString(fmt.Sprintf("a%d\n", i))
	}
	input := b.String()
	r, err := logTemplate().Apply(input)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(r.Output), len(input))
}

func TestLogTemplateUnicodeTokensSurvive(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 30; i++ {
		b.WriteString(fmt.Sprintf("INFO fire worker-%d hello world\n", i))
	}
	r, err := logTemplate().Apply(b.String())
	require.NoError(t, err)
	assert.Contains(t, r.Output, "fire")
	assert.True(t, strings.Contains(r.Output, "hello") || strings.Contains(r.Output, "world"))
}

func TestLogTemplateAllWildcardTemplateEmitsVerbatim(t *testing.T) {
	// 30 lines where every position varies
	var b strings.Builder
	for i := 0; i < 30; i++ {
		b.WriteString(fmt.Sprintf("%d %d %d\n", i, i+1, i+2))
	}
	r, err := logTemplate().Apply(b.String())
	require.NoError(t, err)
	assert.NotContains(t, r.Output, "[Template")
}
