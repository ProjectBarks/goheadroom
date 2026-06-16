package smartcrusher

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/projectbarks/goheadroom/core/transforms/anchorselector"
)

// DeduplicateIndicesByContent collapses content-duplicate indices to their
// lowest representative. Port of Rust deduplicate_indices_by_content.
func DeduplicateIndicesByContent(keepIndices map[int]bool, items []json.RawMessage) map[int]bool {
	if len(keepIndices) == 0 {
		return make(map[int]bool)
	}

	// Sort indices for deterministic iteration (lowest first).
	sorted := setToSortedSlice(keepIndices)

	// hash -> lowest-seen index.
	seen := make(map[string]int)
	for _, idx := range sorted {
		if idx >= len(items) {
			continue
		}
		h := itemContentHash(items[idx], idx)
		if _, exists := seen[h]; !exists {
			seen[h] = idx
		}
	}

	result := make(map[int]bool, len(seen))
	for _, idx := range seen {
		result[idx] = true
	}
	return result
}

// FillRemainingSlots fills keep_indices back up to effectiveMax with diverse,
// content-unique items. Port of Rust fill_remaining_slots.
func FillRemainingSlots(keepIndices map[int]bool, items []json.RawMessage, n, effectiveMax int) map[int]bool {
	remaining := effectiveMax - len(keepIndices)
	if remaining <= 0 {
		return copySet(keepIndices)
	}

	// Hashes of items we're already keeping.
	seen := make(map[string]bool)
	for idx := range keepIndices {
		if idx < n {
			seen[itemContentHash(items[idx], idx)] = true
		}
	}

	// Candidate pool: every index not already kept.
	var candidates []int
	for i := 0; i < n; i++ {
		if !keepIndices[i] {
			candidates = append(candidates, i)
		}
	}
	if len(candidates) == 0 {
		return copySet(keepIndices)
	}

	result := copySet(keepIndices)
	step := len(candidates) / (remaining + 1)
	if step < 1 {
		step = 1
	}
	added := 0

	// Python's interleaved stride: outer loop offsets [0, step).
outer:
	for startOffset := 0; startOffset < step; startOffset++ {
		if added >= remaining {
			break
		}
		for i := startOffset; i < len(candidates); i += step {
			if added >= remaining {
				break outer
			}
			idx := candidates[i]
			h := itemContentHash(items[idx], idx)
			if !seen[h] {
				result[idx] = true
				seen[h] = true
				added++
			}
		}
	}

	return result
}

// PrioritizeIndices applies dedup + fill, then critical-items-first prioritization.
// Port of Rust prioritize_indices.
func PrioritizeIndices(
	config *SmartCrusherConfig,
	keepIndices map[int]bool,
	items []json.RawMessage,
	n int,
	analysis *ArrayAnalysis,
	effectiveMax int,
) map[int]bool {
	// Dedup pass.
	var current map[int]bool
	if config.DedupIdenticalItems {
		current = DeduplicateIndicesByContent(keepIndices, items)
	} else {
		current = copySet(keepIndices)
	}

	// Fill pass.
	if len(current) < effectiveMax && len(current) < n {
		current = FillRemainingSlots(current, items, n, effectiveMax)
	}

	if len(current) <= effectiveMax {
		return current
	}

	// Over budget - apply critical-items-first prioritization.

	// Error indices.
	errorIndices := make(map[int]bool)
	for _, idx := range DetectErrorItemsForPreservation(items, nil) {
		errorIndices[idx] = true
	}

	// Structural outlier indices.
	outlierIndices := make(map[int]bool)
	for _, idx := range DetectStructuralOutliers(items) {
		outlierIndices[idx] = true
	}

	// Numeric anomaly indices.
	anomalyIndices := numericAnomalyIndices(config, items, analysis)

	prioritized := make(map[int]bool)
	for idx := range errorIndices {
		prioritized[idx] = true
	}
	for idx := range outlierIndices {
		prioritized[idx] = true
	}
	for idx := range anomalyIndices {
		prioritized[idx] = true
	}

	// First 3 / last 2 anchors if we have room.
	budgetRemaining := effectiveMax - len(prioritized)
	if budgetRemaining < 0 {
		budgetRemaining = 0
	}
	if budgetRemaining > 0 {
		limit := 3
		if limit > n {
			limit = n
		}
		for i := 0; i < limit; i++ {
			if !prioritized[i] && budgetRemaining > 0 {
				prioritized[i] = true
				budgetRemaining--
			}
		}
		lastStart := n - 2
		if lastStart < 0 {
			lastStart = 0
		}
		for i := lastStart; i < n; i++ {
			if !prioritized[i] && budgetRemaining > 0 {
				prioritized[i] = true
				budgetRemaining--
			}
		}
	}

	// Fill with other-important indices (ascending order).
	if budgetRemaining > 0 {
		others := setDifference(current, prioritized)
		sort.Ints(others)
		for _, i := range others {
			if budgetRemaining == 0 {
				break
			}
			prioritized[i] = true
			budgetRemaining--
		}
	}

	return prioritized
}

// numericAnomalyIndices returns numeric anomaly indices from analysis.
func numericAnomalyIndices(config *SmartCrusherConfig, items []json.RawMessage, analysis *ArrayAnalysis) map[int]bool {
	anomalies := make(map[int]bool)
	if analysis == nil || len(analysis.FieldStats) == 0 {
		return anomalies
	}

	for fieldName, stats := range analysis.FieldStats {
		if stats.FieldType != "numeric" || stats.MeanVal == nil {
			continue
		}
		variance := 0.0
		if stats.Variance != nil {
			variance = *stats.Variance
		}
		if variance <= 0 {
			continue
		}
		std := sqrt(variance)
		if std <= 0 {
			continue
		}
		meanVal := *stats.MeanVal
		threshold := config.VarianceThreshold * std

		for i, raw := range items {
			var obj map[string]json.RawMessage
			if err := json.Unmarshal(raw, &obj); err != nil {
				continue
			}
			v, ok := obj[fieldName]
			if !ok {
				continue
			}
			var num float64
			if err := json.Unmarshal(v, &num); err != nil {
				continue
			}
			if !isFinite(num) {
				continue
			}
			if abs(num-meanVal) > threshold {
				anomalies[i] = true
			}
		}
	}

	return anomalies
}

// itemContentHash returns a content hash for an item.
// Uses ComputeItemHash for objects/arrays, falls back to string content hash.
func itemContentHash(item json.RawMessage, idx int) string {
	trimmed := trimLeft(string(item))
	if len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[') {
		var v interface{}
		if err := json.Unmarshal(item, &v); err == nil {
			return anchorselector.ComputeItemHash(v)
		}
	}
	// Fallback for non-object/array items.
	var content string
	var s string
	if err := json.Unmarshal(item, &s); err == nil {
		content = s
	} else {
		content = string(item)
	}
	digest := md5.Sum([]byte(content))
	return fmt.Sprintf("%x", digest)[:16]
}

// --- helpers ---

func copySet(s map[int]bool) map[int]bool {
	result := make(map[int]bool, len(s))
	for k, v := range s {
		result[k] = v
	}
	return result
}

func setDifference(a, b map[int]bool) []int {
	var result []int
	for k := range a {
		if !b[k] {
			result = append(result, k)
		}
	}
	return result
}

func trimLeft(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] != ' ' && s[i] != '\t' && s[i] != '\n' && s[i] != '\r' {
			return s[i:]
		}
	}
	return ""
}
