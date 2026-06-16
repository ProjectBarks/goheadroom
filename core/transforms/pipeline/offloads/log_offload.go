package offloads

import (
	"strings"

	"github.com/projectbarks/goheadroom/core/ccr"
	"github.com/projectbarks/goheadroom/core/signals"
	"github.com/projectbarks/goheadroom/core/transforms/contentdetector"
	"github.com/projectbarks/goheadroom/core/transforms/logcompressor"
	"github.com/projectbarks/goheadroom/core/transforms/pipeline"
)

const logOffloadName = "log_offload"
const logOffloadConfidence float32 = 0.85

// LogOffload wraps LogCompressor as an OffloadTransform.
// Bloat heuristic: combines repetition and priority dilution signals.
type LogOffload struct {
	compressor *logcompressor.LogCompressor
	bloat      pipeline.LogBloatConfig
	detector   signals.LineImportanceDetector
}

// NewLogOffload creates a LogOffload with the given bloat config.
func NewLogOffload(bloat pipeline.LogBloatConfig) *LogOffload {
	return &LogOffload{
		compressor: logcompressor.New(logcompressor.DefaultConfig()),
		bloat:      bloat,
		detector:   signals.NewKeywordDetector(),
	}
}

func (l *LogOffload) Name() string { return logOffloadName }

func (l *LogOffload) AppliesTo() []contentdetector.ContentType {
	return []contentdetector.ContentType{contentdetector.BuildOutput}
}

func (l *LogOffload) EstimateBloat(content string) float32 {
	if content == "" {
		return 0.0
	}
	unique := make(map[string]struct{}, l.bloat.SampleSize)
	sampled := 0
	lowPriority := 0

	for _, line := range strings.SplitN(content, "\n", l.bloat.SampleSize+1) {
		if sampled >= l.bloat.SampleSize {
			break
		}
		if line == "" {
			continue
		}
		sampled++
		unique[line] = struct{}{}
		signal := l.detector.DetectImportance(line, signals.ImportanceContextLog)
		if signal.Priority <= float64(l.bloat.HighPriorityThreshold) {
			lowPriority++
		}
	}

	totalLines := strings.Count(content, "\n") + 1
	if totalLines < l.bloat.MinLines {
		return 0.0
	}
	if sampled == 0 {
		return 0.0
	}

	repetition := 1.0 - float32(len(unique))/float32(sampled)
	dilution := float32(lowPriority) / float32(sampled)
	score := repetition*l.bloat.UniquenessWeight + dilution*l.bloat.PriorityDilutionWeight
	if score < 0 {
		return 0
	}
	if score > 1 {
		return 1
	}
	return score
}

func (l *LogOffload) Apply(content string, ctx *pipeline.CompressionContext, store pipeline.CcrStore) (*pipeline.OffloadOutput, error) {
	result, stats := l.compressor.CompressWithStore(content, 0.0, store.(ccr.CcrStore))

	if result.CacheKey == nil {
		reason := "no cache_key emitted"
		if stats.CCRSkipReason != nil {
			reason = *stats.CCRSkipReason
		}
		return nil, pipeline.Skipped(logOffloadName, reason)
	}

	out := pipeline.OffloadOutputFromLengths(len(content), result.Compressed, *result.CacheKey)
	return &out, nil
}

func (l *LogOffload) Confidence() float32 { return logOffloadConfidence }

var _ pipeline.OffloadTransform = (*LogOffload)(nil)
