// Package adaptivesizer provides adaptive compression sizing via
// information saturation detection.
//
// Direct port of headroom-core/src/transforms/adaptive_sizer.rs.
//
// Three-tier decision:
//  1. Fast path: trivial cases (n <= 8 -> keep all) and near-total
//     redundancy (<=3 unique-by-simhash -> keep that count).
//  2. Standard: Kneedle on cumulative unique-bigram coverage curve.
//     Coverage stops growing -> that is the knee -> return that count.
//  3. Validation: zlib-ratio sanity check. If keeping k items produces
//     a much-more-redundant subset than the full set, bump k by 20%.
package adaptivesizer

import (
	"compress/flate"
	"crypto/md5"
	"encoding/binary"
	"math"
	"math/bits"
	"strings"
)

// ComputeOptimalK computes the optimal number of items to keep via
// information saturation detection.
//
// items are string representations in importance order.
// bias is a multiplier on the knee point (>1 = keep more).
// minK is the lower bound on the return value.
// maxK is the upper bound; nil means no cap (up to len(items)).
func ComputeOptimalK(items []string, bias float64, minK int, maxK *int) int {
	n := len(items)
	effectiveMax := n
	if maxK != nil {
		effectiveMax = *maxK
	}

	// Tier 1: fast path.
	if n <= 8 {
		return n
	}

	// Near-total redundancy: at most 3 unique groups -> keep that many.
	uniqueCount := CountUniqueSimhash(items, 3)
	if uniqueCount <= 3 {
		k := max(minK, uniqueCount)
		return min(k, effectiveMax)
	}

	// Tier 2: Kneedle on bigram-coverage curve.
	curve := ComputeUniqueBigramCurve(items)
	knee := FindKnee(curve)

	// Diversity ratio: fraction of items that are genuinely unique.
	diversityRatio := float64(uniqueCount) / float64(n)

	if knee == nil {
		// No saturation found -- scale keep-fraction with diversity.
		keepFraction := 0.3 + 0.7*diversityRatio
		v := max(minK, int(float64(n)*keepFraction))
		knee = &v
	} else if diversityRatio > 0.7 {
		// Knee found, but high diversity -- apply diversity floor.
		floor := max(minK, int(float64(n)*(0.3+0.7*diversityRatio)))
		v := max(*knee, floor)
		knee = &v
	}

	kneeVal := minK
	if knee != nil {
		kneeVal = *knee
	}

	// Apply bias multiplier.
	k := max(minK, int(float64(kneeVal)*bias))
	k = min(k, effectiveMax)

	// Tier 3: zlib-ratio validation.
	k = ValidateWithZlib(items, k, effectiveMax, 0.15)

	// Final clamp.
	return max(minK, min(k, effectiveMax))
}

// FindKnee finds the knee in a monotonically-increasing curve using
// the Kneedle algorithm. Returns a pointer to the 1-indexed knee
// point, or nil if no knee is found (curve too short or too linear).
func FindKnee(curve []int) *int {
	n := len(curve)
	if n < 3 {
		return nil
	}

	yMin := float64(curve[0])
	yMax := float64(curve[n-1])

	if math.Abs(yMax-yMin) < 1e-15 {
		// Flat curve -- all items are identical.
		v := 1
		return &v
	}

	xRange := float64(n - 1)
	yRange := yMax - yMin

	maxDiff := -1.0
	kneeIdx := -1

	for i, y := range curve {
		xNorm := float64(i) / xRange
		yNorm := (float64(y) - yMin) / yRange
		diff := yNorm - xNorm
		if diff > maxDiff {
			maxDiff = diff
			kneeIdx = i
		}
	}

	if maxDiff < 0.05 {
		return nil
	}

	v := kneeIdx + 1
	return &v
}

// ComputeUniqueBigramCurve computes the cumulative unique-bigram
// coverage curve. Each item contributes its word-level bigrams;
// single-word items contribute (word, ""). The curve at index k is the
// running count of unique bigrams after seeing items[0..k].
func ComputeUniqueBigramCurve(items []string) []int {
	type bigram struct{ a, b string }
	seen := make(map[bigram]struct{})
	curve := make([]int, 0, len(items))

	for _, item := range items {
		lower := strings.ToLower(item)
		words := strings.Fields(lower)
		if len(words) < 2 {
			first := ""
			if len(words) > 0 {
				first = words[0]
			}
			seen[bigram{first, ""}] = struct{}{}
		} else {
			for j := 0; j < len(words)-1; j++ {
				seen[bigram{words[j], words[j+1]}] = struct{}{}
			}
		}
		curve = append(curve, len(seen))
	}

	return curve
}

// Simhash computes a 64-bit SimHash fingerprint of a text string.
//
// Algorithm:
//  1. Iterate character 4-grams (sliding window). For input shorter
//     than 4 chars, the loop runs once with the entire string.
//  2. Hash each gram with MD5; take the first 64 bits as big-endian u64.
//  3. Per-bit voting: +1 if set, -1 if clear.
//  4. Final bit j is set iff votes[j] > 0 (strict).
func Simhash(text string) uint64 {
	lower := strings.ToLower(text)
	chars := []rune(lower)
	n := len(chars)

	// For n<=3, single iteration on the whole string. For n>=4, n-3 windows.
	iterCount := 1
	if n > 3 {
		iterCount = n - 3
	}

	var votes [64]int32

	for i := 0; i < iterCount; i++ {
		end := i + 4
		if end > n {
			end = n
		}
		gram := string(chars[i:end])

		digest := md5.Sum([]byte(gram))
		h := binary.BigEndian.Uint64(digest[:8])

		for j := range 64 {
			if (h>>j)&1 == 1 {
				votes[j]++
			} else {
				votes[j]--
			}
		}
	}

	var fingerprint uint64
	for j, v := range votes {
		if v > 0 {
			fingerprint |= 1 << j
		}
	}
	return fingerprint
}

// HammingDistance returns the Hamming distance between two 64-bit
// SimHash fingerprints.
func HammingDistance(a, b uint64) int {
	return bits.OnesCount64(a ^ b)
}

// CountUniqueSimhash counts items with distinct content via SimHash
// and greedy clustering. Two items cluster together when their
// fingerprints are within threshold Hamming distance.
func CountUniqueSimhash(items []string, threshold int) int {
	if len(items) == 0 {
		return 0
	}

	fingerprints := make([]uint64, len(items))
	for i, s := range items {
		fingerprints[i] = Simhash(s)
	}

	var clusters []uint64
	for _, fp := range fingerprints {
		matched := false
		for _, rep := range clusters {
			if HammingDistance(fp, rep) <= threshold {
				matched = true
				break
			}
		}
		if !matched {
			clusters = append(clusters, fp)
		}
	}

	return len(clusters)
}

// ValidateWithZlib validates the chosen k using zlib compression ratios.
// If the subset items[:k] compresses much better than the full set, the
// subset is missing diversity, so bump k by 20%.
func ValidateWithZlib(items []string, k, maxK int, tolerance float64) int {
	if k >= len(items) || k >= maxK {
		return k
	}

	fullText := strings.Join(items, "\n")
	subsetText := strings.Join(items[:k], "\n")

	// Skip validation for very small content (zlib overhead dominates).
	if len(fullText) < 200 {
		return k
	}

	fullCompressed := flateCompressedLen([]byte(fullText))
	subsetCompressed := flateCompressedLen([]byte(subsetText))

	fullRatio := 1.0
	if len(fullText) > 0 {
		fullRatio = float64(fullCompressed) / float64(len(fullText))
	}
	subsetRatio := 1.0
	if len(subsetText) > 0 {
		subsetRatio = float64(subsetCompressed) / float64(len(subsetText))
	}

	ratioDiff := math.Abs(fullRatio - subsetRatio)

	if ratioDiff > tolerance {
		adjusted := int(float64(k) * 1.2)
		return min(adjusted, maxK)
	}

	return k
}

// flateCompressedLen compresses data with DEFLATE at BestSpeed (level 1)
// and returns the output length.
func flateCompressedLen(data []byte) int {
	var buf strings.Builder
	w, err := flate.NewWriter(&buf, flate.BestSpeed)
	if err != nil {
		return len(data)
	}
	_, _ = w.Write(data)
	_ = w.Close()
	return buf.Len()
}
