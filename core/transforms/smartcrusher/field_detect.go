package smartcrusher

import (
	"encoding/json"
	"math"
)

// DetectIDFieldStatistically detects whether a field is an ID field.
// Returns (isID, confidence).
func DetectIDFieldStatistically(stats *FieldStats, values []json.RawMessage) (bool, float64) {
	if stats.UniqueRatio < 0.9 {
		return false, 0.0
	}

	if stats.FieldType == "string" {
		// Sample first 20 string values.
		var sampleValues []string
		count := 0
		for _, raw := range values {
			if count >= 20 {
				break
			}
			var s string
			if err := json.Unmarshal(raw, &s); err == nil {
				sampleValues = append(sampleValues, s)
			}
			count++
		}

		if len(sampleValues) > 0 {
			uuidCount := 0
			for _, s := range sampleValues {
				if IsUUIDFormat(s) {
					uuidCount++
				}
			}
			if float64(uuidCount)/float64(len(sampleValues)) > 0.8 {
				return true, 0.95
			}

			totalEntropy := 0.0
			for _, s := range sampleValues {
				totalEntropy += CalculateStringEntropy(s)
			}
			avgEntropy := totalEntropy / float64(len(sampleValues))
			if avgEntropy > 0.7 && stats.UniqueRatio > 0.95 {
				return true, 0.8
			}
		}
	}

	if stats.FieldType == "numeric" {
		if DetectSequentialPattern(values, true) && stats.UniqueRatio > 0.95 {
			return true, 0.9
		}

		if stats.MinVal != nil && stats.MaxVal != nil {
			valueRange := *stats.MaxVal - *stats.MinVal
			if valueRange > 0.0 && stats.UniqueRatio > 0.95 {
				return true, 0.85
			}
		}
	}

	if stats.UniqueRatio > 0.98 {
		return true, 0.7
	}

	return false, 0.0
}

// DetectScoreFieldStatistically detects whether a field is a score field.
// items should be the dict-array items (objects containing the field).
// Returns (isScore, confidence).
func DetectScoreFieldStatistically(stats *FieldStats, items []json.RawMessage) (bool, float64) {
	if stats.FieldType != "numeric" {
		return false, 0.0
	}

	if stats.MinVal == nil || stats.MaxVal == nil {
		return false, 0.0
	}

	minVal := *stats.MinVal
	maxVal := *stats.MaxVal

	confidence := 0.0

	// Range check.
	isBounded := false
	if minVal >= 0 && minVal <= 1 && maxVal >= 0 && maxVal <= 1 {
		confidence += 0.4
		isBounded = true
	} else if minVal >= 0 && minVal <= 10 && maxVal >= 0 && maxVal <= 10 {
		confidence += 0.3
		isBounded = true
	} else if minVal >= 0 && minVal <= 100 && maxVal >= 0 && maxVal <= 100 {
		confidence += 0.25
		isBounded = true
	} else if minVal >= -1 && maxVal <= 1 {
		confidence += 0.35
		isBounded = true
	}

	if !isBounded {
		return false, 0.0
	}

	// Pull field values from first 50 items.
	var sampleValues []json.RawMessage
	limit := 50
	if len(items) < limit {
		limit = len(items)
	}
	for _, raw := range items[:limit] {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(raw, &obj); err == nil {
			if v, ok := obj[stats.Name]; ok {
				sampleValues = append(sampleValues, v)
			}
		}
	}

	// Sequential check.
	if DetectSequentialPattern(sampleValues, true) {
		return false, 0.0
	}

	// Extract all numeric values in order for descending-sort check.
	var valuesInOrder []float64
	for _, raw := range items {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(raw, &obj); err == nil {
			if v, ok := obj[stats.Name]; ok {
				var f float64
				if err := json.Unmarshal(v, &f); err == nil && !math.IsInf(f, 0) && !math.IsNaN(f) {
					valuesInOrder = append(valuesInOrder, f)
				}
			}
		}
	}

	if len(valuesInOrder) >= 5 {
		numPairs := len(valuesInOrder) - 1
		descendingCount := 0
		for i := 1; i < len(valuesInOrder); i++ {
			if valuesInOrder[i-1] >= valuesInOrder[i] {
				descendingCount++
			}
		}
		if numPairs > 0 && float64(descendingCount)/float64(numPairs) > 0.7 {
			confidence += 0.3
		}
	}

	// Float-fraction check on first 20.
	first20 := valuesInOrder
	if len(first20) > 20 {
		first20 = first20[:20]
	}
	floatCount := 0
	for _, v := range first20 {
		if !math.IsInf(v, 0) && !math.IsNaN(v) && v != math.Trunc(v) {
			floatCount++
		}
	}
	if len(first20) > 0 && float64(floatCount) > float64(len(first20))*0.3 {
		confidence += 0.1
	}

	isScore := confidence >= 0.4
	boundedConf := math.Min(confidence, 0.95)
	return isScore, boundedConf
}

// mathIsFinite checks if a float64 is finite (not NaN or Inf).
func mathIsFinite(f float64) bool {
	return !math.IsInf(f, 0) && !math.IsNaN(f)
}
