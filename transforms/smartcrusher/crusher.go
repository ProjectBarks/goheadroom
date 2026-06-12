package smartcrusher

// SmartCrusher is the main entry point for JSON array compression.
type SmartCrusher struct {
	Config      SmartCrusherConfig
	Analyzer    *SmartAnalyzer
	Constraints []Constraint
	Observers   []Observer
}

// Crush compresses JSON content. Returns a CrushResult.
// query is the user query for anchor extraction.
// relevanceWeight controls how much relevance scoring affects selection.
func (sc *SmartCrusher) Crush(content string, query string, relevanceWeight float64) CrushResult {
	if content == "" {
		return CrushResultPassthrough(content)
	}

	inputBytes := len(content)
	result := CrushResultPassthrough(content)

	// Fire observers.
	event := &CrushEvent{
		Strategy:    result.Strategy,
		InputBytes:  inputBytes,
		OutputBytes: len(result.Compressed),
		WasModified: result.WasModified,
	}
	for _, obs := range sc.Observers {
		obs.OnEvent(event)
	}

	return result
}
