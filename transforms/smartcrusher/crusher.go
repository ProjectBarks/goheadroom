package smartcrusher

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/uber/goheadroom/transforms/adaptivesizer"
	"github.com/uber/goheadroom/transforms/anchorselector"
	"github.com/uber/goheadroom/transforms/smartcrusher/compaction"
)

// CrushArrayResult is the return type for CrushArray.
type CrushArrayResult struct {
	Items          []json.RawMessage
	StrategyInfo   string
	CCRHash        *string
	DroppedSummary string
	Compacted      *string
	CompactionKind *string
}

// SmartCrusher is the main entry point for JSON array compression.
type SmartCrusher struct {
	Config         SmartCrusherConfig
	AnchorSelector *anchorselector.AnchorSelector
	Analyzer       *SmartAnalyzer
	Constraints    []Constraint
	Observers      []Observer
	Compaction     *compaction.CompactionStage
}

// Crush compresses JSON content. Returns a CrushResult.
// query is the user query for anchor extraction.
// bias controls compression aggressiveness.
func (sc *SmartCrusher) Crush(content string, query string, bias float64) CrushResult {
	start := time.Now()
	compressed, wasModified, info := sc.SmartCrushContent(content, query, bias)
	strategy := "passthrough"
	if info != "" {
		strategy = info
	}

	if len(sc.Observers) > 0 {
		event := &CrushEvent{
			Strategy:    strategy,
			InputBytes:  len(content),
			OutputBytes: len(compressed),
			ElapsedNs:   uint64(time.Since(start).Nanoseconds()),
			WasModified: wasModified,
		}
		for _, obs := range sc.Observers {
			obs.OnEvent(event)
		}
	}

	return CrushResult{
		Compressed:  compressed,
		Original:    content,
		WasModified: wasModified,
		Strategy:    strategy,
	}
}

// SmartCrushContent parses JSON, recursively processes, re-serializes.
// Returns (crushedContent, wasModified, info).
func (sc *SmartCrusher) SmartCrushContent(content string, queryContext string, bias float64) (string, bool, string) {
	var parsed interface{}
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		return content, false, ""
	}

	crushed, info := sc.ProcessValue(parsed, 0, queryContext, bias)

	result, err := marshalOrderedJSON([]byte(content), crushed)
	if err != nil {
		return content, false, ""
	}
	resultStr := string(result)
	wasModified := resultStr != strings.TrimSpace(content)
	return resultStr, wasModified, info
}

const maxProcessDepth = 50

// ProcessValue recursively processes a value, crushing arrays where appropriate.
// Port of Rust process_value.
func (sc *SmartCrusher) ProcessValue(value interface{}, depth int, queryContext string, bias float64) (interface{}, string) {
	if depth >= maxProcessDepth {
		return value, ""
	}

	var infoParts []string

	switch v := value.(type) {
	case []interface{}:
		n := len(v)
		if n >= sc.Config.MinItemsToAnalyze {
			arrType := classifyInterfaceArray(v)
			switch arrType {
			case ArrayDictArray:
				rawItems := interfaceToRawMessages(v)
				result := sc.CrushArray(rawItems, queryContext, bias)
				if result.Compacted != nil {
					infoParts = append(infoParts, fmt.Sprintf("%s(%d->len=%d)", result.StrategyInfo, n, len(*result.Compacted)))
					return *result.Compacted, strings.Join(infoParts, ",")
				}
				infoParts = append(infoParts, fmt.Sprintf("%s(%d->%d)", result.StrategyInfo, n, len(result.Items)))
				return rawMessagesToInterface(result.Items), strings.Join(infoParts, ",")

			case ArrayStringArray:
				strs := make([]string, 0, n)
				for _, item := range v {
					if s, ok := item.(string); ok {
						strs = append(strs, s)
					}
				}
				crushed, strategy := CrushStringArray(strs, &sc.Config, bias)
				infoParts = append(infoParts, fmt.Sprintf("%s(%d->%d)", strategy, n, len(crushed)))
				result := make([]interface{}, len(crushed))
				for i, s := range crushed {
					result[i] = s
				}
				return result, strings.Join(infoParts, ",")

			case ArrayNumberArray:
				rawItems := interfaceToRawMessages(v)
				crushed, strategy := CrushNumberArray(rawItems, &sc.Config, bias)
				infoParts = append(infoParts, fmt.Sprintf("%s(%d->%d)", strategy, n, len(crushed)))
				return rawMessagesToInterface(crushed), strings.Join(infoParts, ",")

			case ArrayMixedArray:
				rawItems := interfaceToRawMessages(v)
				crushed, strategy := sc.CrushMixedArray(rawItems, queryContext, bias)
				infoParts = append(infoParts, fmt.Sprintf("%s(%d->%d)", strategy, n, len(crushed)))
				return rawMessagesToInterface(crushed), strings.Join(infoParts, ",")
			}
		}

		// Below threshold or not crushable: recurse into items.
		processed := make([]interface{}, n)
		for i, item := range v {
			pItem, pInfo := sc.ProcessValue(item, depth+1, queryContext, bias)
			processed[i] = pItem
			if pInfo != "" {
				infoParts = append(infoParts, pInfo)
			}
		}
		return processed, strings.Join(infoParts, ",")

	case map[string]interface{}:
		// First pass: recurse into values.
		processed := make(map[string]interface{})
		for k, val := range v {
			pVal, pInfo := sc.ProcessValue(val, depth+1, queryContext, bias)
			processed[k] = pVal
			if pInfo != "" {
				infoParts = append(infoParts, pInfo)
			}
		}

		// Second pass: if the object itself has many keys, compress at key level.
		if len(processed) >= sc.Config.MinItemsToAnalyze {
			rawObj := interfaceMapToRawMessages(processed)
			crushed, strategy := CrushObject(rawObj, &sc.Config, bias)
			if strategy != "object:passthrough" {
				infoParts = append(infoParts, strategy)
				return rawMessageMapToInterface(crushed), strings.Join(infoParts, ",")
			}
		}

		return processed, strings.Join(infoParts, ",")

	default:
		return value, ""
	}
}

// ExecutePlan executes a CompressionPlan against items.
// Returns the kept-items list in original-array order.
func (sc *SmartCrusher) ExecutePlan(plan *CompressionPlan, items []json.RawMessage) []json.RawMessage {
	indices := make([]int, len(plan.KeepIndices))
	copy(indices, plan.KeepIndices)
	sort.Ints(indices)

	var result []json.RawMessage
	for _, idx := range indices {
		if idx < len(items) {
			result = append(result, items[idx])
		}
	}
	return result
}

// CrushArray compresses an array of dict items.
// Port of Rust crush_array.
func (sc *SmartCrusher) CrushArray(items []json.RawMessage, queryContext string, bias float64) CrushArrayResult {
	itemStrings := make([]string, len(items))
	for i, raw := range items {
		itemStrings[i] = string(raw)
	}

	var maxK *int
	if sc.Config.MaxItemsAfterCrush > 0 {
		v := sc.Config.MaxItemsAfterCrush
		maxK = &v
	}
	adaptiveK := adaptivesizer.ComputeOptimalK(itemStrings, bias, 3, maxK)

	// Tier-1 boundary: array already small enough -- passthrough,
	// nothing to compact, nothing to drop.
	if len(items) <= adaptiveK {
		result := make([]json.RawMessage, len(items))
		copy(result, items)
		return CrushArrayResult{
			Items:        result,
			StrategyInfo: "none:adaptive_at_limit",
		}
	}

	// Lossless-first: try tabular compaction before lossy selection.
	if sc.Compaction != nil && len(items) >= sc.Config.MinItemsToAnalyze {
		parsed := make([]interface{}, len(items))
		for i, raw := range items {
			var v interface{}
			json.Unmarshal(raw, &v)
			parsed[i] = v
		}
		c, rendered := sc.Compaction.Run(parsed)
		if c.WasCompacted() {
			inputBytes := estimateArrayBytes(itemStrings)
			savingsRatio := 0.0
			if inputBytes > 0 {
				savingsRatio = 1.0 - float64(len(rendered))/float64(inputBytes)
			}
			if savingsRatio >= sc.Config.LosslessMinSavingsRatio {
				kind := compactionKindStr(c)
				result := make([]json.RawMessage, len(items))
				copy(result, items)
				return CrushArrayResult{
					Items:        result,
					StrategyInfo: fmt.Sprintf("lossless:%s", kind),
					Compacted:    &rendered,
					CompactionKind: &kind,
				}
			}
		}
	}

	// Lossy path.
	effectiveMaxItems := adaptiveK
	analysis := sc.Analyzer.AnalyzeArray(items)

	// Crushability gate.
	if analysis.RecommendedStrategy == StrategySkip {
		reason := ""
		if analysis.Crushability != nil {
			reason = fmt.Sprintf("skip:%s", analysis.Crushability.Reason)
		}
		result := make([]json.RawMessage, len(items))
		copy(result, items)
		return CrushArrayResult{
			Items:        result,
			StrategyInfo: reason,
		}
	}

	planner := NewSmartCrusherPlanner(&sc.Config, sc.AnchorSelector, sc.Analyzer, sc.Constraints)
	plan := planner.CreatePlan(analysis, items, queryContext, nil, &effectiveMaxItems, itemStrings)
	kept := sc.ExecutePlan(&plan, items)

	strategyInfo := analysis.RecommendedStrategy.String()

	return CrushArrayResult{
		Items:        kept,
		StrategyInfo: strategyInfo,
	}
}

// CrushMixedArray compresses a mixed-type array.
// Port of Rust crush_mixed_array.
func (sc *SmartCrusher) CrushMixedArray(items []json.RawMessage, queryContext string, bias float64) ([]json.RawMessage, string) {
	n := len(items)
	if n <= 8 {
		result := make([]json.RawMessage, n)
		copy(result, items)
		return result, "mixed:passthrough"
	}

	// Group by type.
	type group struct {
		key     string
		indices []int
		values  []json.RawMessage
	}
	var groups []group
	groupIndex := make(map[string]int)

	for i, raw := range items {
		key := groupKey(raw)
		if idx, ok := groupIndex[key]; ok {
			groups[idx].indices = append(groups[idx].indices, i)
			groups[idx].values = append(groups[idx].values, raw)
		} else {
			groupIndex[key] = len(groups)
			groups = append(groups, group{key: key, indices: []int{i}, values: []json.RawMessage{raw}})
		}
	}

	keepIndices := make(map[int]bool)
	var strategyParts []string

	for _, g := range groups {
		if len(g.values) < sc.Config.MinItemsToAnalyze {
			for _, idx := range g.indices {
				keepIndices[idx] = true
			}
			continue
		}

		switch g.key {
		case "dict":
			result := sc.CrushArray(g.values, queryContext, bias)
			crushedKeys := make(map[string]bool)
			for _, raw := range result.Items {
				crushedKeys[string(raw)] = true
			}
			for i, idx := range g.indices {
				if crushedKeys[string(g.values[i])] {
					keepIndices[idx] = true
				}
			}
			strategyParts = append(strategyParts, fmt.Sprintf("dict:%d->%d", len(g.values), len(result.Items)))

		case "str":
			strs := make([]string, 0, len(g.values))
			for _, raw := range g.values {
				var s string
				if err := json.Unmarshal(raw, &s); err == nil {
					strs = append(strs, s)
				}
			}
			crushed, _ := CrushStringArray(strs, &sc.Config, bias)
			crushedSet := make(map[string]bool)
			for _, s := range crushed {
				crushedSet[s] = true
			}
			for i, idx := range g.indices {
				var s string
				if err := json.Unmarshal(g.values[i], &s); err == nil && crushedSet[s] {
					keepIndices[idx] = true
				}
			}
			strategyParts = append(strategyParts, fmt.Sprintf("str:%d->%d", len(g.values), len(crushed)))

		case "number":
			strItems := make([]string, len(g.values))
			for i, raw := range g.values {
				strItems[i] = string(raw)
			}
			_, kFirst, kLast, _ := ComputeKSplit(strItems, &sc.Config, bias)
			kFirst = min(kFirst, len(g.values))
			kLast = min(kLast, len(g.values)-kFirst)
			for i := 0; i < kFirst; i++ {
				keepIndices[g.indices[i]] = true
			}
			for i := len(g.indices) - kLast; i < len(g.indices); i++ {
				if i >= 0 {
					keepIndices[g.indices[i]] = true
				}
			}

			// Outliers via finite-only stats (matches Rust crush_mixed_array).
			var finite []float64
			for _, raw := range g.values {
				var num float64
				if err := json.Unmarshal(raw, &num); err == nil && !math.IsInf(num, 0) && !math.IsNaN(num) {
					finite = append(finite, num)
				}
			}
			if len(finite) > 1 {
				if meanV, ok := Mean(finite); ok {
					if stdV, ok := SampleStdev(finite); ok && stdV > 0 {
						threshold := sc.Config.VarianceThreshold * stdV
						for i, raw := range g.values {
							var num float64
							if err := json.Unmarshal(raw, &num); err == nil && !math.IsInf(num, 0) && !math.IsNaN(num) {
								if math.Abs(num-meanV) > threshold {
									keepIndices[g.indices[i]] = true
								}
							}
						}
					}
				}
			}
			strategyParts = append(strategyParts, fmt.Sprintf("num:%d", len(g.values)))

		default:
			// list / bool / none / other: keep all items.
			for _, idx := range g.indices {
				keepIndices[idx] = true
			}
		}
	}

	// Reassemble in original order.
	sorted := setToSortedSlice(keepIndices)
	result := make([]json.RawMessage, len(sorted))
	for i, idx := range sorted {
		result[i] = items[idx]
	}

	strategy := fmt.Sprintf("mixed:adaptive(%d->%d,%s)", n, len(result), strings.Join(strategyParts, ","))
	return result, strategy
}

// --- helpers ---

func classifyInterfaceArray(items []interface{}) ArrayType {
	if len(items) == 0 {
		return ArrayEmpty
	}

	// Track which types we've seen -- mirrors Rust classify_array which
	// requires pure (single-type) arrays for non-mixed classification.
	hasBool := false
	hasNumber := false
	hasString := false
	hasObject := false
	hasArray := false
	hasNull := false

	for _, item := range items {
		switch item.(type) {
		case bool:
			hasBool = true
		case float64, json.Number:
			hasNumber = true
		case string:
			hasString = true
		case map[string]interface{}:
			hasObject = true
		case []interface{}:
			hasArray = true
		case nil:
			hasNull = true
		}
	}

	// Pure dict array.
	if hasObject && !hasBool && !hasNumber && !hasString && !hasArray && !hasNull {
		return ArrayDictArray
	}
	// Pure string array.
	if hasString && !hasBool && !hasNumber && !hasObject && !hasArray && !hasNull {
		return ArrayStringArray
	}
	// Pure number array (excludes bools).
	if hasNumber && !hasBool && !hasString && !hasObject && !hasArray && !hasNull {
		return ArrayNumberArray
	}
	// Pure bool array.
	if hasBool && !hasNumber && !hasString && !hasObject && !hasArray && !hasNull {
		// BoolArray not currently handled separately; treat as mixed.
		return ArrayMixedArray
	}
	// Check if anything was detected at all.
	if !hasBool && !hasNumber && !hasString && !hasObject && !hasArray && !hasNull {
		return ArrayEmpty
	}
	return ArrayMixedArray
}

func interfaceToRawMessages(items []interface{}) []json.RawMessage {
	result := make([]json.RawMessage, len(items))
	for i, item := range items {
		data, err := json.Marshal(item)
		if err != nil {
			result[i] = json.RawMessage("null")
		} else {
			result[i] = data
		}
	}
	return result
}

func rawMessagesToInterface(items []json.RawMessage) []interface{} {
	result := make([]interface{}, len(items))
	for i, raw := range items {
		var v interface{}
		_ = json.Unmarshal(raw, &v)
		result[i] = v
	}
	return result
}

func interfaceMapToRawMessages(m map[string]interface{}) map[string]json.RawMessage {
	result := make(map[string]json.RawMessage, len(m))
	for k, v := range m {
		data, err := json.Marshal(v)
		if err != nil {
			result[k] = json.RawMessage("null")
		} else {
			result[k] = data
		}
	}
	return result
}

func rawMessageMapToInterface(m map[string]json.RawMessage) map[string]interface{} {
	result := make(map[string]interface{}, len(m))
	for k, v := range m {
		var val interface{}
		_ = json.Unmarshal(v, &val)
		result[k] = val
	}
	return result
}

func groupKey(item json.RawMessage) string {
	trimmed := strings.TrimSpace(string(item))
	if len(trimmed) == 0 {
		return "none"
	}
	switch trimmed[0] {
	case '{':
		return "dict"
	case '"':
		return "str"
	case '[':
		return "list"
	case 't', 'f':
		return "bool"
	case 'n':
		return "none"
	default:
		if trimmed[0] == '-' || (trimmed[0] >= '0' && trimmed[0] <= '9') {
			return "number"
		}
		return "other"
	}
}

func estimateArrayBytes(itemStrings []string) int {
	total := 2 // [ and ]
	for i, s := range itemStrings {
		if i > 0 {
			total += 2 // ", "
		}
		total += len(s)
	}
	return total
}

func compactionKindStr(c *compaction.Compaction) string {
	switch c.Kind {
	case compaction.CompactionTable:
		return "csv_schema"
	case compaction.CompactionBuckets:
		return "buckets"
	case compaction.CompactionOpaqueRef:
		return "opaque_ref"
	default:
		return "unknown"
	}
}
