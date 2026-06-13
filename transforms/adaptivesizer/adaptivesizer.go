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
	"bytes"
	"compress/flate"
	"crypto/md5"
	"encoding/binary"
	"io"
	"math"
	"math/bits"
	"strings"
	"sync"
	"unicode/utf8"
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
	// Use FNV-style hashing of bigram pairs to avoid string key allocations.
	// Two words are hashed into a single uint64 key.
	seen := make(map[uint64]struct{}, len(items)*2)
	curve := make([]int, 0, len(items))

	// wordStarts/wordEnds are reused across iterations to avoid
	// allocating a []string from strings.Fields.
	var wordStarts, wordEnds []int

	for _, item := range items {
		// Find word boundaries with inline lowercasing for hash computation.
		// We avoid strings.ToLower and strings.Fields entirely.
		wordStarts = wordStarts[:0]
		wordEnds = wordEnds[:0]

		i := 0
		for i < len(item) {
			// Skip whitespace.
			for i < len(item) && isSpaceByte(item[i]) {
				i++
			}
			if i >= len(item) {
				break
			}
			wordStarts = append(wordStarts, i)
			for i < len(item) && !isSpaceByte(item[i]) {
				i++
			}
			wordEnds = append(wordEnds, i)
		}

		nw := len(wordStarts)
		if nw < 2 {
			// Hash single word (or empty).
			h := fnvWordLower(item, wordStarts, wordEnds, 0)
			// Combine with zero second-word hash.
			seen[h] = struct{}{}
		} else {
			for j := 0; j < nw-1; j++ {
				h1 := fnvWordLower(item, wordStarts, wordEnds, j)
				h2 := fnvWordLower(item, wordStarts, wordEnds, j+1)
				// Combine the two hashes.
				combined := h1*6364136223846793005 + h2
				seen[combined] = struct{}{}
			}
		}
		curve = append(curve, len(seen))
	}

	return curve
}

func isSpaceByte(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

// fnvWordLower computes FNV-1a hash of the j-th word in item,
// lowercasing ASCII bytes inline. If no words, returns a fixed hash.
func fnvWordLower(item string, starts, ends []int, j int) uint64 {
	if len(starts) == 0 {
		return 0xcbf29ce484222325 // FNV offset basis for empty
	}
	h := uint64(0xcbf29ce484222325)
	for i := starts[j]; i < ends[j]; i++ {
		c := item[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		h ^= uint64(c)
		h *= 0x100000001b3
	}
	return h
}

// isASCII reports whether s contains only ASCII bytes.
func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
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
	// Fast path for ASCII-only strings: avoid strings.ToLower (alloc)
	// and []rune (alloc) by working directly on bytes with inline toLower.
	if isASCII(text) {
		return simhashASCII(text)
	}
	return simhashUnicode(text)
}

// simhashASCII handles the common case where text is pure ASCII.
// Zero allocations: operates on the original string bytes with inline lowering.
func simhashASCII(text string) uint64 {
	n := len(text)
	iterCount := 1
	if n > 3 {
		iterCount = n - 3
	}

	var votes [64]int32
	var buf [4]byte

	for i := 0; i < iterCount; i++ {
		end := i + 4
		if end > n {
			end = n
		}
		for k, j := 0, i; j < end; j++ {
			c := text[j]
			if c >= 'A' && c <= 'Z' {
				c += 'a' - 'A'
			}
			buf[k] = c
			k++
		}
		gramLen := end - i

		digest := md5.Sum(buf[:gramLen])
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

// simhashUnicode handles the general case with non-ASCII characters.
func simhashUnicode(text string) uint64 {
	lower := strings.ToLower(text)
	chars := []rune(lower)
	n := len(chars)

	// For n<=3, single iteration on the whole string. For n>=4, n-3 windows.
	iterCount := 1
	if n > 3 {
		iterCount = n - 3
	}

	var votes [64]int32
	// Reusable buffer for encoding rune grams to bytes -- avoids
	// string(chars[i:end]) + []byte(gram) allocations per window.
	var buf [16]byte // 4 runes * up to 4 bytes each

	for i := 0; i < iterCount; i++ {
		end := i + 4
		if end > n {
			end = n
		}
		// Encode runes directly into buf, no intermediate string.
		off := 0
		for _, r := range chars[i:end] {
			off += utf8.EncodeRune(buf[off:], r)
		}

		digest := md5.Sum(buf[:off])
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

	// Pre-allocate clusters with a reasonable initial capacity to
	// avoid repeated slice growth.
	clusters := make([]uint64, 0, min(len(items), 32))
	for _, fp := range fingerprints {
		matched := false
		for _, rep := range clusters {
			if bits.OnesCount64(fp^rep) <= threshold {
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

	fullCompressed := flateCompressedLen(fullText)
	subsetCompressed := flateCompressedLen(subsetText)

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

var flatePool = sync.Pool{
	New: func() interface{} {
		w, _ := flate.NewWriter(nil, flate.BestSpeed)
		return w
	},
}

// flateCompressedLen compresses data with DEFLATE at BestSpeed (level 1)
// and returns the output length. Uses a sync.Pool to reuse flate.Writer
// objects, avoiding ~1.7GB of allocations.
func flateCompressedLen(s string) int {
	var buf bytes.Buffer
	w := flatePool.Get().(*flate.Writer)
	w.Reset(&buf)
	_, _ = io.WriteString(w, s)
	_ = w.Close()
	flatePool.Put(w)
	return buf.Len()
}
