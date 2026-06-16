package smartcrusher

// SmartCrusherConfig holds configuration for SmartCrusher.
type SmartCrusherConfig struct {
	Enabled                 bool
	MinItemsToAnalyze       int
	MinTokensToCrush        int
	VarianceThreshold       float64
	UniquenessThreshold     float64
	SimilarityThreshold     float64
	MaxItemsAfterCrush      int
	PreserveChangePoints    bool
	FactorOutConstants      bool
	IncludeSummaries        bool
	UseFeedbackHints        bool
	TOINConfidenceThreshold float64
	DedupIdenticalItems     bool
	FirstFraction           float64
	LastFraction            float64
	RelevanceThreshold      float64
	LosslessMinSavingsRatio float64
	EnableCCRMarker         bool
}

// DefaultSmartCrusherConfig returns configuration with defaults matching Python parity.
func DefaultSmartCrusherConfig() SmartCrusherConfig {
	return SmartCrusherConfig{
		Enabled:                 true,
		MinItemsToAnalyze:       5,
		MinTokensToCrush:        200,
		VarianceThreshold:       2.0,
		UniquenessThreshold:     0.1,
		SimilarityThreshold:     0.8,
		MaxItemsAfterCrush:      15,
		PreserveChangePoints:    true,
		FactorOutConstants:      false,
		IncludeSummaries:        false,
		UseFeedbackHints:        true,
		TOINConfidenceThreshold: 0.5,
		DedupIdenticalItems:     true,
		FirstFraction:           0.3,
		LastFraction:            0.15,
		RelevanceThreshold:      0.3,
		LosslessMinSavingsRatio: 0.30,
		EnableCCRMarker:         true,
	}
}
