package smartcrusher

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/uber/goheadroom/transforms/adaptivesizer"
)

// ComputeKSplit computes K split (total / first / last / importance) for adaptive
// crushers. Port of Rust compute_k_split.
//
// Returns (kTotal, kFirst, kLast, kImportance).
func ComputeKSplit(items []string, config *SmartCrusherConfig, bias float64) (int, int, int, int) {
	var maxK *int
	if config.MaxItemsAfterCrush > 0 {
		v := config.MaxItemsAfterCrush
		maxK = &v
	}
	kTotal := adaptivesizer.ComputeOptimalK(items, bias, 3, maxK)

	kFirstRaw := max(1, int(roundTiesEven(float64(kTotal)*config.FirstFraction)))
	kLastRaw := max(1, int(roundTiesEven(float64(kTotal)*config.LastFraction)))

	// BUG #4 FIX: clamp so k_first + k_last <= k_total.
	kFirst := min(kFirstRaw, kTotal)
	kLast := min(kLastRaw, kTotal-kFirst)
	if kTotal < kFirst {
		kLast = 0
	}
	kImportance := kTotal - kFirst - kLast
	if kImportance < 0 {
		kImportance = 0
	}
	return kTotal, kFirst, kLast, kImportance
}

// CrushStringArray crushes an array of strings.
// Port of Rust crush_string_array.
// Returns (crushedItems, strategyString).
func CrushStringArray(items []string, config *SmartCrusherConfig, bias float64) ([]string, string) {
	n := len(items)
	if n <= 8 {
		result := make([]string, n)
		copy(result, items)
		return result, "string:passthrough"
	}

	kTotal, kFirst, kLast, _ := ComputeKSplit(items, config, bias)

	// 1. Error-keyword indices.
	errorIndices := make(map[int]bool)
	for i, s := range items {
		lower := strings.ToLower(s)
		for _, kw := range ErrorKeywords {
			if strings.Contains(lower, kw) {
				errorIndices[i] = true
				break
			}
		}
	}

	// 2. Length anomaly indices.
	lengths := make([]float64, n)
	for i, s := range items {
		lengths[i] = float64(len([]rune(s)))
	}
	anomalyIndices := make(map[int]bool)
	if len(lengths) > 1 {
		meanLen, meanOK := Mean(lengths)
		stdLen, stdOK := SampleStdev(lengths)
		if meanOK && stdOK && stdLen > 0 {
			threshold := config.VarianceThreshold * stdLen
			for i, l := range lengths {
				if math.Abs(l-meanLen) > threshold {
					anomalyIndices[i] = true
				}
			}
		}
	}

	// 3. Boundary indices.
	keepIndices := make(map[int]bool)
	limit := min(kFirst, n)
	for i := 0; i < limit; i++ {
		keepIndices[i] = true
	}
	lastStart := n - kLast
	if lastStart < 0 {
		lastStart = 0
	}
	for i := lastStart; i < n; i++ {
		keepIndices[i] = true
	}

	// 4. Combine.
	for idx := range errorIndices {
		keepIndices[idx] = true
	}
	for idx := range anomalyIndices {
		keepIndices[idx] = true
	}

	// Pre-populate seen strings.
	seen := make(map[string]bool)
	for idx := range keepIndices {
		seen[items[idx]] = true
	}

	// 5. Stride-fill remaining budget.
	dedupCount := 0
	remainingBudget := kTotal - len(keepIndices)
	if remainingBudget < 0 {
		remainingBudget = 0
	}
	if remainingBudget > 0 {
		stride := (n - 1) / (remainingBudget + 1)
		if stride < 1 {
			stride = 1
		}
		cap := kTotal + len(errorIndices) + len(anomalyIndices)
		for i := 0; i < n; i += stride {
			if len(keepIndices) >= cap {
				break
			}
			if !keepIndices[i] {
				if !seen[items[i]] {
					keepIndices[i] = true
					seen[items[i]] = true
				} else {
					dedupCount++
				}
			}
		}
	}

	// 6. Build output preserving original order.
	sorted := setToSortedSlice(keepIndices)
	result := make([]string, len(sorted))
	for i, idx := range sorted {
		result[i] = items[idx]
	}

	strategy := fmt.Sprintf("string:adaptive(%d->%d", n, len(result))
	if dedupCount > 0 {
		strategy += fmt.Sprintf(",dedup=%d", dedupCount)
	}
	if len(errorIndices) > 0 {
		strategy += fmt.Sprintf(",errors=%d", len(errorIndices))
	}
	strategy += ")"

	return result, strategy
}

// CrushNumberArray crushes an array of numbers.
// Port of Rust crush_number_array. Carries BUG #1 fix.
func CrushNumberArray(items []json.RawMessage, config *SmartCrusherConfig, bias float64) ([]json.RawMessage, string) {
	n := len(items)
	if n <= 8 {
		result := make([]json.RawMessage, n)
		copy(result, items)
		return result, "number:passthrough"
	}

	// Filter to finite f64.
	var finite []float64
	for _, raw := range items {
		trimmed := strings.TrimSpace(string(raw))
		if trimmed == "null" || trimmed == "" {
			continue
		}
		var f float64
		if err := json.Unmarshal(raw, &f); err == nil && isFinite(f) {
			finite = append(finite, f)
		}
	}
	if len(finite) == 0 {
		result := make([]json.RawMessage, n)
		copy(result, items)
		return result, "number:no_finite"
	}

	// K split.
	itemStrings := make([]string, n)
	for i, raw := range items {
		itemStrings[i] = string(raw)
	}
	kTotal, kFirst, kLast, _ := ComputeKSplit(itemStrings, config, bias)

	// Statistics.
	meanVal, _ := Mean(finite)
	medianVal, _ := Median(finite)
	stdVal := 0.0
	if len(finite) > 1 {
		if s, ok := SampleStdev(finite); ok {
			stdVal = s
		}
	}

	// Sorted for percentiles.
	sortedFinite := make([]float64, len(finite))
	copy(sortedFinite, finite)
	sort.Float64s(sortedFinite)

	// BUG #1 FIX: proper linear interpolation.
	p25 := percentileLinear(sortedFinite, 0.25)
	p75 := percentileLinear(sortedFinite, 0.75)

	// Outliers.
	outlierIndices := make(map[int]bool)
	if stdVal > 0 {
		threshold := config.VarianceThreshold * stdVal
		for i, raw := range items {
			trimmed := strings.TrimSpace(string(raw))
			if trimmed == "null" || trimmed == "" {
				continue
			}
			var f float64
			if err := json.Unmarshal(raw, &f); err == nil && isFinite(f) {
				if math.Abs(f-meanVal) > threshold {
					outlierIndices[i] = true
				}
			}
		}
	}

	// Change points.
	changeIndices := make(map[int]bool)
	if config.PreserveChangePoints && n > 10 {
		window := 5
		for i := window; i < n-window; i++ {
			var left, right []float64
			for j := i - window; j < i; j++ {
				trimmed := strings.TrimSpace(string(items[j]))
				if trimmed == "null" || trimmed == "" {
					continue
				}
				var f float64
				if err := json.Unmarshal(items[j], &f); err == nil && isFinite(f) {
					left = append(left, f)
				}
			}
			for j := i; j < i+window; j++ {
				trimmed := strings.TrimSpace(string(items[j]))
				if trimmed == "null" || trimmed == "" {
					continue
				}
				var f float64
				if err := json.Unmarshal(items[j], &f); err == nil && isFinite(f) {
					right = append(right, f)
				}
			}
			if len(left) > 0 && len(right) > 0 {
				leftMean := meanSlice(left)
				rightMean := meanSlice(right)
				if stdVal > 0 && math.Abs(rightMean-leftMean) > config.VarianceThreshold*stdVal {
					changeIndices[i] = true
				}
			}
		}
	}

	// Boundary.
	keepIndices := make(map[int]bool)
	limit := min(kFirst, n)
	for i := 0; i < limit; i++ {
		keepIndices[i] = true
	}
	lastStart := n - kLast
	if lastStart < 0 {
		lastStart = 0
	}
	for i := lastStart; i < n; i++ {
		keepIndices[i] = true
	}

	// Combine.
	for idx := range outlierIndices {
		keepIndices[idx] = true
	}
	for idx := range changeIndices {
		keepIndices[idx] = true
	}

	// Stride-fill.
	remainingBudget := kTotal - len(keepIndices)
	if remainingBudget < 0 {
		remainingBudget = 0
	}
	if remainingBudget > 0 {
		stride := (n - 1) / (remainingBudget + 1)
		if stride < 1 {
			stride = 1
		}
		cap := kTotal + len(outlierIndices)
		for i := 0; i < n; i += stride {
			if len(keepIndices) >= cap {
				break
			}
			if !keepIndices[i] {
				keepIndices[i] = true
			}
		}
	}

	// Build output.
	sorted := setToSortedSlice(keepIndices)
	result := make([]json.RawMessage, len(sorted))
	for i, idx := range sorted {
		result[i] = items[idx]
	}

	mn := finiteMin(finite)
	mx := finiteMax(finite)
	strategy := fmt.Sprintf("number:adaptive(%d->%d,min=%s,max=%s,mean=%s,median=%s,stddev=%s,p25=%s,p75=%s",
		n, len(result),
		formatNumberRepr(mn), formatNumberRepr(mx),
		FormatG(meanVal), FormatG(medianVal),
		FormatG(stdVal), FormatG(p25), FormatG(p75))
	if len(outlierIndices) > 0 {
		strategy += fmt.Sprintf(",outliers=%d", len(outlierIndices))
	}
	if len(changeIndices) > 0 {
		strategy += fmt.Sprintf(",change_points=%d", len(changeIndices))
	}
	strategy += ")"

	return result, strategy
}

// CrushObject crushes a JSON object by selecting the most informative keys.
// Port of Rust crush_object.
func CrushObject(obj map[string]json.RawMessage, config *SmartCrusherConfig, bias float64) (map[string]json.RawMessage, string) {
	n := len(obj)
	if n <= 8 {
		return copyMap(obj), "object:passthrough"
	}

	// Estimate tokens per key-value pair.
	keys := sortedMapKeys(obj)
	totalTokens := 0
	kvTokens := make([]int, n)
	for i, key := range keys {
		valStr := string(obj[key])
		tokens := len(valStr)/4 + len(key)/4 + 2
		kvTokens[i] = tokens
		totalTokens += tokens
	}

	if totalTokens < config.MinTokensToCrush {
		return copyMap(obj), "object:passthrough"
	}

	// Compute adaptive K.
	kvStrings := make([]string, n)
	for i, key := range keys {
		kvStrings[i] = fmt.Sprintf("%s: %s", key, string(obj[key]))
	}

	var maxK *int
	if config.MaxItemsAfterCrush > 0 {
		v := config.MaxItemsAfterCrush
		maxK = &v
	}
	kTotal := adaptivesizer.ComputeOptimalK(kvStrings, bias, 3, maxK)

	if kTotal >= n {
		return copyMap(obj), "object:passthrough"
	}

	// Always keep: error-keyword values.
	keepKeys := make(map[string]bool)
	for _, key := range keys {
		valLower := strings.ToLower(string(obj[key]))
		for _, kw := range ErrorKeywords {
			if strings.Contains(valLower, kw) {
				keepKeys[key] = true
				break
			}
		}
	}

	// Always keep: small values (<=12 tokens).
	smallThreshold := 50 / 4
	for i, key := range keys {
		if kvTokens[i] <= smallThreshold {
			keepKeys[key] = true
		}
	}

	// Boundary: first kFirst and last kLast.
	kFirst := max(1, int(roundTiesEven(float64(kTotal)*config.FirstFraction)))
	kLast := max(1, int(roundTiesEven(float64(kTotal)*config.LastFraction)))
	for i := 0; i < min(kFirst, n); i++ {
		keepKeys[keys[i]] = true
	}
	for i := max(0, n-kLast); i < n; i++ {
		keepKeys[keys[i]] = true
	}

	// Stride fill.
	remaining := kTotal - len(keepKeys)
	if remaining < 0 {
		remaining = 0
	}
	if remaining > 0 {
		stride := (n - 1) / (remaining + 1)
		if stride < 1 {
			stride = 1
		}
		for i := 0; i < n; i += stride {
			// Recompute error count each iteration (Python behavior).
			errorKeptCount := 0
			for k := range keepKeys {
				valLower := strings.ToLower(string(obj[k]))
				for _, kw := range ErrorKeywords {
					if strings.Contains(valLower, kw) {
						errorKeptCount++
						break
					}
				}
			}
			if len(keepKeys) >= kTotal+errorKeptCount {
				break
			}
			keepKeys[keys[i]] = true
		}
	}

	// Build output preserving original key order.
	result := make(map[string]json.RawMessage)
	for _, key := range keys {
		if keepKeys[key] {
			result[key] = obj[key]
		}
	}

	strategy := fmt.Sprintf("object:adaptive(%d->%d keys)", n, len(result))
	return result, strategy
}

// --- helpers ---

// roundTiesEven implements Python's banker's rounding.
func roundTiesEven(x float64) float64 {
	return math.RoundToEven(x)
}

// percentileLinear implements linear interpolation percentile (numpy "linear").
func percentileLinear(sortedValues []float64, q float64) float64 {
	n := len(sortedValues)
	if n == 0 {
		return 0.0
	}
	if n == 1 {
		return sortedValues[0]
	}
	pos := q * float64(n-1)
	lo := int(pos)
	hi := lo + 1
	if hi >= n {
		hi = lo
	}
	frac := pos - float64(lo)
	return sortedValues[lo]*(1.0-frac) + sortedValues[hi]*frac
}

func finiteMin(values []float64) float64 {
	if len(values) == 0 {
		return 0.0
	}
	m := values[0]
	for _, v := range values[1:] {
		if v < m {
			m = v
		}
	}
	return m
}

func finiteMax(values []float64) float64 {
	if len(values) == 0 {
		return 0.0
	}
	m := values[0]
	for _, v := range values[1:] {
		if v > m {
			m = v
		}
	}
	return m
}

func formatNumberRepr(x float64) string {
	if math.IsNaN(x) {
		return "nan"
	}
	if math.IsInf(x, 1) {
		return "inf"
	}
	if math.IsInf(x, -1) {
		return "-inf"
	}
	if x == math.Trunc(x) && math.Abs(x) < 1e16 {
		return fmt.Sprintf("%d", int64(x))
	}
	return fmt.Sprintf("%g", x)
}

func meanSlice(values []float64) float64 {
	if len(values) == 0 {
		return 0.0
	}
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

func sortedMapKeys(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func copyMap(m map[string]json.RawMessage) map[string]json.RawMessage {
	result := make(map[string]json.RawMessage, len(m))
	for k, v := range m {
		result[k] = v
	}
	return result
}
