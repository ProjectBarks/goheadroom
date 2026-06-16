// Package logcompressor compresses build and test output (pytest, npm,
// cargo, jest, make, generic) by detecting format, classifying lines,
// scoring, selecting important lines, and deduplicating.
//
// Port of headroom-core/src/transforms/log_compressor.rs.
package logcompressor

import (
	"crypto/md5"
	"encoding/hex"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/uber/goheadroom/ccr"
	"github.com/uber/goheadroom/internal/textutil"
	"github.com/uber/goheadroom/transforms/adaptivesizer"
)

// ── Log format enum ─────────────────────────────────────────────────

// LogFormat represents the detected build/test log format.
type LogFormat int

const (
	LogFormatPytest  LogFormat = iota
	LogFormatNpm     LogFormat = iota
	LogFormatCargo   LogFormat = iota
	LogFormatJest    LogFormat = iota
	LogFormatMake    LogFormat = iota
	LogFormatGeneric LogFormat = iota
)

func (f LogFormat) String() string {
	switch f {
	case LogFormatPytest:
		return "pytest"
	case LogFormatNpm:
		return "npm"
	case LogFormatCargo:
		return "cargo"
	case LogFormatJest:
		return "jest"
	case LogFormatMake:
		return "make"
	default:
		return "generic"
	}
}

// ── Log level enum ──────────────────────────────────────────────────

// LogLevel represents per-line log severity.
type LogLevel int

const (
	LogLevelTrace   LogLevel = iota // 0 (lowest)
	LogLevelDebug   LogLevel = iota // 1
	LogLevelInfo    LogLevel = iota // 2
	LogLevelUnknown LogLevel = iota // 3
	LogLevelWarn    LogLevel = iota // 4
	LogLevelFail    LogLevel = iota // 5
	LogLevelError   LogLevel = iota // 6 (highest)
)

func (l LogLevel) String() string {
	switch l {
	case LogLevelError:
		return "error"
	case LogLevelFail:
		return "fail"
	case LogLevelWarn:
		return "warn"
	case LogLevelInfo:
		return "info"
	case LogLevelDebug:
		return "debug"
	case LogLevelTrace:
		return "trace"
	default:
		return "unknown"
	}
}

// ── Log line ────────────────────────────────────────────────────────

// LogLine is one classified log line.
type LogLine struct {
	LineNumber   int
	Content      string
	Level        LogLevel
	IsStackTrace bool
	IsSummary    bool
	Score        float32
}

// ── Config ──────────────────────────────────────────────────────────

// LogCompressorConfig mirrors Rust LogCompressorConfig.
type LogCompressorConfig struct {
	MaxErrors               int
	ErrorContextLines       int
	KeepFirstError          bool
	KeepLastError           bool
	MaxStackTraces          int
	StackTraceMaxLines      int
	MaxWarnings             int
	DedupeWarnings          bool
	KeepSummaryLines        bool
	MaxTotalLines           int
	EnableCCR               bool
	MinLinesForCCR          int
	MinCompressionRatioForCCR float64
}

// DefaultConfig returns defaults matching Rust/Python.
func DefaultConfig() LogCompressorConfig {
	return LogCompressorConfig{
		MaxErrors:               10,
		ErrorContextLines:       3,
		KeepFirstError:          true,
		KeepLastError:           true,
		MaxStackTraces:          3,
		StackTraceMaxLines:      20,
		MaxWarnings:             5,
		DedupeWarnings:          true,
		KeepSummaryLines:        true,
		MaxTotalLines:           100,
		EnableCCR:               true,
		MinLinesForCCR:          50,
		MinCompressionRatioForCCR: 0.5,
	}
}

// ── Result types ────────────────────────────────────────────────────

// LogCompressionResult is the parity-equal result.
type LogCompressionResult struct {
	Compressed          string
	Original            string
	OriginalLineCount   int
	CompressedLineCount int
	FormatDetected      LogFormat
	CompressionRatio    float64
	CacheKey            *string
	Stats               map[string]int64
}

// LogCompressorStats carries sidecar diagnostics.
type LogCompressorStats struct {
	Format                   *LogFormat
	StackTracesSeen          int
	StackTracesKept          int
	WarningsDroppedByDedupe  int
	LinesDroppedByGlobalCap  int
	CCREmitted               bool
	CCRSkipReason            *string
}

// ── Compressor ──────────────────────────────────────────────────────

// LogCompressor compresses build/test log output.
type LogCompressor struct {
	config LogCompressorConfig
}

// New creates a LogCompressor with the given config.
func New(config LogCompressorConfig) *LogCompressor {
	return &LogCompressor{config: config}
}

// Compress compresses content without a CCR store, matching Rust's
// compress() which passes None. No CCR footer is emitted.
func (lc *LogCompressor) Compress(content string, bias float64) (LogCompressionResult, LogCompressorStats) {
	return lc.CompressWithStore(content, bias, nil)
}

// CompressWithStore compresses with optional CCR persistence.
func (lc *LogCompressor) CompressWithStore(content string, bias float64, store ccr.CcrStore) (LogCompressionResult, LogCompressorStats) {
	var stats LogCompressorStats
	allLines := splitLines(content)
	originalLineCount := len(allLines)

	if originalLineCount < lc.config.MinLinesForCCR {
		return LogCompressionResult{
			Compressed:          content,
			Original:            content,
			OriginalLineCount:   originalLineCount,
			CompressedLineCount: originalLineCount,
			FormatDetected:      LogFormatGeneric,
			CompressionRatio:    1.0,
			Stats:               make(map[string]int64),
		}, stats
	}

	format := detectFormatLines(allLines)
	stats.Format = &format

	logLines := lc.parseLinesFromStrings(allLines)
	selected := lc.selectLines(logLines, allLines, bias, &stats)
	compressedBody, outputStats := lc.formatOutput(selected, logLines)
	compressed := compressedBody

	var maxLen int
	if len(content) > 0 {
		maxLen = len(content)
	} else {
		maxLen = 1
	}
	ratio := float64(len(compressed)) / float64(maxLen)

	var cacheKey *string
	if lc.config.EnableCCR {
		if ratio >= lc.config.MinCompressionRatioForCCR {
			reason := "compression ratio too high"
			stats.CCRSkipReason = &reason
		} else if store != nil {
			key := md5Hex24(content)
			var mb strings.Builder
			mb.Grow(len(compressed) + 80)
			mb.WriteString(compressed)
			mb.WriteString("\n[")
			mb.WriteString(strconv.Itoa(originalLineCount))
			mb.WriteString(" lines compressed to ")
			mb.WriteString(strconv.Itoa(len(selected)))
			mb.WriteString(". Retrieve more: hash=")
			mb.WriteString(key)
			mb.WriteByte(']')
			compressed = mb.String()
			store.Put(key, []byte(content))
			cacheKey = &key
			stats.CCREmitted = true
		} else {
			reason := "no store provided"
			stats.CCRSkipReason = &reason
		}
	} else {
		reason := "ccr disabled in config"
		stats.CCRSkipReason = &reason
	}

	result := LogCompressionResult{
		Compressed:          compressed,
		Original:            content,
		OriginalLineCount:   originalLineCount,
		CompressedLineCount: len(selected),
		FormatDetected:      format,
		CompressionRatio:    ratio,
		CacheKey:            cacheKey,
		Stats:               outputStats,
	}
	return result, stats
}

// ── Format detection ────────────────────────────────────────────────

type formatCandidate struct {
	format  LogFormat
	pattern string
}

var formatByFirstByte [256][]formatCandidate

func init() {
	raw := []struct {
		format   LogFormat
		patterns []string
	}{
		{LogFormatPytest, []string{
			"=== FAILURES", "=== ERRORS", "=== test session",
			"=== short test summary", "PASSED [", "FAILED [",
			"ERROR [", "SKIPPED [", "collected ",
		}},
		{LogFormatNpm, []string{"npm ERR!", "npm WARN", "npm info", "npm http"}},
		{LogFormatCargo, []string{"Compiling ", "Finished ", "Running ", "warning: ", "error[E"}},
		{LogFormatJest, []string{"PASS ", "FAIL ", "Test Suites:"}},
		{LogFormatMake, []string{"make[", "make:", "gcc ", "g++ ", "clang "}},
	}
	for _, fp := range raw {
		for _, pat := range fp.patterns {
			b := pat[0]
			formatByFirstByte[b] = append(formatByFirstByte[b], formatCandidate{fp.format, pat})
		}
	}
}

const numFormats = 6

func splitLines(content string) []string {
	n := strings.Count(content, "\n") + 1
	out := make([]string, 0, n)
	start := 0
	for i := 0; i < len(content); i++ {
		if content[i] == '\n' {
			out = append(out, content[start:i])
			start = i + 1
		}
	}
	out = append(out, content[start:])
	return out
}

func detectFormatLines(lines []string) LogFormat {
	sampleN := len(lines)
	if sampleN > 100 {
		sampleN = 100
	}
	var scores [numFormats]int
	var matched [numFormats]bool
	for i := 0; i < sampleN; i++ {
		line := lines[i]
		n := len(line)
		if n == 0 {
			continue
		}
		for k := range matched {
			matched[k] = false
		}
		for j := 0; j < n; j++ {
			candidates := formatByFirstByte[line[j]]
			for _, c := range candidates {
				fi := int(c.format)
				if matched[fi] {
					continue
				}
				pl := len(c.pattern)
				if j+pl <= n && line[j:j+pl] == c.pattern {
					scores[fi]++
					matched[fi] = true
				}
			}
		}
	}
	bestFormat := LogFormatGeneric
	bestScore := 0
	for fi := 0; fi < numFormats-1; fi++ {
		if scores[fi] > bestScore {
			bestScore = scores[fi]
			bestFormat = LogFormat(fi)
		}
	}
	return bestFormat
}

// DetectFormat is exported for testing.
func DetectFormat(lines []string) LogFormat {
	return detectFormatLines(lines)
}

// ── Level classification ────────────────────────────────────────────

func classifyLevel(line string) LogLevel {
	n := len(line)
	if n == 0 {
		return LogLevelUnknown
	}
	best := LogLevel(-1)
	for i := 0; i < n; i++ {
		c := line[i] | 0x20
		if c < 'a' || c > 'z' {
			continue
		}
		if i > 0 && textutil.IsWordChar(line[i-1]) {
			continue
		}
		switch c {
		case 'e':
			if matchLevelWord(line, i, n, "error", 5) {
				return LogLevelError
			}
		case 'f':
			if matchLevelWord(line, i, n, "fatal", 5) {
				return LogLevelError
			}
			if best < LogLevelFail {
				if matchLevelWord(line, i, n, "failed", 6) || matchLevelWord(line, i, n, "fail", 4) {
					best = LogLevelFail
				}
			}
		case 'c':
			if matchLevelWord(line, i, n, "critical", 8) {
				return LogLevelError
			}
		case 'w':
			if best < LogLevelWarn {
				if matchLevelWord(line, i, n, "warning", 7) || matchLevelWord(line, i, n, "warn", 4) {
					best = LogLevelWarn
				}
			}
		case 'i':
			if best < LogLevelInfo {
				if matchLevelWord(line, i, n, "info", 4) {
					best = LogLevelInfo
				}
			}
		case 'd':
			if best < LogLevelDebug {
				if matchLevelWord(line, i, n, "debug", 5) {
					best = LogLevelDebug
				}
			}
		case 't':
			if best < LogLevelTrace {
				if matchLevelWord(line, i, n, "trace", 5) {
					best = LogLevelTrace
				}
			}
		}
	}
	if best < 0 {
		return LogLevelUnknown
	}
	return best
}

func matchLevelWord(line string, i, n int, kw string, kwLen int) bool {
	if i+kwLen > n {
		return false
	}
	for j := 1; j < kwLen; j++ {
		if line[i+j]|0x20 != kw[j] {
			return false
		}
	}
	end := i + kwLen
	if end < n && textutil.IsWordChar(line[end]) {
		return false
	}
	return true
}

// ── Stack trace detection ───────────────────────────────────────────

type traceFlavor int

const (
	traceFlavorPython traceFlavor = iota
	traceFlavorJS
	traceFlavorJava
	traceFlavorRust
	traceFlavorGo
)

func detectTraceFlavor(line string) (traceFlavor, bool) {
	trimmed := strings.TrimLeft(line, " \t")
	if strings.HasPrefix(trimmed, "Traceback (most recent call last)") || isPythonFileFrame(trimmed) {
		return traceFlavorPython, true
	}
	if isJSAtFrame(trimmed) {
		return traceFlavorJS, true
	}
	if isJavaAtFrame(trimmed) {
		return traceFlavorJava, true
	}
	if strings.HasPrefix(trimmed, "--> ") && hasLineColSuffix(trimmed) {
		return traceFlavorRust, true
	}
	if isGoFrame(line) {
		return traceFlavorGo, true
	}
	return 0, false
}

func isPythonFileFrame(s string) bool {
	return strings.HasPrefix(s, `File "`) && strings.Contains(s, `", line `) &&
		len(s) > 0 && s[len(s)-1] >= '0' && s[len(s)-1] <= '9'
}

func isJSAtFrame(s string) bool {
	return strings.HasPrefix(s, "at ") && strings.Contains(s, "(") &&
		strings.Contains(s, ")") && hasLineColSuffix(s)
}

func isJavaAtFrame(s string) bool {
	if !strings.HasPrefix(s, "at ") || !strings.Contains(s, "(") {
		return false
	}
	parenIdx := strings.Index(s, "(")
	body := s[3:parenIdx]
	if body == "" {
		return false
	}
	for _, c := range body {
		if !((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '.' || c == '_' || c == '$') {
			return false
		}
	}
	return true
}

func hasLineColSuffix(s string) bool {
	for i := 0; i < len(s)-2; i++ {
		if s[i] == ':' && s[i+1] >= '0' && s[i+1] <= '9' {
			j := i + 1
			for j < len(s) && s[j] >= '0' && s[j] <= '9' {
				j++
			}
			if j < len(s) && s[j] == ':' && j+1 < len(s) && s[j+1] >= '0' && s[j+1] <= '9' {
				return true
			}
		}
	}
	return false
}

func isGoFrame(s string) bool {
	trimmed := strings.TrimLeft(s, " \t")
	sawDigit := false
	i := 0
	for i < len(trimmed) && trimmed[i] >= '0' && trimmed[i] <= '9' {
		sawDigit = true
		i++
	}
	if !sawDigit || i >= len(trimmed) || trimmed[i] != ':' {
		return false
	}
	i++
	for i < len(trimmed) && trimmed[i] == ' ' {
		i++
	}
	rest := trimmed[i:]
	if !strings.HasPrefix(rest, "0x") {
		return false
	}
	hexPart := rest[2:]
	count := 0
	for _, c := range hexPart {
		if (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') {
			count++
		} else {
			break
		}
	}
	return count > 0
}

func traceTerminates(flavor traceFlavor, line string) bool {
	trimmed := strings.TrimLeft(line, " \t")
	switch flavor {
	case traceFlavorPython:
		isIndentedOrBlank := strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") || line == ""
		isContinuation := strings.HasPrefix(trimmed, "Traceback") ||
			strings.HasPrefix(trimmed, "File ") ||
			strings.HasPrefix(trimmed, "During handling") ||
			strings.HasPrefix(trimmed, "The above exception")
		if isIndentedOrBlank || isContinuation {
			return false
		}
		// Terminate unless starts with uppercase (exception type line).
		if len(trimmed) > 0 {
			return !unicode.IsUpper(rune(trimmed[0]))
		}
		return true
	case traceFlavorJS, traceFlavorJava:
		return !strings.HasPrefix(trimmed, "at ") && line != ""
	case traceFlavorRust:
		return !strings.HasPrefix(trimmed, "--> ") && line != ""
	case traceFlavorGo:
		if line == "" {
			return false
		}
		if len(trimmed) > 0 && trimmed[0] >= '0' && trimmed[0] <= '9' {
			return false
		}
		return true
	}
	return true
}

// ── Summary detection ───────────────────────────────────────────────

func isSummaryLine(line string) bool {
	if len(line) < 3 {
		return false
	}
	c0 := line[0]
	if c0 == '=' && line[1] == '=' && line[2] == '=' {
		return true
	}
	if c0 == '-' && line[1] == '-' && line[2] == '-' {
		return true
	}
	if c0 >= '0' && c0 <= '9' {
		i := 1
		for i < len(line) && line[i] >= '0' && line[i] <= '9' {
			i++
		}
		if i < len(line) && line[i] == ' ' {
			rest := line[i+1:]
			if strings.HasPrefix(rest, "passed") || strings.HasPrefix(rest, "failed") ||
				strings.HasPrefix(rest, "skipped") || strings.HasPrefix(rest, "error") ||
				strings.HasPrefix(rest, "warning") {
				return true
			}
		}
	}
	if c0 == 'T' {
		if rest, ok := strings.CutPrefix(line, "Tests"); ok {
			if isSummaryTestSuffix(rest) {
				return true
			}
		}
		if rest, ok := strings.CutPrefix(line, "Test"); ok {
			if isSummaryTestSuffix(rest) {
				return true
			}
			return strings.Contains(line, "succeeded") || strings.Contains(line, "failed") || strings.Contains(line, "complete")
		}
		if strings.HasPrefix(line, "TOTAL") || strings.HasPrefix(line, "Total") {
			return true
		}
		return false
	}
	if c0 == 'S' {
		if rest, ok := strings.CutPrefix(line, "Suites"); ok {
			if isSummaryTestSuffix(rest) {
				return true
			}
		}
		if rest, ok := strings.CutPrefix(line, "Suite"); ok {
			if isSummaryTestSuffix(rest) {
				return true
			}
		}
		if strings.HasPrefix(line, "Summary") {
			return true
		}
		return false
	}
	if c0 == 'B' && strings.HasPrefix(line, "Build") {
		return strings.Contains(line, "succeeded") || strings.Contains(line, "failed") || strings.Contains(line, "complete")
	}
	if c0 == 'C' && strings.HasPrefix(line, "Compile") {
		return strings.Contains(line, "succeeded") || strings.Contains(line, "failed") || strings.Contains(line, "complete")
	}
	return false
}

func isSummaryTestSuffix(rest string) bool {
	if len(rest) == 0 {
		return false
	}
	if rest[0] == ':' || rest[0] == ' ' {
		for i := 1; i < len(rest); i++ {
			c := rest[i]
			if c != ' ' && c != '\t' {
				return c >= '0' && c <= '9'
			}
		}
	}
	return false
}

// ── Scoring ─────────────────────────────────────────────────────────

func scoreLogLine(line *LogLine) float32 {
	var levelScore float32
	switch line.Level {
	case LogLevelError, LogLevelFail:
		levelScore = 1.0
	case LogLevelWarn:
		levelScore = 0.5
	case LogLevelInfo, LogLevelUnknown:
		levelScore = 0.1
	case LogLevelDebug:
		levelScore = 0.05
	case LogLevelTrace:
		levelScore = 0.02
	}
	var stackBoost float32
	if line.IsStackTrace {
		stackBoost = 0.3
	}
	var summaryBoost float32
	if line.IsSummary {
		summaryBoost = 0.4
	}
	total := levelScore + stackBoost + summaryBoost
	if total > 1.0 {
		total = 1.0
	}
	return total
}

// ── Parse lines ─────────────────────────────────────────────────────

func (lc *LogCompressor) parseLinesFromStrings(lines []string) []LogLine {
	n := len(lines)
	out := make([]LogLine, n)
	var activeFlavor traceFlavor
	activeSet := false
	traceLines := 0

	for i := 0; i < n; i++ {
		line := lines[i]
		out[i] = LogLine{
			LineNumber: i,
			Content:    line,
			Level:      classifyLevel(line),
			IsSummary:  isSummaryLine(line),
		}

		if activeSet {
			if traceLines >= lc.config.StackTraceMaxLines || traceTerminates(activeFlavor, line) {
				activeSet = false
				traceLines = 0
				if flavor, ok := detectTraceFlavor(line); ok {
					activeFlavor = flavor
					activeSet = true
					traceLines = 1
					out[i].IsStackTrace = true
				}
			} else {
				out[i].IsStackTrace = true
				traceLines++
			}
		} else if flavor, ok := detectTraceFlavor(line); ok {
			activeFlavor = flavor
			activeSet = true
			traceLines = 1
			out[i].IsStackTrace = true
		}

		out[i].Score = scoreLogLine(&out[i])
	}
	return out
}

func (lc *LogCompressor) parseLines(lines []string) []LogLine {
	return lc.parseLinesFromStrings(lines)
}

// ── Selection ───────────────────────────────────────────────────────

func (lc *LogCompressor) selectLines(logLines []LogLine, allLines []string, bias float64, stats *LogCompressorStats) []LogLine {
	const adaptiveMinK = 10
	maxK := lc.config.MaxTotalLines
	ordered := lc.selectLinesCore(logLines, stats)

	if len(ordered) > adaptiveMinK {
		adaptiveMax := adaptivesizer.ComputeOptimalK(allLines, bias, adaptiveMinK, &maxK)
		if len(ordered) > adaptiveMax {
			stats.LinesDroppedByGlobalCap += len(ordered) - adaptiveMax
			sort.SliceStable(ordered, func(i, j int) bool {
				if ordered[i].Score != ordered[j].Score {
					return ordered[i].Score > ordered[j].Score
				}
				return ordered[i].LineNumber < ordered[j].LineNumber
			})
			ordered = ordered[:adaptiveMax]
			sort.Slice(ordered, func(i, j int) bool {
				return ordered[i].LineNumber < ordered[j].LineNumber
			})
		}
	}

	return ordered
}

func (lc *LogCompressor) selectLinesWithMax(logLines []LogLine, adaptiveMax int, stats *LogCompressorStats) []LogLine {
	ordered := lc.selectLinesCore(logLines, stats)

	if len(ordered) > adaptiveMax {
		stats.LinesDroppedByGlobalCap += len(ordered) - adaptiveMax
		sort.SliceStable(ordered, func(i, j int) bool {
			if ordered[i].Score != ordered[j].Score {
				return ordered[i].Score > ordered[j].Score
			}
			return ordered[i].LineNumber < ordered[j].LineNumber
		})
		ordered = ordered[:adaptiveMax]
		sort.Slice(ordered, func(i, j int) bool {
			return ordered[i].LineNumber < ordered[j].LineNumber
		})
	}

	return ordered
}

func (lc *LogCompressor) selectLinesCore(logLines []LogLine, stats *LogCompressorStats) []LogLine {
	n := len(logLines)

	var errorIdx, failIdx, warnIdx, summaryIdx []int
	var stackTraces [][]int
	var currentStack []int

	for i := range logLines {
		switch logLines[i].Level {
		case LogLevelError:
			errorIdx = append(errorIdx, i)
		case LogLevelFail:
			failIdx = append(failIdx, i)
		case LogLevelWarn:
			warnIdx = append(warnIdx, i)
		}
		if logLines[i].IsStackTrace {
			currentStack = append(currentStack, i)
		} else if len(currentStack) > 0 {
			stackTraces = append(stackTraces, currentStack)
			currentStack = nil
		}
		if logLines[i].IsSummary {
			summaryIdx = append(summaryIdx, i)
		}
	}
	if len(currentStack) > 0 {
		stackTraces = append(stackTraces, currentStack)
	}
	stats.StackTracesSeen = len(stackTraces)

	sel := make([]bool, n)
	selCount := 0
	markSelected := func(idx int) {
		if !sel[idx] {
			sel[idx] = true
			selCount++
		}
	}

	for _, idx := range lc.selectWithFirstLastIdx(logLines, errorIdx, lc.config.MaxErrors) {
		markSelected(idx)
	}
	for _, idx := range lc.selectWithFirstLastIdx(logLines, failIdx, lc.config.MaxErrors) {
		markSelected(idx)
	}

	if lc.config.DedupeWarnings {
		original := len(warnIdx)
		warnIdx = dedupeSimilarIdx(logLines, warnIdx)
		stats.WarningsDroppedByDedupe = original - len(warnIdx)
	}
	for i, idx := range warnIdx {
		if i >= lc.config.MaxWarnings {
			break
		}
		markSelected(idx)
	}

	for i, stack := range stackTraces {
		if i >= lc.config.MaxStackTraces {
			break
		}
		stats.StackTracesKept++
		for j, idx := range stack {
			if j >= lc.config.StackTraceMaxLines {
				break
			}
			markSelected(idx)
		}
	}

	if lc.config.KeepSummaryLines {
		for _, idx := range summaryIdx {
			markSelected(idx)
		}
	}

	contextSel := make([]bool, n)
	for i := 0; i < n; i++ {
		if sel[i] {
			lo := i - lc.config.ErrorContextLines
			if lo < 0 {
				lo = 0
			}
			hi := i + lc.config.ErrorContextLines + 1
			if hi > n {
				hi = n
			}
			for j := lo; j < hi; j++ {
				if j != i {
					contextSel[j] = true
				}
			}
		}
	}
	for i := 0; i < n; i++ {
		if contextSel[i] && !sel[i] {
			sel[i] = true
			selCount++
		}
	}

	ordered := make([]LogLine, 0, selCount)
	for i := 0; i < n; i++ {
		if sel[i] {
			ordered = append(ordered, logLines[i])
		}
	}

	return ordered
}

// selectWithFirstLastIdx selects up to maxCount indices from idx,
// keeping first/last and highest-scoring. Returns selected indices.
func (lc *LogCompressor) selectWithFirstLastIdx(logLines []LogLine, idx []int, maxCount int) []int {
	if len(idx) <= maxCount {
		return idx
	}
	out := make([]int, 0, maxCount)
	seen := make([]bool, len(logLines))
	push := func(i int) {
		if !seen[i] {
			seen[i] = true
			out = append(out, i)
		}
	}
	if lc.config.KeepFirstError {
		push(idx[0])
	}
	if lc.config.KeepLastError {
		push(idx[len(idx)-1])
	}
	remaining := maxCount - len(out)
	if remaining > 0 {
		byScore := make([]int, len(idx))
		copy(byScore, idx)
		sort.SliceStable(byScore, func(i, j int) bool {
			si, sj := logLines[byScore[i]].Score, logLines[byScore[j]].Score
			if si != sj {
				return si > sj
			}
			return byScore[i] < byScore[j]
		})
		for _, i := range byScore {
			if !seen[i] {
				push(i)
				if len(out) >= maxCount {
					break
				}
			}
		}
	}
	return out
}

// ── Deduplication ───────────────────────────────────────────────────

func normalizeForDedupe(content string) string {
	splitAt := strings.IndexAny(content, ":=")
	if splitAt < 0 {
		splitAt = len(content)
	}
	prefix := content[:splitAt]
	suffix := content[splitAt:]

	buf := make([]byte, 0, len(suffix))
	i := 0
	for i < len(suffix) {
		if suffix[i] >= '0' && suffix[i] <= '9' {
			if suffix[i] == '0' && i+2 < len(suffix) && suffix[i+1] == 'x' && isHexByte(suffix[i+2]) {
				j := i + 2
				allNonDigitHex := true
				for j < len(suffix) && isHexByte(suffix[j]) {
					if suffix[j] >= '0' && suffix[j] <= '9' {
						allNonDigitHex = false
					}
					j++
				}
				if allNonDigitHex && j > i+2 {
					buf = append(buf, "ADDR"...)
					i = j
					continue
				}
			}
			buf = append(buf, 'N')
			i++
			for i < len(suffix) && suffix[i] >= '0' && suffix[i] <= '9' {
				i++
			}
			continue
		}
		buf = append(buf, suffix[i])
		i++
	}

	var b strings.Builder
	b.Grow(len(prefix) + len(buf))
	b.WriteString(prefix)
	i = 0
	for i < len(buf) {
		if buf[i] == '/' && i+1 < len(buf) && isWordSlashByte(buf[i+1]) {
			j := i + 1
			for j < len(buf) && isWordSlashByte(buf[j]) {
				j++
			}
			lastSlash := -1
			for k := j - 1; k > i; k-- {
				if buf[k] == '/' {
					lastSlash = k
					break
				}
			}
			if lastSlash > i {
				b.WriteString("/PATH/")
				i = lastSlash + 1
				continue
			}
		}
		b.WriteByte(buf[i])
		i++
	}
	return b.String()
}

func isHexByte(b byte) bool {
	return (b >= '0' && b <= '9') || (b >= 'a' && b <= 'f') || (b >= 'A' && b <= 'F')
}

func isWordSlashByte(b byte) bool {
	return textutil.IsWordChar(b) || b == '/'
}

// dedupeSimilarIdx deduplicates warning indices by normalized content.
func dedupeSimilarIdx(logLines []LogLine, idx []int) []int {
	seen := make(map[string]bool, len(idx))
	out := make([]int, 0, len(idx))
	for _, i := range idx {
		key := normalizeForDedupe(logLines[i].Content)
		if !seen[key] {
			seen[key] = true
			out = append(out, i)
		}
	}
	return out
}

// dedupeSimilar is a convenience wrapper used by tests.
func dedupeSimilar(lines []LogLine) []LogLine {
	idx := make([]int, len(lines))
	for i := range lines {
		idx[i] = i
	}
	kept := dedupeSimilarIdx(lines, idx)
	out := make([]LogLine, len(kept))
	for i, j := range kept {
		out[i] = lines[j]
	}
	return out
}

// selectWithFirstLast is a convenience wrapper used by tests.
func (lc *LogCompressor) selectWithFirstLast(lines []LogLine, maxCount int) []LogLine {
	idx := make([]int, len(lines))
	for i := range lines {
		idx[i] = i
	}
	kept := lc.selectWithFirstLastIdx(lines, idx, maxCount)
	out := make([]LogLine, len(kept))
	for i, j := range kept {
		out[i] = lines[j]
	}
	return out
}

// ── Output formatting ───────────────────────────────────────────────

func (lc *LogCompressor) formatOutput(selected []LogLine, allLines []LogLine) (string, map[string]int64) {
	stats := map[string]int64{
		"errors":   countLevel(allLines, LogLevelError),
		"fails":    countLevel(allLines, LogLevelFail),
		"warnings": countLevel(allLines, LogLevelWarn),
		"info":     countLevel(allLines, LogLevelInfo),
		"total":    int64(len(allLines)),
		"selected": int64(len(selected)),
	}

	// Estimate output size to minimize Builder growth.
	estSize := 0
	for i := range selected {
		estSize += len(selected[i].Content) + 1 // +1 for newline separator
	}
	estSize += 128 // room for the omitted-lines trailer

	var b strings.Builder
	b.Grow(estSize)

	for i := range selected {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(selected[i].Content)
	}

	omitted := len(allLines) - len(selected)
	if omitted > 0 {
		type labelKey struct {
			label string
			key   string
		}
		entries := [4]labelKey{
			{"ERROR", "errors"},
			{"FAIL", "fails"},
			{"WARN", "warnings"},
			{"INFO", "info"},
		}
		// Build the parts inline into a small buffer to avoid []string alloc.
		var partsB strings.Builder
		partsB.Grow(64)
		partCount := 0
		for _, entry := range entries {
			n := stats[entry.key]
			if n > 0 {
				if partCount > 0 {
					partsB.WriteString(", ")
				}
				partsB.WriteString(strconv.FormatInt(n, 10))
				partsB.WriteByte(' ')
				partsB.WriteString(entry.label)
				partCount++
			}
		}
		if partCount > 0 {
			if len(selected) > 0 {
				b.WriteByte('\n')
			}
			b.WriteByte('[')
			b.WriteString(strconv.Itoa(omitted))
			b.WriteString(" lines omitted: ")
			b.WriteString(partsB.String())
			b.WriteByte(']')
		}
	}
	return b.String(), stats
}

func countLevel(lines []LogLine, level LogLevel) int64 {
	var count int64
	for _, l := range lines {
		if l.Level == level {
			count++
		}
	}
	return count
}

// ── Helpers ─────────────────────────────────────────────────────────

func md5Hex24(s string) string {
	h := md5.Sum([]byte(s))
	var buf [32]byte
	hex.Encode(buf[:], h[:])
	return string(buf[:24])
}
