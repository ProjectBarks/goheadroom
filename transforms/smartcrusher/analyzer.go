package smartcrusher

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/uber/goheadroom/internal/textutil"
)

// SmartAnalyzer is the statistical brain that decides whether and how to crush.
type SmartAnalyzer struct {
	Config SmartCrusherConfig
}

// NewSmartAnalyzer creates a new analyzer with the given config.
func NewSmartAnalyzer(config SmartCrusherConfig) *SmartAnalyzer {
	return &SmartAnalyzer{Config: config}
}

// AnalyzeArray is the top-level analysis entry point.
func (a *SmartAnalyzer) AnalyzeArray(items []json.RawMessage) *ArrayAnalysis {
	// Empty / non-dict-first guard.
	firstIsDict := false
	if len(items) > 0 {
		trimmed := strings.TrimSpace(string(items[0]))
		if len(trimmed) > 0 && trimmed[0] == '{' {
			firstIsDict = true
		}
	}

	if !firstIsDict {
		return &ArrayAnalysis{
			ItemCount:           len(items),
			FieldStats:          make(map[string]*FieldStats),
			DetectedPattern:     "generic",
			RecommendedStrategy: StrategyNone,
			ConstantFields:      make(map[string]interface{}),
			EstimatedReduction:  0.0,
		}
	}

	// Union of all keys across dict items, sorted for determinism.
	allKeys := make(map[string]bool)
	for _, raw := range items {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(raw, &obj); err == nil {
			for k := range obj {
				allKeys[k] = true
			}
		}
	}
	sortedKeys := make([]string, 0, len(allKeys))
	for k := range allKeys {
		sortedKeys = append(sortedKeys, k)
	}
	sort.Strings(sortedKeys)

	fieldStats := make(map[string]*FieldStats)
	for _, key := range sortedKeys {
		fs := a.AnalyzeField(key, items)
		fieldStats[key] = fs
	}

	pattern := a.DetectPattern(fieldStats, items)

	// Constant fields.
	constantFields := make(map[string]interface{})
	for k, fs := range fieldStats {
		if fs.IsConstant && fs.ConstantValue != nil {
			constantFields[k] = fs.ConstantValue
		}
	}

	crushability := a.AnalyzeCrushability(items, fieldStats)

	strategy := a.SelectStrategy(fieldStats, pattern, len(items), crushability)

	var reduction float64
	if strategy == StrategySkip {
		reduction = 0.0
	} else {
		reduction = a.EstimateReduction(fieldStats, strategy, len(items))
	}

	return &ArrayAnalysis{
		ItemCount:           len(items),
		FieldStats:          fieldStats,
		DetectedPattern:     pattern,
		RecommendedStrategy: strategy,
		ConstantFields:      constantFields,
		EstimatedReduction:  reduction,
		Crushability:        crushability,
	}
}

// AnalyzeField computes per-field statistics.
func (a *SmartAnalyzer) AnalyzeField(key string, items []json.RawMessage) *FieldStats {
	// Collect raw values.
	type decodedVal struct {
		raw     json.RawMessage
		decoded interface{}
		isNull  bool
	}

	var values []decodedVal
	for _, raw := range items {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(raw, &obj); err != nil {
			continue
		}
		fieldRaw, exists := obj[key]
		if !exists {
			values = append(values, decodedVal{isNull: true})
			continue
		}
		trimmed := strings.TrimSpace(string(fieldRaw))
		if trimmed == "null" {
			values = append(values, decodedVal{raw: fieldRaw, isNull: true})
			continue
		}
		var decoded interface{}
		json.Unmarshal(fieldRaw, &decoded)
		values = append(values, decodedVal{raw: fieldRaw, decoded: decoded, isNull: false})
	}

	var nonNull []decodedVal
	for _, v := range values {
		if !v.isNull {
			nonNull = append(nonNull, v)
		}
	}

	if len(nonNull) == 0 {
		return &FieldStats{
			Name:        key,
			FieldType:   "null",
			Count:       len(values),
			UniqueCount: 0,
			UniqueRatio: 0.0,
			IsConstant:  true,
		}
	}

	// Determine field type from first non-null value.
	first := nonNull[0].decoded
	fieldType := "unknown"
	switch first.(type) {
	case bool:
		fieldType = "boolean"
	case float64:
		fieldType = "numeric"
	case string:
		fieldType = "string"
	case map[string]interface{}:
		fieldType = "object"
	case []interface{}:
		fieldType = "array"
	}

	// Uniqueness.
	strValues := make([]string, len(values))
	for i, v := range values {
		strValues[i] = pythonRepr(v.decoded, v.isNull)
	}
	uniqueSet := make(map[string]bool)
	for _, s := range strValues {
		uniqueSet[s] = true
	}
	uniqueCount := len(uniqueSet)
	uniqueRatio := 0.0
	if len(values) > 0 {
		uniqueRatio = float64(uniqueCount) / float64(len(values))
	}

	isConstant := uniqueCount == 1
	var constantValue interface{}
	if isConstant {
		constantValue = nonNull[0].decoded
	}

	stats := &FieldStats{
		Name:          key,
		FieldType:     fieldType,
		Count:         len(values),
		UniqueCount:   uniqueCount,
		UniqueRatio:   uniqueRatio,
		IsConstant:    isConstant,
		ConstantValue: constantValue,
	}

	switch fieldType {
	case "numeric":
		var nums []float64
		for _, v := range nonNull {
			if f, ok := v.decoded.(float64); ok && !math.IsInf(f, 0) && !math.IsNaN(f) {
				nums = append(nums, f)
			}
		}
		if len(nums) > 0 {
			minVal := nums[0]
			maxVal := nums[0]
			for _, n := range nums[1:] {
				if n < minVal {
					minVal = n
				}
				if n > maxVal {
					maxVal = n
				}
			}
			meanVal, meanOK := Mean(nums)
			var variance float64
			var varianceOK bool
			if len(nums) > 1 {
				variance, varianceOK = SampleVariance(nums)
			} else {
				variance = 0.0
				varianceOK = true
			}

			allFinite := meanOK && varianceOK &&
				!math.IsInf(minVal, 0) && !math.IsNaN(minVal) &&
				!math.IsInf(maxVal, 0) && !math.IsNaN(maxVal)

			if allFinite {
				stats.MinVal = &minVal
				stats.MaxVal = &maxVal
				stats.MeanVal = &meanVal
				stats.Variance = &variance
				stats.ChangePoints = a.DetectChangePoints(nums, 5)
			} else {
				zero := 0.0
				stats.Variance = &zero
			}
		}

	case "string":
		var strs []string
		for _, v := range nonNull {
			if s, ok := v.decoded.(string); ok {
				strs = append(strs, s)
			}
		}
		if len(strs) > 0 {
			lens := make([]float64, len(strs))
			for i, s := range strs {
				lens[i] = float64(len([]rune(s)))
			}
			if avgLen, ok := Mean(lens); ok {
				stats.AvgLength = &avgLen
			}
			stats.TopValues = topNByCount(strs, 5)
		}
	}

	return stats
}

// DetectChangePoints implements sliding-window change-point detection.
func (a *SmartAnalyzer) DetectChangePoints(values []float64, window int) []int {
	if len(values) < window*2 {
		return nil
	}

	overallStd, ok := SampleStdev(values)
	if !ok || overallStd <= 0.0 {
		return nil
	}

	threshold := a.Config.VarianceThreshold * overallStd

	var changePoints []int
	end := len(values) - window
	for i := window; i < end; i++ {
		before, _ := Mean(values[i-window : i])
		after, _ := Mean(values[i : i+window])
		if math.Abs(after-before) > threshold {
			changePoints = append(changePoints, i)
		}
	}

	if len(changePoints) == 0 {
		return nil
	}

	// Greedy dedup.
	deduped := []int{changePoints[0]}
	for _, cp := range changePoints[1:] {
		last := deduped[len(deduped)-1]
		if cp-last > window {
			deduped = append(deduped, cp)
		}
	}
	return deduped
}

// DetectPattern classifies the array pattern.
func (a *SmartAnalyzer) DetectPattern(fieldStats map[string]*FieldStats, items []json.RawMessage) string {
	hasTimestamp := a.DetectTemporalField(fieldStats, items)

	hasNumericWithVariance := false
	for _, fs := range fieldStats {
		if fs.FieldType == "numeric" {
			v := 0.0
			if fs.Variance != nil {
				v = *fs.Variance
			}
			if v > 0 {
				hasNumericWithVariance = true
				break
			}
		}
	}

	if hasTimestamp && hasNumericWithVariance {
		return "time_series"
	}

	// Logs pattern.
	hasMessageLike := false
	hasLevelLike := false
	// Sort keys for determinism.
	sortedKeys := sortFieldKeys(fieldStats)
	for _, k := range sortedKeys {
		fs := fieldStats[k]
		if fs.FieldType != "string" {
			continue
		}
		avgLen := 0.0
		if fs.AvgLength != nil {
			avgLen = *fs.AvgLength
		}
		if fs.UniqueRatio > 0.5 && avgLen > 20.0 {
			hasMessageLike = true
		} else if fs.UniqueRatio < 0.1 && fs.UniqueCount >= 2 && fs.UniqueCount <= 10 {
			hasLevelLike = true
		}
	}
	if hasMessageLike && hasLevelLike {
		return "logs"
	}

	// Search results: any field with score-like signal at confidence >= 0.5.
	for _, k := range sortedKeys {
		fs := fieldStats[k]
		isScore, confidence := DetectScoreFieldStatistically(fs, items)
		if isScore && confidence >= 0.5 {
			return "search_results"
		}
	}

	return "generic"
}

// DetectTemporalField detects temporal fields.
func (a *SmartAnalyzer) DetectTemporalField(fieldStats map[string]*FieldStats, items []json.RawMessage) bool {
	sortedKeys := sortFieldKeys(fieldStats)
	for _, name := range sortedKeys {
		fs := fieldStats[name]
		switch fs.FieldType {
		case "string":
			// Sample first 10 items.
			var sample []string
			count := 0
			for _, raw := range items {
				if count >= 10 {
					break
				}
				var obj map[string]json.RawMessage
				if err := json.Unmarshal(raw, &obj); err == nil {
					if fieldRaw, ok := obj[name]; ok {
						var s string
						if err := json.Unmarshal(fieldRaw, &s); err == nil {
							sample = append(sample, s)
						}
					}
				}
				count++
			}
			if len(sample) == 0 {
				continue
			}
			isoCount := 0
			for _, s := range sample {
				if isISODatetime(s) || isISODate(s) {
					isoCount++
				}
			}
			if float64(isoCount)/float64(len(sample)) > 0.5 {
				return true
			}

		case "numeric":
			if fs.MinVal != nil && fs.MaxVal != nil {
				mn := *fs.MinVal
				unixSeconds := mn >= 1_000_000_000 && mn <= 2_000_000_000
				unixMillis := mn >= 1_000_000_000_000 && mn <= 2_000_000_000_000
				if unixSeconds || unixMillis {
					return true
				}
			}
		}
	}
	return false
}

// AnalyzeCrushability decides whether an array is safe to crush.
func (a *SmartAnalyzer) AnalyzeCrushability(items []json.RawMessage, fieldStats map[string]*FieldStats) *CrushabilityAnalysis {
	var signalsPresent []string
	var signalsAbsent []string

	sortedKeys := sortFieldKeys(fieldStats)

	// 1. ID field detection.
	var idFieldName string
	var idUniqueness float64
	var idConfidence float64
	for _, name := range sortedKeys {
		fs := fieldStats[name]
		// Collect field values.
		var values []json.RawMessage
		for _, raw := range items {
			var obj map[string]json.RawMessage
			if err := json.Unmarshal(raw, &obj); err == nil {
				if v, ok := obj[name]; ok {
					values = append(values, v)
				} else {
					values = append(values, []byte("null"))
				}
			}
		}
		isID, confidence := DetectIDFieldStatistically(fs, values)
		if isID && confidence > idConfidence {
			idFieldName = name
			idUniqueness = fs.UniqueRatio
			idConfidence = confidence
		}
	}
	hasIDField := idFieldName != "" && idConfidence >= 0.7

	// 2. Score field detection.
	hasScoreField := false
	for _, name := range sortedKeys {
		fs := fieldStats[name]
		isScore, confidence := DetectScoreFieldStatistically(fs, items)
		if isScore {
			hasScoreField = true
			signalsPresent = append(signalsPresent, fmt.Sprintf("score_field:%s(conf=%.2f)", name, confidence))
			break
		}
	}
	if !hasScoreField {
		signalsAbsent = append(signalsAbsent, "score_field")
	}

	// 3. Structural outliers.
	outlierIndices := DetectStructuralOutliers(items)
	structuralOutlierCount := len(outlierIndices)
	if structuralOutlierCount > 0 {
		signalsPresent = append(signalsPresent, fmt.Sprintf("structural_outliers:%d", structuralOutlierCount))
	} else {
		signalsAbsent = append(signalsAbsent, "structural_outliers")
	}

	// 3b. Error keyword fallback.
	errorKeywordIndices := DetectErrorItemsForPreservation(items, nil)
	keywordErrorCount := len(errorKeywordIndices)
	if keywordErrorCount > 0 && structuralOutlierCount == 0 {
		signalsPresent = append(signalsPresent, fmt.Sprintf("error_keywords:%d", keywordErrorCount))
	}

	errorCount := structuralOutlierCount
	if keywordErrorCount > errorCount {
		errorCount = keywordErrorCount
	}

	// 4. Numeric anomalies.
	anomalySet := make(map[int]bool)
	for _, name := range sortedKeys {
		fs := fieldStats[name]
		if fs.FieldType != "numeric" {
			continue
		}
		if fs.MeanVal == nil || fs.Variance == nil {
			continue
		}
		meanVal := *fs.MeanVal
		variance := *fs.Variance
		if variance <= 0 {
			continue
		}
		std := math.Sqrt(variance)
		if std <= 0 {
			continue
		}
		threshold := a.Config.VarianceThreshold * std
		for i, raw := range items {
			var obj map[string]json.RawMessage
			if err := json.Unmarshal(raw, &obj); err != nil {
				continue
			}
			fieldRaw, ok := obj[name]
			if !ok {
				continue
			}
			var num float64
			if err := json.Unmarshal(fieldRaw, &num); err == nil && !math.IsNaN(num) {
				if math.Abs(num-meanVal) > threshold {
					anomalySet[i] = true
				}
			}
		}
	}
	anomalyCount := len(anomalySet)
	if anomalyCount > 0 {
		signalsPresent = append(signalsPresent, fmt.Sprintf("anomalies:%d", anomalyCount))
	} else {
		signalsAbsent = append(signalsAbsent, "anomalies")
	}

	// 5. Average string uniqueness, excluding detected ID field.
	var stringRatios []float64
	for _, name := range sortedKeys {
		fs := fieldStats[name]
		if fs.FieldType == "string" && name != idFieldName {
			stringRatios = append(stringRatios, fs.UniqueRatio)
		}
	}
	avgStringUniqueness := 0.0
	if len(stringRatios) > 0 {
		avgStringUniqueness, _ = Mean(stringRatios)
	}

	var nonIDNumericRatios []float64
	for _, name := range sortedKeys {
		fs := fieldStats[name]
		if fs.FieldType == "numeric" && name != idFieldName {
			nonIDNumericRatios = append(nonIDNumericRatios, fs.UniqueRatio)
		}
	}
	avgNonIDNumericUniqueness := 0.0
	if len(nonIDNumericRatios) > 0 {
		avgNonIDNumericUniqueness, _ = Mean(nonIDNumericRatios)
	}

	maxUniqueness := math.Max(avgStringUniqueness, math.Max(idUniqueness, 0.0))
	nonIDContentUniqueness := math.Max(avgStringUniqueness, avgNonIDNumericUniqueness)

	// 6. Change points.
	hasChangePoints := false
	for _, name := range sortedKeys {
		fs := fieldStats[name]
		if fs.FieldType == "numeric" && len(fs.ChangePoints) > 0 {
			hasChangePoints = true
			break
		}
	}
	if hasChangePoints {
		signalsPresent = append(signalsPresent, "change_points")
	}

	hasAnySignal := len(signalsPresent) > 0

	make_ := func(crushable bool, confidence float64, reason string, sp, sa []string) *CrushabilityAnalysis {
		return &CrushabilityAnalysis{
			Crushable:           crushable,
			Confidence:          confidence,
			Reason:              reason,
			SignalsPresent:      sp,
			SignalsAbsent:       sa,
			HasIDField:          hasIDField,
			IDUniqueness:        idUniqueness,
			AvgStringUniqueness: avgStringUniqueness,
			HasScoreField:       hasScoreField,
			ErrorItemCount:      errorCount,
			AnomalyCount:        anomalyCount,
		}
	}

	// Case 0: repetitive content with unique IDs.
	if nonIDContentUniqueness < 0.1 && hasIDField {
		sp := append(append([]string{}, signalsPresent...), "repetitive_content")
		return make_(true, 0.85, "repetitive_content_with_ids", sp, signalsAbsent)
	}

	// Case 1: low uniqueness.
	if maxUniqueness < 0.3 {
		return make_(true, 0.9, "low_uniqueness_safe_to_sample", signalsPresent, signalsAbsent)
	}

	// Case 2: high uniqueness + ID field + NO signal.
	if hasIDField && maxUniqueness > 0.8 && !hasAnySignal {
		return make_(false, 0.85, "unique_entities_no_signal", signalsPresent, signalsAbsent)
	}

	// Case 3: high uniqueness + has signal.
	if maxUniqueness > 0.8 && hasAnySignal {
		return make_(true, 0.7, "unique_entities_with_signal", signalsPresent, signalsAbsent)
	}

	// Case 4: medium uniqueness + no signal.
	if !hasAnySignal {
		return make_(false, 0.6, "medium_uniqueness_no_signal", signalsPresent, signalsAbsent)
	}

	// Case 5: medium uniqueness + has signal.
	return make_(true, 0.5, "medium_uniqueness_with_signal", signalsPresent, signalsAbsent)
}

// SelectStrategy picks the compression strategy.
func (a *SmartAnalyzer) SelectStrategy(fieldStats map[string]*FieldStats, pattern string, itemCount int, crushability *CrushabilityAnalysis) CompressionStrategy {
	if itemCount < a.Config.MinItemsToAnalyze {
		return StrategyNone
	}

	if crushability != nil && !crushability.Crushable {
		return StrategySkip
	}

	if pattern == "time_series" {
		for _, fs := range fieldStats {
			if fs.FieldType == "numeric" && len(fs.ChangePoints) > 0 {
				return StrategyTimeSeries
			}
		}
	}

	if pattern == "logs" {
		for _, k := range sortFieldKeys(fieldStats) {
			if strings.Contains(strings.ToLower(k), "message") {
				mf := fieldStats[k]
				if mf.UniqueRatio < 0.5 {
					return StrategyClusterSample
				}
			}
		}
	}

	if pattern == "search_results" {
		return StrategyTopN
	}

	return StrategySmartSample
}

// EstimateReduction estimates the compression ratio.
func (a *SmartAnalyzer) EstimateReduction(fieldStats map[string]*FieldStats, strategy CompressionStrategy, itemCount int) float64 {
	if strategy == StrategyNone {
		return 0.0
	}
	if len(fieldStats) == 0 {
		return 0.0
	}

	constantCount := 0
	for _, fs := range fieldStats {
		if fs.IsConstant {
			constantCount++
		}
	}
	constantRatio := float64(constantCount) / float64(len(fieldStats))

	var base float64
	switch strategy {
	case StrategyTimeSeries:
		base = 0.7
	case StrategyClusterSample:
		base = 0.8
	case StrategyTopN:
		base = 0.6
	case StrategySmartSample:
		base = 0.5
	default:
		base = 0.3
	}

	return math.Min(base+constantRatio*0.2, 0.95)
}

// helpers

func sortFieldKeys(fieldStats map[string]*FieldStats) []string {
	keys := make([]string, 0, len(fieldStats))
	for k := range fieldStats {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func pythonRepr(v interface{}, isNull bool) string {
	if isNull || v == nil {
		return "None"
	}
	switch val := v.(type) {
	case bool:
		if val {
			return "True"
		}
		return "False"
	case float64:
		b, _ := json.Marshal(val)
		return string(b)
	case string:
		return val
	default:
		b, _ := json.Marshal(val)
		return string(b)
	}
}

func topNByCount(strs []string, n int) []TopValue {
	order := make([]string, 0)
	counts := make(map[string]int)
	for _, s := range strs {
		if _, exists := counts[s]; !exists {
			order = append(order, s)
		}
		counts[s]++
	}

	// Stable sort by count desc.
	sort.SliceStable(order, func(i, j int) bool {
		return counts[order[i]] > counts[order[j]]
	})

	if len(order) > n {
		order = order[:n]
	}

	result := make([]TopValue, len(order))
	for i, k := range order {
		result[i] = TopValue{Value: k, Count: counts[k]}
	}
	return result
}

func isISODatetime(s string) bool {
	b := []byte(s)
	if len(b) < 19 {
		return false
	}
	return textutil.IsDigit(b[0]) && textutil.IsDigit(b[1]) && textutil.IsDigit(b[2]) && textutil.IsDigit(b[3]) &&
		b[4] == '-' &&
		textutil.IsDigit(b[5]) && textutil.IsDigit(b[6]) &&
		b[7] == '-' &&
		textutil.IsDigit(b[8]) && textutil.IsDigit(b[9]) &&
		(b[10] == 'T' || b[10] == ' ') &&
		textutil.IsDigit(b[11]) && textutil.IsDigit(b[12]) &&
		b[13] == ':' &&
		textutil.IsDigit(b[14]) && textutil.IsDigit(b[15]) &&
		b[16] == ':' &&
		textutil.IsDigit(b[17]) && textutil.IsDigit(b[18])
}

func isISODate(s string) bool {
	b := []byte(s)
	if len(b) != 10 {
		return false
	}
	return textutil.IsDigit(b[0]) && textutil.IsDigit(b[1]) && textutil.IsDigit(b[2]) && textutil.IsDigit(b[3]) &&
		b[4] == '-' &&
		textutil.IsDigit(b[5]) && textutil.IsDigit(b[6]) &&
		b[7] == '-' &&
		textutil.IsDigit(b[8]) && textutil.IsDigit(b[9])
}
