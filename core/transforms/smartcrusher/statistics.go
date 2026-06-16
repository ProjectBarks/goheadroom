package smartcrusher

import (
	"encoding/json"
	"math"
	"sort"
	"strconv"
	"strings"
)

// IsUUIDFormat checks if a string looks like a UUID (format check only).
func IsUUIDFormat(value string) bool {
	if len(value) != 36 {
		return false
	}
	parts := strings.Split(value, "-")
	if len(parts) != 5 {
		return false
	}
	expectedLens := [5]int{8, 4, 4, 4, 12}
	for i, part := range parts {
		if len(part) != expectedLens[i] {
			return false
		}
		for _, c := range part {
			if !isHexDigit(c) {
				return false
			}
		}
	}
	return true
}

func isHexDigit(c rune) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

// CalculateStringEntropy returns Shannon entropy of a string, normalized to [0, 1].
func CalculateStringEntropy(s string) float64 {
	runes := []rune(s)
	n := len(runes)
	if n < 2 {
		return 0.0
	}

	freq := make(map[rune]int)
	for _, c := range runes {
		freq[c]++
	}

	length := float64(n)
	entropy := 0.0
	for _, count := range freq {
		p := float64(count) / length
		if p > 0 {
			entropy -= p * math.Log2(p)
		}
	}

	uniqueChars := len(freq)
	if uniqueChars < n {
		uniqueChars = len(freq)
	}
	minVal := len(freq)
	if n < minVal {
		minVal = n
	}
	maxEntropy := math.Log2(float64(minVal))
	if maxEntropy > 0 {
		return entropy / maxEntropy
	}
	return 0.0
}

// PythonIntParse parses a string the way Python's int() does for plain integer literals.
// Handles leading/trailing whitespace, leading sign, and PEP 515 underscore separators.
func PythonIntParse(s string) (int64, bool) {
	trimmed := strings.TrimSpace(s)
	if len(trimmed) == 0 {
		return 0, false
	}

	// Handle PEP 515 underscores.
	cleaned := trimmed
	if strings.Contains(trimmed, "_") {
		bytes := []byte(trimmed)
		// Reject leading/trailing underscore or double underscores.
		if bytes[0] == '_' || bytes[len(bytes)-1] == '_' || strings.Contains(trimmed, "__") {
			return 0, false
		}
		// Also check that sign char is not followed by underscore.
		if len(bytes) > 1 && (bytes[0] == '+' || bytes[0] == '-') && bytes[1] == '_' {
			return 0, false
		}
		cleaned = strings.ReplaceAll(trimmed, "_", "")
	}

	v, err := strconv.ParseInt(cleaned, 10, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// DetectSequentialPattern detects if values form a sequential numeric pattern.
// Uses json.RawMessage to handle mixed types (numbers, strings, bools).
func DetectSequentialPattern(values []json.RawMessage, checkOrder bool) bool {
	if len(values) < 5 {
		return false
	}

	var nums []float64
	hadNonStringNumeric := false

	for _, raw := range values {
		// Try to detect the type by looking at the first byte.
		trimmed := strings.TrimSpace(string(raw))
		if len(trimmed) == 0 {
			continue
		}

		switch trimmed[0] {
		case '"':
			// String value - try to parse as int.
			var s string
			if err := json.Unmarshal(raw, &s); err == nil {
				if parsed, ok := PythonIntParse(s); ok {
					nums = append(nums, float64(parsed))
					// BUG #2 fix: do NOT set hadNonStringNumeric.
				}
			}
		case 't', 'f':
			// Boolean - explicitly excluded per Python parity.
			continue
		case 'n':
			// null
			continue
		default:
			// Number
			var f float64
			if err := json.Unmarshal(raw, &f); err == nil {
				nums = append(nums, f)
				hadNonStringNumeric = true
			}
		}
	}

	if len(nums) < 5 {
		return false
	}

	// BUG #2 fix gate.
	if !hadNonStringNumeric {
		return false
	}

	if len(nums) < 2 {
		return false
	}

	// Sort and compute pairwise diffs.
	sortedNums := make([]float64, len(nums))
	copy(sortedNums, nums)
	sort.Float64s(sortedNums)

	diffs := make([]float64, 0, len(sortedNums)-1)
	for i := 1; i < len(sortedNums); i++ {
		diffs = append(diffs, sortedNums[i]-sortedNums[i-1])
	}
	if len(diffs) == 0 {
		return false
	}

	avgDiff := 0.0
	for _, d := range diffs {
		avgDiff += d
	}
	avgDiff /= float64(len(diffs))

	if avgDiff < 0.5 || avgDiff > 2.0 {
		return false
	}

	// Most diffs in [0.5, 2.0] => sequential candidate.
	consistentCount := 0
	for _, d := range diffs {
		if d >= 0.5 && d <= 2.0 {
			consistentCount++
		}
	}
	isSequential := float64(consistentCount)/float64(len(diffs)) > 0.8
	if !isSequential {
		return false
	}

	if checkOrder {
		ascendingCount := 0
		for i := 1; i < len(nums); i++ {
			if nums[i-1] <= nums[i] {
				ascendingCount++
			}
		}
		nPairs := len(nums) - 1
		return float64(ascendingCount)/float64(nPairs) > 0.7
	}

	return true
}
