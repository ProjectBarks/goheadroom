package signals

// EscalateThreshold is the confidence at which Tiered accepts a tier's
// signal without consulting later tiers. KeywordDetector emits 0.7, so
// it wins by default; an ML tier with calibrated confidence >= 0.8
// would short-circuit the keyword tier.
const EscalateThreshold = 0.7

// Tiered is a composition combinator for layered detectors. It chains
// an ordered list of detectors. The first tier whose signal exceeds
// EscalateThreshold confidence wins; lower-confidence tiers are skipped
// past. If no tier exceeds the threshold, the highest-confidence signal
// seen is returned.
type Tiered struct {
	tiers []LineImportanceDetector
}

// NewTiered creates a new empty Tiered combinator.
func NewTiered() *Tiered {
	return &Tiered{}
}

// With adds a detector tier. Order matters: most precise first.
func (t *Tiered) With(detector LineImportanceDetector) *Tiered {
	t.tiers = append(t.tiers, detector)
	return t
}

// Evaluate scores a line through all tiers and returns the winning signal.
// Empty returns NeutralSignal. Single item returns that item's signal.
// If max confidence >= EscalateThreshold, that signal is returned immediately.
// Otherwise, the highest-confidence signal seen is returned.
func (t *Tiered) Evaluate(line string, ctx ImportanceContext) ImportanceSignal {
	if len(t.tiers) == 0 {
		return NeutralSignal()
	}

	best := NeutralSignal()
	for _, tier := range t.tiers {
		signal := tier.DetectImportance(line, ctx)
		if signal.Confidence >= EscalateThreshold {
			return signal
		}
		if signal.Confidence > best.Confidence {
			best = signal
		}
	}
	return best
}

// DetectImportance implements LineImportanceDetector for composability.
func (t *Tiered) DetectImportance(line string, ctx ImportanceContext) ImportanceSignal {
	return t.Evaluate(line, ctx)
}

// Name implements LineImportanceDetector.
func (t *Tiered) Name() string {
	return "tiered"
}
