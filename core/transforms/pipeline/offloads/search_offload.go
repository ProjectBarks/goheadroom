package offloads

import (
	"strings"

	"github.com/projectbarks/goheadroom/core/ccr"
	"github.com/projectbarks/goheadroom/core/transforms/contentdetector"
	"github.com/projectbarks/goheadroom/core/transforms/pipeline"
	"github.com/projectbarks/goheadroom/core/transforms/searchcompressor"
)

const searchOffloadName = "search_offload"
const searchOffloadConfidence float32 = 0.85

// SearchOffload wraps SearchCompressor as an OffloadTransform.
// Bloat heuristic: match clustering (avg matches per file).
type SearchOffload struct {
	compressor *searchcompressor.SearchCompressor
	bloat      pipeline.SearchBloatConfig
}

// NewSearchOffload creates a SearchOffload with the given bloat config.
func NewSearchOffload(bloat pipeline.SearchBloatConfig) *SearchOffload {
	return &SearchOffload{
		compressor: searchcompressor.New(searchcompressor.DefaultConfig()),
		bloat:      bloat,
	}
}

func (s *SearchOffload) Name() string { return searchOffloadName }

func (s *SearchOffload) AppliesTo() []contentdetector.ContentType {
	return []contentdetector.ContentType{contentdetector.SearchResults}
}

func (s *SearchOffload) EstimateBloat(content string) float32 {
	if content == "" {
		return 0.0
	}
	var total int
	files := make(map[string]struct{})

	for _, line := range strings.Split(content, "\n") {
		if file := extractFilePrefix(line); file != "" {
			total++
			files[file] = struct{}{}
		}
	}

	if total < s.bloat.MinMatches || len(files) == 0 {
		return 0.0
	}
	avg := float32(total) / float32(len(files))
	if avg <= 1.0 {
		return 0.0
	}
	score := (avg - 1.0) / s.bloat.ClusterThreshold
	if score > 1.0 {
		score = 1.0
	}
	return score
}

func (s *SearchOffload) Apply(content string, ctx *pipeline.CompressionContext, store pipeline.CcrStore) (*pipeline.OffloadOutput, error) {
	result, stats := s.compressor.CompressWithStore(content, ctx.Query, 0.0, store.(ccr.CcrStore))

	if result.CacheKey == nil {
		reason := "no cache_key emitted"
		if stats.CCRSkipReason != nil {
			reason = *stats.CCRSkipReason
		}
		return nil, pipeline.Skipped(searchOffloadName, reason)
	}

	out := pipeline.OffloadOutputFromLengths(len(content), result.Compressed, *result.CacheKey)
	return &out, nil
}

func (s *SearchOffload) Confidence() float32 { return searchOffloadConfidence }

// extractFilePrefix parses a grep-style match line to get the file path.
// Handles: path:42:content, path-42-content, C:\path:42:content
func extractFilePrefix(line string) string {
	bytes := []byte(line)
	if len(bytes) == 0 {
		return ""
	}
	// Skip Windows drive prefix.
	scanStart := 0
	if len(bytes) >= 2 && bytes[1] == ':' && isASCIIAlpha(bytes[0]) {
		scanStart = 2
	}

	for i := scanStart; i < len(bytes); i++ {
		b := bytes[i]
		if b == ':' || b == '-' {
			sep := b
			j := i + 1
			digitStart := j
			for j < len(bytes) && bytes[j] >= '0' && bytes[j] <= '9' {
				j++
			}
			if j > digitStart && j < len(bytes) && bytes[j] == sep {
				return line[:i]
			}
		}
	}
	return ""
}

func isASCIIAlpha(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

var _ pipeline.OffloadTransform = (*SearchOffload)(nil)
