// Package anchorselector provides dynamic anchor selection for array compression.
//
// Direct port of headroom-core/src/transforms/anchor_selector.rs.
// Used by the smart crusher analyzer to allocate position-based anchor
// slots: the items kept purely for their position in the array, not
// their relevance score.
//
// Given an array of N items and a target K (max items after compression),
// decide which K' < K positions to "anchor" (always keep). The choice
// depends on:
//
//  1. Pattern: search results favor the front; logs favor the back;
//     time series want both ends; generic spreads evenly.
//  2. Query keywords: "latest"/"recent" shift toward back;
//     "first"/"earliest" shift toward front.
//  3. Information density (middle region only): compute a [0,1] score
//     per candidate based on field-value uniqueness, content length,
//     and structural uniqueness.
//  4. Dedup: identical items hash to the same MD5[:16]; duplicates
//     are skipped so we don't waste slots.
//
// Hash parity with Python
//
// ComputeItemHash returns md5(json.dumps(item, sort_keys=True,
// default=str)).hexdigest()[:16]. Python's json.dumps by default
// emits ", " and ": " separators and ASCII-escapes non-ASCII via
// \uXXXX. We replicate this in PythonJsonDumps below.
package anchorselector

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
)

// ============================================================================
// Configuration (Python headroom/config.py:294 AnchorConfig)
// ============================================================================

// AnchorConfig holds configuration for dynamic anchor allocation.
// Defaults match Python byte-for-byte.
type AnchorConfig struct {
	AnchorBudgetPct float64
	MinAnchorSlots  int
	MaxAnchorSlots  int

	DefaultFrontWeight  float64
	DefaultBackWeight   float64
	DefaultMiddleWeight float64

	SearchFrontWeight float64
	SearchBackWeight  float64
	LogsFrontWeight   float64
	LogsBackWeight    float64

	RecencyKeywords    []string
	HistoricalKeywords []string

	UseInformationDensity bool
	CandidateMultiplier   int
	DedupIdenticalItems   bool
}

// DefaultAnchorConfig returns the default anchor configuration matching Python.
func DefaultAnchorConfig() AnchorConfig {
	return AnchorConfig{
		AnchorBudgetPct: 0.25,
		MinAnchorSlots:  3,
		MaxAnchorSlots:  12,

		DefaultFrontWeight:  0.5,
		DefaultBackWeight:   0.4,
		DefaultMiddleWeight: 0.1,

		SearchFrontWeight: 0.75,
		SearchBackWeight:  0.15,
		LogsFrontWeight:   0.15,
		LogsBackWeight:    0.75,

		RecencyKeywords:    []string{"latest", "recent", "last", "newest", "current", "now"},
		HistoricalKeywords: []string{"first", "oldest", "earliest", "original", "initial", "beginning"},

		UseInformationDensity: true,
		CandidateMultiplier:   3,
		DedupIdenticalItems:   true,
	}
}

// ============================================================================
// Enums
// ============================================================================

// DataPattern is the detected data pattern that drives anchor strategy selection.
type DataPattern int

const (
	SearchResults DataPattern = iota
	Logs
	TimeSeries
	Generic
)

// DataPatternFromString converts a string to DataPattern. Unknown strings
// fall through to Generic.
func DataPatternFromString(s string) DataPattern {
	switch strings.ToLower(s) {
	case "search_results":
		return SearchResults
	case "logs":
		return Logs
	case "time_series":
		return TimeSeries
	case "generic":
		return Generic
	default:
		return Generic
	}
}

// AnchorStrategy is the anchor distribution strategy.
type AnchorStrategy int

const (
	FrontHeavy AnchorStrategy = iota
	BackHeavy
	Balanced
	Distributed
)

// AnchorWeights holds distribution weights for front/middle/back regions.
type AnchorWeights struct {
	Front  float64
	Middle float64
	Back   float64
}

// DefaultAnchorWeights returns the default weights.
func DefaultAnchorWeights() AnchorWeights {
	return AnchorWeights{Front: 0.5, Middle: 0.1, Back: 0.4}
}

// Normalize returns a copy with weights summing to 1.0.
// If total is 0, returns DefaultAnchorWeights.
func (w AnchorWeights) Normalize() AnchorWeights {
	total := w.Front + w.Middle + w.Back
	if total == 0.0 {
		return DefaultAnchorWeights()
	}
	return AnchorWeights{
		Front:  w.Front / total,
		Middle: w.Middle / total,
		Back:   w.Back / total,
	}
}

// ============================================================================
// Python-compatible JSON serialization
// ============================================================================

// PythonJsonDumps serializes a value matching Python's
// json.dumps(value, sort_keys=True) format exactly:
//   - Separators: ", " and ": " (with spaces).
//   - Object keys sorted alphabetically at all levels.
//   - Non-ASCII characters escaped as \uXXXX (ensure_ascii=True).
//   - Surrogate pairs for codepoints above U+FFFF.
func PythonJsonDumps(value interface{}) string {
	var buf strings.Builder
	writePythonJSON(value, &buf, true)
	return buf.String()
}

func writePythonJSON(value interface{}, buf *strings.Builder, sortKeys bool) {
	if value == nil {
		buf.WriteString("null")
		return
	}
	switch v := value.(type) {
	case bool:
		if v {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
	case json.Number:
		buf.WriteString(string(v))
	case float64:
		// Python outputs integers without decimal point when they are whole.
		if v == math.Trunc(v) && !math.IsInf(v, 0) && !math.IsNaN(v) {
			buf.WriteString(fmt.Sprintf("%d", int64(v)))
		} else {
			buf.WriteString(fmt.Sprintf("%g", v))
		}
	case int:
		buf.WriteString(fmt.Sprintf("%d", v))
	case int64:
		buf.WriteString(fmt.Sprintf("%d", v))
	case string:
		writePythonJSONString(v, buf)
	case map[string]interface{}:
		buf.WriteByte('{')
		if sortKeys {
			keys := make([]string, 0, len(v))
			for k := range v {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for i, k := range keys {
				if i > 0 {
					buf.WriteString(", ")
				}
				writePythonJSONString(k, buf)
				buf.WriteString(": ")
				writePythonJSON(v[k], buf, sortKeys)
			}
		} else {
			i := 0
			for k, val := range v {
				if i > 0 {
					buf.WriteString(", ")
				}
				writePythonJSONString(k, buf)
				buf.WriteString(": ")
				writePythonJSON(val, buf, sortKeys)
				i++
			}
		}
		buf.WriteByte('}')
	case []interface{}:
		buf.WriteByte('[')
		for i, item := range v {
			if i > 0 {
				buf.WriteString(", ")
			}
			writePythonJSON(item, buf, sortKeys)
		}
		buf.WriteByte(']')
	default:
		// Fallback: use Go's JSON encoding, though this shouldn't happen
		// with properly parsed JSON values.
		b, _ := json.Marshal(v)
		buf.Write(b)
	}
}

// writePythonJSONString writes a JSON string with Python's ensure_ascii=True
// behavior: non-ASCII chars become \uXXXX, codepoints above U+FFFF use
// surrogate pairs.
func writePythonJSONString(s string, buf *strings.Builder) {
	buf.WriteByte('"')
	for _, c := range s {
		switch c {
		case '"':
			buf.WriteString("\\\"")
		case '\\':
			buf.WriteString("\\\\")
		case '\b':
			buf.WriteString("\\b")
		case '\t':
			buf.WriteString("\\t")
		case '\n':
			buf.WriteString("\\n")
		case '\f':
			buf.WriteString("\\f")
		case '\r':
			buf.WriteString("\\r")
		default:
			if c < 0x20 {
				buf.WriteString(fmt.Sprintf("\\u%04x", c))
			} else if c <= 0x7E {
				buf.WriteByte(byte(c))
			} else {
				// Non-ASCII: ensure_ascii=True
				cp := uint32(c)
				if cp <= 0xFFFF {
					buf.WriteString(fmt.Sprintf("\\u%04x", cp))
				} else {
					// Surrogate pair
					cp -= 0x10000
					hi := 0xD800 + (cp >> 10)
					lo := 0xDC00 + (cp & 0x3FF)
					buf.WriteString(fmt.Sprintf("\\u%04x\\u%04x", hi, lo))
				}
			}
		}
	}
	buf.WriteByte('"')
}

// ============================================================================
// Item hashing
// ============================================================================

// ComputeItemHash computes a 16-hex-char MD5 hash of the item's content.
// Matches Python: md5(json.dumps(item, sort_keys=True).encode()).hexdigest()[:16].
func ComputeItemHash(item interface{}) string {
	content := PythonJsonDumps(item)
	digest := md5.Sum([]byte(content))
	hex := fmt.Sprintf("%x", digest)
	return hex[:16]
}

// ============================================================================
// Information density scoring
// ============================================================================

// CalculateInformationScore computes an information density score in [0.0, 1.0]
// combining value uniqueness (0.4), content length (0.3), and structural
// uniqueness (0.3).
func CalculateInformationScore(item interface{}, allItems []interface{}) float64 {
	if len(allItems) == 0 {
		return 0.0
	}
	_, ok := item.(map[string]interface{})
	if !ok {
		return 0.0
	}

	uniqueness := calculateValueUniqueness(item, allItems)
	length := calculateLengthScore(item, allItems)
	structural := calculateStructuralUniqueness(item, allItems)

	score := uniqueness*0.4 + length*0.3 + structural*0.3
	return clamp(score, 0.0, 1.0)
}

func calculateValueUniqueness(item interface{}, allItems []interface{}) float64 {
	if len(allItems) < 2 {
		return 0.5
	}

	// Build per-field value counts.
	fieldCounts := map[string]map[string]int{}
	for _, other := range allItems {
		obj, ok := other.(map[string]interface{})
		if !ok {
			continue
		}
		for key, value := range obj {
			valueStr := stringifyForUniqueness(value)
			if _, exists := fieldCounts[key]; !exists {
				fieldCounts[key] = map[string]int{}
			}
			fieldCounts[key][valueStr]++
		}
	}

	itemObj, ok := item.(map[string]interface{})
	if !ok {
		return 0.5
	}

	totalItems := float64(len(allItems))
	var rarenessScores []float64

	for key, value := range itemObj {
		counts, exists := fieldCounts[key]
		if !exists {
			continue
		}
		valueStr := stringifyForUniqueness(value)
		count, exists := counts[valueStr]
		if exists && count > 0 {
			frequency := float64(count) / totalItems
			rarenessScores = append(rarenessScores, 1.0-frequency)
		}
	}

	if len(rarenessScores) == 0 {
		return 0.5
	}
	sum := 0.0
	for _, s := range rarenessScores {
		sum += s
	}
	return sum / float64(len(rarenessScores))
}

// stringifyForUniqueness mirrors Python: bare strings stay bare; everything
// else uses the Python-compatible sort-keys serializer.
func stringifyForUniqueness(value interface{}) string {
	if s, ok := value.(string); ok {
		return s
	}
	return PythonJsonDumps(value)
}

func calculateLengthScore(item interface{}, allItems []interface{}) float64 {
	if len(allItems) < 2 {
		return 0.5
	}

	itemBytes, _ := json.Marshal(item)
	itemLength := len(itemBytes)

	var lengths []int
	for _, other := range allItems {
		if _, ok := other.(map[string]interface{}); !ok {
			continue
		}
		b, _ := json.Marshal(other)
		lengths = append(lengths, len(b))
	}

	if len(lengths) == 0 {
		return 0.5
	}

	maxLen := lengths[0]
	minLen := lengths[0]
	for _, l := range lengths[1:] {
		if l > maxLen {
			maxLen = l
		}
		if l < minLen {
			minLen = l
		}
	}

	if maxLen == minLen {
		return 0.5
	}

	return float64(itemLength-minLen) / float64(maxLen-minLen)
}

func calculateStructuralUniqueness(item interface{}, allItems []interface{}) float64 {
	var valid []map[string]interface{}
	for _, v := range allItems {
		if obj, ok := v.(map[string]interface{}); ok {
			valid = append(valid, obj)
		}
	}
	n := len(valid)
	if n < 2 {
		return 0.5
	}

	fieldCounts := map[string]int{}
	for _, obj := range valid {
		for key := range obj {
			fieldCounts[key]++
		}
	}

	nf := float64(n)
	common := map[string]bool{}
	rare := map[string]bool{}
	for key, count := range fieldCounts {
		if float64(count) >= nf*0.8 {
			common[key] = true
		}
		if float64(count) < nf*0.2 {
			rare[key] = true
		}
	}

	itemObj, ok := item.(map[string]interface{})
	if !ok {
		return 0.5
	}
	itemFields := map[string]bool{}
	for key := range itemObj {
		itemFields[key] = true
	}

	hasRare := 0
	for key := range rare {
		if itemFields[key] {
			hasRare++
		}
	}
	missingCommon := 0
	for key := range common {
		if !itemFields[key] {
			missingCommon++
		}
	}

	uniqueness := 0.0
	if len(rare) > 0 {
		uniqueness += 0.5 * (float64(hasRare) / float64(max(len(rare), 1)))
	}
	if len(common) > 0 {
		uniqueness += 0.5 * (float64(missingCommon) / float64(max(len(common), 1)))
	}
	return math.Min(uniqueness, 1.0)
}

// ============================================================================
// AnchorSelector
// ============================================================================

// AnchorSelector performs dynamic anchor selection. Stateless other than Config.
type AnchorSelector struct {
	Config AnchorConfig
}

// NewAnchorSelector creates a new selector with the given config.
func NewAnchorSelector(config AnchorConfig) *AnchorSelector {
	return &AnchorSelector{Config: config}
}

// CalculateAnchorBudget calculates the number of anchor slots to allocate.
func (s *AnchorSelector) CalculateAnchorBudget(arraySize, maxItems int) int {
	if arraySize <= maxItems {
		return 0
	}
	raw := int(float64(maxItems) * s.Config.AnchorBudgetPct)
	budget := max(s.Config.MinAnchorSlots, raw)
	budget = min(s.Config.MaxAnchorSlots, budget)
	return min(budget, arraySize)
}

// StrategyForPattern returns the anchor strategy for a data pattern.
func (s *AnchorSelector) StrategyForPattern(pattern DataPattern) AnchorStrategy {
	switch pattern {
	case SearchResults:
		return FrontHeavy
	case Logs:
		return BackHeavy
	case TimeSeries:
		return Balanced
	case Generic:
		return Distributed
	default:
		return Distributed
	}
}

// BaseWeightsForStrategy returns the base weights for a strategy.
func (s *AnchorSelector) BaseWeightsForStrategy(strategy AnchorStrategy) AnchorWeights {
	switch strategy {
	case FrontHeavy:
		return AnchorWeights{
			Front:  s.Config.SearchFrontWeight,
			Middle: 1.0 - s.Config.SearchFrontWeight - s.Config.SearchBackWeight,
			Back:   s.Config.SearchBackWeight,
		}
	case BackHeavy:
		return AnchorWeights{
			Front:  s.Config.LogsFrontWeight,
			Middle: 1.0 - s.Config.LogsFrontWeight - s.Config.LogsBackWeight,
			Back:   s.Config.LogsBackWeight,
		}
	case Balanced:
		return AnchorWeights{
			Front:  0.45,
			Middle: 0.1,
			Back:   0.45,
		}
	case Distributed:
		return AnchorWeights{
			Front:  s.Config.DefaultFrontWeight,
			Middle: s.Config.DefaultMiddleWeight,
			Back:   s.Config.DefaultBackWeight,
		}
	default:
		return DefaultAnchorWeights()
	}
}

// AdjustWeightsForQuery adjusts weights based on query keywords.
// +0.15 toward back on recency keywords, +0.15 toward front on historical.
// Returns base unchanged when no keywords match (or both match).
func (s *AnchorSelector) AdjustWeightsForQuery(base AnchorWeights, query *string) AnchorWeights {
	if query == nil || *query == "" {
		return base
	}
	qLower := strings.ToLower(*query)

	hasRecency := false
	for _, kw := range s.Config.RecencyKeywords {
		if strings.Contains(qLower, kw) {
			hasRecency = true
			break
		}
	}
	hasHistorical := false
	for _, kw := range s.Config.HistoricalKeywords {
		if strings.Contains(qLower, kw) {
			hasHistorical = true
			break
		}
	}

	shift := 0.15
	if hasRecency && !hasHistorical {
		return AnchorWeights{
			Front:  math.Max(0.1, base.Front-shift),
			Middle: base.Middle,
			Back:   math.Min(0.8, base.Back+shift),
		}.Normalize()
	} else if hasHistorical && !hasRecency {
		return AnchorWeights{
			Front:  math.Min(0.8, base.Front+shift),
			Middle: base.Middle,
			Back:   math.Max(0.1, base.Back-shift),
		}.Normalize()
	}
	return base
}

// SelectAnchors selects anchor indices for an array.
func (s *AnchorSelector) SelectAnchors(
	items []interface{},
	maxItems int,
	pattern DataPattern,
	query *string,
) []int {
	arraySize := len(items)
	if arraySize == 0 {
		return nil
	}
	if arraySize <= maxItems {
		result := make([]int, arraySize)
		for i := range result {
			result[i] = i
		}
		return result
	}

	budget := s.CalculateAnchorBudget(arraySize, maxItems)
	if budget == 0 {
		return nil
	}

	strategy := s.StrategyForPattern(pattern)
	base := s.BaseWeightsForStrategy(strategy)
	weights := s.AdjustWeightsForQuery(base, query).Normalize()

	frontSlots := max(1, int(float64(budget)*weights.Front))
	backSlots := max(1, int(float64(budget)*weights.Back))
	middleSlots := budget - frontSlots - backSlots
	if middleSlots < 0 {
		middleSlots = 0
	}

	// Ensure we don't exceed budget.
	total := frontSlots + middleSlots + backSlots
	if total > budget {
		excess := total - budget
		middleReduction := min(middleSlots, excess)
		middleSlots -= middleReduction
		excess -= middleReduction
		if excess > 0 {
			backSlots = max(1, backSlots-excess)
		}
	}

	anchors := map[int]bool{}
	seen := map[string]bool{}

	// Front region: [0, min(frontSlots*2, arraySize/3))
	frontEnd := min(frontSlots*2, arraySize/3)
	frontAnchors := s.selectRegion(items, 0, frontEnd, frontSlots, seen, false)
	for _, idx := range frontAnchors {
		anchors[idx] = true
	}
	frontCount := len(frontAnchors)

	// Back region: [max(arraySize - backSlots*2, 2*arraySize/3), arraySize)
	backStart := max(arraySize-backSlots*2, (2*arraySize)/3)
	backAnchors := s.selectRegion(items, backStart, arraySize, backSlots, seen, false)
	for _, idx := range backAnchors {
		anchors[idx] = true
	}
	backCount := len(backAnchors)

	// Middle region
	if middleSlots > 0 {
		middleStart := frontCount
		middleEnd := arraySize - backCount
		if middleEnd > middleStart {
			middleAnchors := s.selectRegion(items, middleStart, middleEnd, middleSlots, seen, s.Config.UseInformationDensity)
			for _, idx := range middleAnchors {
				anchors[idx] = true
			}
		}
	}

	// Convert to sorted slice
	result := make([]int, 0, len(anchors))
	for idx := range anchors {
		result = append(result, idx)
	}
	sort.Ints(result)
	return result
}

func (s *AnchorSelector) selectRegion(
	items []interface{},
	startIdx, endIdx, numSlots int,
	seen map[string]bool,
	useDensity bool,
) []int {
	if numSlots == 0 || startIdx >= endIdx {
		return nil
	}
	regionSize := endIdx - startIdx

	if useDensity {
		return s.selectByDensity(items, startIdx, endIdx, numSlots, seen)
	}

	var selected []int
	if numSlots >= regionSize {
		// Take all (with dedup).
		for idx := startIdx; idx < endIdx; idx++ {
			if s.shouldInclude(items, idx, seen, false) {
				selected = append(selected, idx)
			}
		}
	} else {
		step := float64(regionSize) / float64(numSlots+1)
		for i := 0; i < numSlots; i++ {
			rawIdx := startIdx + int(float64(i+1)*step)
			idx := min(rawIdx, endIdx-1)
			if s.shouldInclude(items, idx, seen, false) {
				selected = append(selected, idx)
			} else {
				// Try adjacent indices.
				for _, offset := range []int{1, -1, 2, -2} {
					alt := idx + offset
					if alt < startIdx || alt >= endIdx {
						continue
					}
					if s.shouldInclude(items, alt, seen, false) {
						selected = append(selected, alt)
						break
					}
				}
			}
		}
	}
	return selected
}

func (s *AnchorSelector) selectByDensity(
	items []interface{},
	startIdx, endIdx, numSlots int,
	seen map[string]bool,
) []int {
	regionSize := endIdx - startIdx
	numCandidates := min(numSlots*s.Config.CandidateMultiplier, regionSize)
	step := 1.0
	if numCandidates > 0 {
		step = float64(regionSize) / float64(numCandidates+1)
	}

	regionItems := items[startIdx:endIdx]

	type candidate struct {
		idx   int
		score float64
	}
	var candidates []candidate

	for i := 0; i < numCandidates; i++ {
		raw := startIdx + int(float64(i+1)*step)
		idx := min(raw, endIdx-1)
		if !s.shouldInclude(items, idx, seen, true) {
			continue
		}
		item := items[idx]
		score := 0.5
		if _, ok := item.(map[string]interface{}); ok {
			score = CalculateInformationScore(item, regionItems)
		}
		candidates = append(candidates, candidate{idx: idx, score: score})
	}

	// Sort by score descending, then by index ascending for ties.
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		return candidates[i].idx < candidates[j].idx
	})

	var selected []int
	for _, c := range candidates {
		if len(selected) >= numSlots {
			break
		}
		if s.shouldInclude(items, c.idx, seen, false) {
			selected = append(selected, c.idx)
		}
	}
	return selected
}

func (s *AnchorSelector) shouldInclude(
	items []interface{},
	idx int,
	seen map[string]bool,
	checkOnly bool,
) bool {
	if !s.Config.DedupIdenticalItems {
		return true
	}
	if idx >= len(items) {
		return false
	}
	item := items[idx]
	if _, ok := item.(map[string]interface{}); !ok {
		return true
	}
	h := ComputeItemHash(item)
	if seen[h] {
		return false
	}
	if !checkOnly {
		seen[h] = true
	}
	return true
}

// ============================================================================
// Helpers
// ============================================================================

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
