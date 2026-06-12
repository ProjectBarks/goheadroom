package smartcrusher

import (
	"encoding/json"
	"sort"
	"strings"
)

// DetectStructuralOutliers detects items that are structural outliers.
// Returns deduplicated, ascending-sorted indices.
func DetectStructuralOutliers(items []json.RawMessage) []int {
	if len(items) < 5 {
		return nil
	}

	// Count field occurrences across all items.
	fieldCounts := make(map[string]int)
	for _, raw := range items {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(raw, &obj); err == nil {
			for k := range obj {
				fieldCounts[k]++
			}
		}
	}

	n := len(items)
	commonFields := make(map[string]bool)
	rareFields := make(map[string]bool)
	for k, c := range fieldCounts {
		if float64(c) >= float64(n)*0.8 {
			commonFields[k] = true
		}
		if float64(c) < float64(n)*0.2 {
			rareFields[k] = true
		}
	}

	outlierSet := make(map[int]bool)

	// 1. Rare-field outliers.
	for i, raw := range items {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(raw, &obj); err == nil {
			for k := range obj {
				if rareFields[k] {
					outlierSet[i] = true
					break
				}
			}
		}
	}

	// 2. Rare-status outliers.
	for _, idx := range DetectRareStatusValues(items, commonFields) {
		outlierSet[idx] = true
	}

	result := make([]int, 0, len(outlierSet))
	for idx := range outlierSet {
		result = append(result, idx)
	}
	sort.Ints(result)
	return result
}

// DetectRareStatusValues detects items with rare values in status-like categorical fields.
func DetectRareStatusValues(items []json.RawMessage, commonFields map[string]bool) []int {
	var outlierIndices []int

	// Sort fields for determinism.
	sortedFields := make([]string, 0, len(commonFields))
	for f := range commonFields {
		sortedFields = append(sortedFields, f)
	}
	sort.Strings(sortedFields)

	for _, fieldName := range sortedFields {
		// Collect field values from dict items.
		type valueEntry struct {
			val       interface{} // decoded JSON value
			isNull    bool
			stringKey string // for frequency counting
			itemIndex int
		}

		var values []valueEntry
		for i, raw := range items {
			var obj map[string]json.RawMessage
			if err := json.Unmarshal(raw, &obj); err != nil {
				continue
			}
			rawVal, exists := obj[fieldName]
			if !exists {
				continue
			}
			trimmed := strings.TrimSpace(string(rawVal))
			isNull := trimmed == "null"
			stringKey := ""
			if isNull {
				stringKey = "__none__"
			} else {
				// Stringify the value for frequency counting.
				var decoded interface{}
				if err := json.Unmarshal(rawVal, &decoded); err == nil {
					stringKey = stringifyValue(decoded)
				}
			}
			values = append(values, valueEntry{isNull: isNull, stringKey: stringKey, itemIndex: i})
		}

		// Unique non-null values for cardinality.
		uniqueNonNull := make(map[string]bool)
		for _, v := range values {
			if !v.isNull {
				uniqueNonNull[v.stringKey] = true
			}
		}

		card := len(uniqueNonNull)
		if card < 2 || card > 50 {
			continue
		}

		// Frequency count.
		valueCounts := make(map[string]int)
		for _, v := range values {
			valueCounts[v.stringKey]++
		}
		if len(valueCounts) == 0 {
			continue
		}

		total := len(values)

		// Pareto check: find smallest K such that top-K values cover >= 80%.
		type countEntry struct {
			key   string
			count int
		}
		sorted := make([]countEntry, 0, len(valueCounts))
		for k, c := range valueCounts {
			sorted = append(sorted, countEntry{k, c})
		}
		sort.Slice(sorted, func(i, j int) bool {
			if sorted[i].count != sorted[j].count {
				return sorted[i].count > sorted[j].count
			}
			return sorted[i].key < sorted[j].key
		})

		threshold := int(float64(total)*0.8 + 0.999999) // ceil
		cumulative := 0
		topKValues := make(map[string]bool)
		for _, entry := range sorted {
			cumulative += entry.count
			topKValues[entry.key] = true
			if cumulative >= threshold {
				break
			}
		}

		if len(topKValues) > 5 {
			continue
		}

		// Items with values NOT in topK are outliers.
		for _, v := range values {
			if !topKValues[v.stringKey] {
				outlierIndices = append(outlierIndices, v.itemIndex)
			}
		}
	}

	return outlierIndices
}

// DetectErrorItemsForPreservation detects items containing error keywords.
func DetectErrorItemsForPreservation(items []json.RawMessage, itemStrings []string) []int {
	var errorIndices []int

	for i, raw := range items {
		// Only process dict items.
		trimmed := strings.TrimSpace(string(raw))
		if len(trimmed) == 0 || trimmed[0] != '{' {
			continue
		}

		var serialized string
		if itemStrings != nil && i < len(itemStrings) {
			serialized = strings.ToLower(itemStrings[i])
		} else {
			serialized = strings.ToLower(string(raw))
		}

		for _, kw := range ErrorKeywords {
			if strings.Contains(serialized, kw) {
				errorIndices = append(errorIndices, i)
				break
			}
		}
	}

	return errorIndices
}

func stringifyValue(v interface{}) string {
	switch val := v.(type) {
	case string:
		return val
	case bool:
		if val {
			return "true"
		}
		return "false"
	case float64:
		// Format as integer if no fractional part.
		if val == float64(int64(val)) {
			return strings.TrimRight(strings.TrimRight(
				strings.Replace(json_FormatFloat(val), ".0", "", 1),
				"0"), ".")
		}
		return json_FormatFloat(val)
	case nil:
		return "__none__"
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}

func json_FormatFloat(f float64) string {
	b, _ := json.Marshal(f)
	return string(b)
}
