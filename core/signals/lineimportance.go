// Package signals provides line-level importance detection for compressors.
//
// Compressors call these detectors when deciding which lines to drop
// under a token budget. Signals carry category, priority, and confidence
// so future tiers can short-circuit on high confidence.
package signals

// ImportanceContext describes where the line came from, determining
// which pattern set fires.
type ImportanceContext int

const (
	// ImportanceContextText is free-form prose -- markdown structure matters.
	ImportanceContextText ImportanceContext = iota
	// ImportanceContextSearch is grep/ripgrep output -- error/warn keywords win.
	ImportanceContextSearch
	// ImportanceContextDiff is git diff -- error + security + importance keywords.
	ImportanceContextDiff
	// ImportanceContextLog is log output -- error/warn keywords + level prefixes.
	ImportanceContextLog
)

// ImportanceCategory describes why a line earned its priority.
type ImportanceCategory int

const (
	ImportanceCategoryError      ImportanceCategory = iota
	ImportanceCategoryWarning                       // 1
	ImportanceCategoryImportance                    // 2
	ImportanceCategorySecurity                      // 3
	ImportanceCategoryMarkdown                      // 4
)

// ImportanceSignal is the output of a single detector for a single line.
// Priority is what compressors rank by; Confidence is what the Tiered
// combinator uses to decide whether to keep asking the next tier.
type ImportanceSignal struct {
	Category   ImportanceCategory
	HasMatch   bool
	Priority   float64
	Confidence float64
	Matched    string
}

// NeutralSignal returns a signal indicating no opinion on the line.
func NeutralSignal() ImportanceSignal {
	return ImportanceSignal{}
}

// MatchedSignal returns a fired detection with explicit category and priority.
func MatchedSignal(category ImportanceCategory, priority, confidence float64) ImportanceSignal {
	return ImportanceSignal{
		Category:   category,
		HasMatch:   true,
		Priority:   priority,
		Confidence: confidence,
	}
}

// LineImportanceDetector is a single-line importance classifier.
// Implementations are expected to be cheap (keyword automaton) or
// amortizable (embedding + classifier head with batched inference).
type LineImportanceDetector interface {
	DetectImportance(line string, context ImportanceContext) ImportanceSignal
	Name() string
}
