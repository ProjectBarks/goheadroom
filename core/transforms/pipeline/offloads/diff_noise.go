package offloads

import (
	"fmt"
	"strings"

	"github.com/projectbarks/goheadroom/core/ccr"
	"github.com/projectbarks/goheadroom/core/transforms/contentdetector"
	"github.com/projectbarks/goheadroom/core/transforms/pipeline"
)

const diffNoiseName = "diff_noise"
const diffNoiseConfidence float32 = 0.9

// DiffNoise drops hunks the LLM does not need: lockfile churn and
// whitespace-only changes. Stashes the original via CCR.
type DiffNoise struct {
	config pipeline.DiffNoiseConfig
}

// NewDiffNoise creates a DiffNoise with the given config.
func NewDiffNoise(config pipeline.DiffNoiseConfig) *DiffNoise {
	return &DiffNoise{config: config}
}

func (d *DiffNoise) Name() string { return diffNoiseName }

func (d *DiffNoise) AppliesTo() []contentdetector.ContentType {
	return []contentdetector.ContentType{contentdetector.GitDiff}
}

func (d *DiffNoise) EstimateBloat(content string) float32 {
	if content == "" {
		return 0.0
	}
	totalLines := strings.Count(content, "\n") + 1
	if totalLines < d.config.MinLines {
		return 0.0
	}
	segments := parseSegments(content)
	if len(segments) == 0 {
		return 0.0
	}

	var droppableBytes, totalBytes int
	for _, seg := range segments {
		bodyBytes := 0
		for _, line := range seg.bodyLines {
			bodyBytes += len(line) + 1
		}
		totalBytes += bodyBytes
		if d.isLockfile(seg.newPath) || (d.config.DropWhitespaceOnlyHunks && seg.bodyIsWhitespaceOnly()) {
			droppableBytes += bodyBytes
		}
	}
	if totalBytes == 0 {
		return 0.0
	}
	score := float32(droppableBytes) / float32(totalBytes)
	if score < 0 {
		return 0
	}
	if score > 1 {
		return 1
	}
	return score
}

func (d *DiffNoise) Apply(content string, ctx *pipeline.CompressionContext, store pipeline.CcrStore) (*pipeline.OffloadOutput, error) {
	segments := parseSegments(content)
	if len(segments) == 0 {
		return nil, pipeline.Skipped(diffNoiseName, "no diff sections")
	}

	var output strings.Builder
	output.Grow(len(content))
	droppedAny := false

	for _, seg := range segments {
		// Always emit header lines.
		for _, h := range seg.headerLines {
			output.WriteString(h)
			output.WriteByte('\n')
		}
		dropLockfile := d.isLockfile(seg.newPath)
		dropWhitespace := d.config.DropWhitespaceOnlyHunks && seg.bodyIsWhitespaceOnly()
		if dropLockfile || dropWhitespace {
			reason := "lockfile"
			if !dropLockfile {
				reason = "whitespace-only"
			}
			output.WriteString(fmt.Sprintf("[diff_noise: %s hunks dropped (%d lines)]\n",
				reason, len(seg.bodyLines)))
			droppedAny = true
		} else {
			for _, b := range seg.bodyLines {
				output.WriteString(b)
				output.WriteByte('\n')
			}
		}
	}

	// Pre-diff content (before the first `diff --git`)
	preDiff := leadingPreDiffLines(content)
	result := output.String()
	if preDiff != "" {
		result = preDiff + result
	}

	if !droppedAny || len(result) >= len(content) {
		return nil, pipeline.Skipped(diffNoiseName, "no droppable hunks")
	}

	// CCR: hash original, stash, append marker.
	key := ccr.ComputeKey([]byte(content))
	store.Put(key, []byte(content))
	result += fmt.Sprintf("\n[diff_noise CCR: hash=%s]", key)

	out := pipeline.OffloadOutputFromLengths(len(content), result, key)
	return &out, nil
}

func (d *DiffNoise) Confidence() float32 { return diffNoiseConfidence }

func (d *DiffNoise) isLockfile(path string) bool {
	if path == "" {
		return false
	}
	for _, suffix := range d.config.LockfileSuffixes {
		if strings.HasSuffix(path, suffix) {
			prefixLen := len(path) - len(suffix)
			if prefixLen == 0 {
				return true
			}
			prev := path[prefixLen-1]
			if prev == '/' || prev == '\\' {
				return true
			}
		}
	}
	return false
}

// diffSegment represents one file's section of a diff.
type diffSegment struct {
	newPath     string
	headerLines []string
	bodyLines   []string
}

func (seg *diffSegment) bodyIsWhitespaceOnly() bool {
	var adds, subs []string
	sawChange := false
	for _, line := range seg.bodyLines {
		if len(line) == 0 {
			continue
		}
		if line[0] == '+' && !strings.HasPrefix(line, "+++") {
			sawChange = true
			adds = append(adds, stripWS(line[1:]))
		} else if line[0] == '-' && !strings.HasPrefix(line, "---") {
			sawChange = true
			subs = append(subs, stripWS(line[1:]))
		}
	}
	if !sawChange {
		return false
	}
	if len(adds) != len(subs) {
		return false
	}
	for i := range adds {
		if adds[i] != subs[i] {
			return false
		}
	}
	return true
}

func stripWS(s string) string {
	var b strings.Builder
	for _, c := range s {
		if c != ' ' && c != '\t' && c != '\r' && c != '\n' {
			b.WriteRune(c)
		}
	}
	return b.String()
}

func parseSegments(content string) []*diffSegment {
	var segments []*diffSegment
	var current *diffSegment
	inBody := false

	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "diff --git") {
			if current != nil {
				segments = append(segments, current)
			}
			current = &diffSegment{
				newPath:     parseNewPath(line),
				headerLines: []string{line},
			}
			inBody = false
			continue
		}
		if current == nil {
			continue // pre-diff prelude
		}
		if !inBody {
			if strings.HasPrefix(line, "@@") {
				inBody = true
				current.bodyLines = append(current.bodyLines, line)
				continue
			}
			current.headerLines = append(current.headerLines, line)
		} else {
			current.bodyLines = append(current.bodyLines, line)
		}
	}
	if current != nil {
		segments = append(segments, current)
	}
	return segments
}

func leadingPreDiffLines(content string) string {
	var b strings.Builder
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "diff --git") {
			break
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

func parseNewPath(header string) string {
	idx := strings.LastIndex(header, " b/")
	if idx < 0 {
		return ""
	}
	return header[idx+3:]
}

var _ pipeline.OffloadTransform = (*DiffNoise)(nil)
