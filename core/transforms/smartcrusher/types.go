// Package smartcrusher implements JSON schema analysis, array compaction,
// outlier detection, and statistics-based compression for context windows.
package smartcrusher

import "encoding/json"

// CompressionStrategy represents strategies based on data patterns.
type CompressionStrategy int

const (
	StrategyNone          CompressionStrategy = iota
	StrategySkip                              // Not safe to crush.
	StrategyTimeSeries                        // Keep change points, summarize stable runs.
	StrategyClusterSample                     // Dedupe similar items.
	StrategyTopN                              // Keep highest-scored items.
	StrategySmartSample                       // Statistical sampling with anchor-preservation.
)

// String returns the lowercase strategy name matching Python parity.
func (s CompressionStrategy) String() string {
	switch s {
	case StrategyNone:
		return "none"
	case StrategySkip:
		return "skip"
	case StrategyTimeSeries:
		return "time_series"
	case StrategyClusterSample:
		return "cluster"
	case StrategyTopN:
		return "top_n"
	case StrategySmartSample:
		return "smart_sample"
	default:
		return "unknown"
	}
}

// FieldStats holds statistics for a single field across array items.
type FieldStats struct {
	Name          string
	FieldType     string // "numeric", "string", "boolean", "object", "array", "null"
	Count         int
	UniqueCount   int
	UniqueRatio   float64
	IsConstant    bool
	ConstantValue interface{}

	// Numeric-specific
	MinVal       *float64
	MaxVal       *float64
	MeanVal      *float64
	Variance     *float64
	ChangePoints []int

	// String-specific
	AvgLength *float64
	TopValues []TopValue // (value, count) pairs by frequency descending
}

// TopValue is a (value, count) pair for field statistics.
type TopValue struct {
	Value string
	Count int
}

// CrushabilityAnalysis determines whether an array is safe to crush.
type CrushabilityAnalysis struct {
	Crushable      bool
	Confidence     float64
	Reason         string
	SignalsPresent []string
	SignalsAbsent  []string

	HasIDField          bool
	IDUniqueness        float64
	AvgStringUniqueness float64
	HasScoreField       bool
	ErrorItemCount      int
	AnomalyCount        int
}

// CrushabilitySkip builds a "not crushable" verdict.
func CrushabilitySkip(reason string, confidence float64) CrushabilityAnalysis {
	return CrushabilityAnalysis{
		Crushable:  false,
		Confidence: confidence,
		Reason:     reason,
	}
}

// ArrayAnalysis is a complete analysis of an array.
type ArrayAnalysis struct {
	ItemCount           int
	FieldStats          map[string]*FieldStats
	DetectedPattern     string // "time_series", "logs", "search_results", "generic"
	RecommendedStrategy CompressionStrategy
	ConstantFields      map[string]interface{}
	EstimatedReduction  float64
	Crushability        *CrushabilityAnalysis
}

// CompressionPlan describes how to compress an array.
type CompressionPlan struct {
	Strategy       CompressionStrategy
	KeepIndices    []int
	ConstantFields map[string]interface{}
	SummaryRanges  []SummaryRange
	ClusterField   string
	SortField      string
	KeepCount      int
}

// SummaryRange is a (start, end, summary) triple.
type SummaryRange struct {
	Start   int
	End     int
	Summary json.RawMessage
}

// NewCompressionPlan returns a CompressionPlan with default values matching Python.
func NewCompressionPlan() CompressionPlan {
	return CompressionPlan{
		Strategy:       StrategyNone,
		KeepIndices:    nil,
		ConstantFields: nil,
		SummaryRanges:  nil,
		KeepCount:      10,
	}
}

// CrushResult is the result from SmartCrusher.Crush().
type CrushResult struct {
	Compressed  string
	Original    string
	WasModified bool
	Strategy    string
}

// CrushResultPassthrough creates a pass-through result with no modification.
func CrushResultPassthrough(content string) CrushResult {
	return CrushResult{
		Compressed:  content,
		Original:    content,
		WasModified: false,
		Strategy:    "passthrough",
	}
}
