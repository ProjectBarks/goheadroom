// Package diffcompressor compresses unified-diff output by parsing into
// files + hunks, capping file and hunk counts, trimming context lines,
// and optionally emitting a CCR cache key.
//
// Port of headroom-core/src/transforms/diff_compressor.rs.
package diffcompressor

import (
	"crypto/md5"
	"encoding/hex"
	"sort"
	"strconv"
	"strings"

	"github.com/uber/goheadroom/ccr"
	"github.com/uber/goheadroom/internal/textutil"
)

// ── Score-weight constants ──────────────────────────────────────────

const (
	ScoreChangeDensityWeight = 0.03
	ScoreChangeDensityCap    = 0.3
	ScoreContextWordWeight   = 0.2
	ScoreContextMinWordLen   = 2
	ScorePriorityPatternBoost = 0.3
	ScoreTotalCap            = 1.0
)

// ── Config ──────────────────────────────────────────────────────────

// DiffCompressorConfig mirrors Rust DiffCompressorConfig.
type DiffCompressorConfig struct {
	MaxContextLines          int
	MaxHunksPerFile          int
	MaxFiles                 int
	AlwaysKeepAdditions      bool
	AlwaysKeepDeletions      bool
	EnableCCR                bool
	MinLinesForCCR           int
	MinCompressionRatioForCCR float64
}

// DefaultConfig returns defaults matching the Rust/Python implementation.
func DefaultConfig() DiffCompressorConfig {
	return DiffCompressorConfig{
		MaxContextLines:          2,
		MaxHunksPerFile:          10,
		MaxFiles:                 20,
		AlwaysKeepAdditions:      true,
		AlwaysKeepDeletions:      true,
		EnableCCR:                true,
		MinLinesForCCR:           50,
		MinCompressionRatioForCCR: 0.8,
	}
}

// ── Result types ────────────────────────────────────────────────────

// DiffCompressionResult is the parity-equal result.
type DiffCompressionResult struct {
	Compressed          string
	OriginalLineCount   int
	CompressedLineCount int
	FilesAffected       int
	Additions           int
	Deletions           int
	HunksKept           int
	HunksRemoved        int
	CacheKey            *string
}

// DiffCompressorStats carries observability sidecar data.
type DiffCompressorStats struct {
	InputLines              int
	OutputLines             int
	CompressionRatio        float64
	FilesTotal              int
	FilesKept               int
	FilesDropped            []string
	HunksTotal              int
	HunksKept               int
	HunksDropped            int
	HunksDroppedPerFile     map[string]int
	ContextLinesInput       int
	ContextLinesKept        int
	ContextLinesTrimmed     int
	LargestHunkKeptLines    int
	LargestHunkDroppedLines int
	FileModeNormalizations  [][2]string
	BinaryFilesSimplified   []string
	ParseWarnings           []string
	CacheKeyEmitted         bool
	CCRSkippedReason        *string
}

// ── Compressor ──────────────────────────────────────────────────────

// DiffCompressor holds only the config.
type DiffCompressor struct {
	config DiffCompressorConfig
}

// New creates a DiffCompressor with the given config.
func New(config DiffCompressorConfig) *DiffCompressor {
	return &DiffCompressor{config: config}
}

// Compress compresses content. context is an optional user-query string
// used for relevance scoring when max_hunks_per_file fires.
func (dc *DiffCompressor) Compress(content, context string) DiffCompressionResult {
	r, _ := dc.CompressWithStats(content, context)
	return r
}

// CompressWithStats returns the result and granular stats.
func (dc *DiffCompressor) CompressWithStats(content, context string) (DiffCompressionResult, DiffCompressorStats) {
	return dc.CompressWithStore(content, context, nil)
}

// CompressWithStore compresses with optional CCR persistence.
func (dc *DiffCompressor) CompressWithStore(content, context string, store ccr.CcrStore) (DiffCompressionResult, DiffCompressorStats) {
	stats := DiffCompressorStats{
		HunksDroppedPerFile: make(map[string]int),
	}

	li := textutil.NewLineIndex(content)
	originalLineCount := li.LineCount()
	stats.InputLines = originalLineCount

	if originalLineCount < dc.config.MinLinesForCCR {
		stats.OutputLines = originalLineCount
		stats.CompressionRatio = 1.0
		reason := "input below min_lines_for_ccr"
		stats.CCRSkippedReason = &reason
		return passThroughResult(content, originalLineCount), stats
	}

	parsed := parseDiff(&li)
	preDiffLines := parsed.preDiffLines
	diffFiles := parsed.files
	stats.ParseWarnings = parsed.parseWarnings
	stats.FilesTotal = len(diffFiles)
	for _, f := range diffFiles {
		stats.HunksTotal += len(f.hunks)
		for _, h := range f.hunks {
			stats.ContextLinesInput += h.contextLines
		}
	}

	// Short-circuit 2: no diff sections parsed -> pass through.
	if len(diffFiles) == 0 {
		stats.OutputLines = originalLineCount
		stats.CompressionRatio = 1.0
		reason := "no diff sections parsed"
		stats.CCRSkippedReason = &reason
		return passThroughResult(content, originalLineCount), stats
	}

	// Score hunks.
	scoreHunks(diffFiles, context)

	// File cap.
	if len(diffFiles) > dc.config.MaxFiles {
		sort.SliceStable(diffFiles, func(i, j int) bool {
			ci := diffFiles[i].totalAdditions() + diffFiles[i].totalDeletions()
			cj := diffFiles[j].totalAdditions() + diffFiles[j].totalDeletions()
			return ci > cj
		})
		dropped := diffFiles[dc.config.MaxFiles:]
		for _, f := range dropped {
			stats.FilesDropped = append(stats.FilesDropped, f.oldFile+" -> "+f.newFile)
		}
		diffFiles = diffFiles[:dc.config.MaxFiles]
	}
	stats.FilesKept = len(diffFiles)

	// Capture lossy-emit signals.
	for _, file := range diffFiles {
		label := file.oldFile + " -> " + file.newFile
		if file.originalNewFileModeLine != nil && *file.originalNewFileModeLine != "new file mode 100644" {
			stats.FileModeNormalizations = append(stats.FileModeNormalizations, [2]string{label, *file.originalNewFileModeLine})
		}
		if file.originalDeletedFileModeLine != nil && *file.originalDeletedFileModeLine != "deleted file mode 100644" {
			stats.FileModeNormalizations = append(stats.FileModeNormalizations, [2]string{label, *file.originalDeletedFileModeLine})
		}
		if file.originalBinaryLine != nil && *file.originalBinaryLine != "Binary files differ" {
			stats.BinaryFilesSimplified = append(stats.BinaryFilesSimplified, *file.originalBinaryLine)
		}
	}

	// Compress each file's hunks.
	compressedFiles := make([]*diffFile, 0, len(diffFiles))
	var totalAdditions, totalDeletions, hunksKeptTotal, hunksRemovedTotal int
	var largestKept, largestDropped int
	var contextKeptTotal int

	for _, file := range diffFiles {
		totalAdditions += file.totalAdditions()
		totalDeletions += file.totalDeletions()
		originalHunkCount := len(file.hunks)

		selected, dropped := selectHunks(file.hunks, dc.config.MaxHunksPerFile)
		droppedCount := len(dropped)
		if droppedCount > 0 {
			fileLabel := file.oldFile + " -> " + file.newFile
			stats.HunksDroppedPerFile[fileLabel] = droppedCount
			for _, h := range dropped {
				if h.lineCount() > largestDropped {
					largestDropped = h.lineCount()
				}
			}
		}

		// Trim context inside each kept hunk.
		compressedHunks := make([]*diffHunk, 0, len(selected))
		for _, hunk := range selected {
			trimmed := reduceContext(hunk, dc.config.MaxContextLines)
			if trimmed.lineCount() > largestKept {
				largestKept = trimmed.lineCount()
			}
			contextKeptTotal += trimmed.contextLines
			compressedHunks = append(compressedHunks, trimmed)
		}

		hunksKeptTotal += len(compressedHunks)
		hunksRemovedTotal += originalHunkCount - len(compressedHunks)

		cf := *file
		cf.hunks = compressedHunks
		compressedFiles = append(compressedFiles, &cf)
	}

	stats.HunksKept = hunksKeptTotal
	stats.HunksDropped = hunksRemovedTotal
	stats.ContextLinesKept = contextKeptTotal
	if stats.ContextLinesInput > contextKeptTotal {
		stats.ContextLinesTrimmed = stats.ContextLinesInput - contextKeptTotal
	}
	stats.LargestHunkKeptLines = largestKept
	stats.LargestHunkDroppedLines = largestDropped

	filesAffected := len(compressedFiles)

	// Format compressed output.
	compressedOutput := formatOutput(preDiffLines, compressedFiles, filesAffected, totalAdditions, totalDeletions, hunksRemovedTotal)
	compressedLineCount := countSplitLines(compressedOutput)

	// CCR layer.
	savingsThreshold := dc.config.MinCompressionRatioForCCR
	var cacheKey *string
	if dc.config.EnableCCR && float64(compressedLineCount) < float64(originalLineCount)*savingsThreshold {
		key := md5Hex24(content)
		var cb strings.Builder
		cb.Grow(len(compressedOutput) + 80)
		cb.WriteString(compressedOutput)
		cb.WriteString("\n[")
		cb.WriteString(strconv.Itoa(originalLineCount))
		cb.WriteString(" lines compressed to ")
		cb.WriteString(strconv.Itoa(compressedLineCount))
		cb.WriteString(". Retrieve full diff: hash=")
		cb.WriteString(key)
		cb.WriteByte(']')
		compressedOutput = cb.String()
		if store != nil {
			store.Put(key, []byte(content))
		}
		cacheKey = &key
		stats.CacheKeyEmitted = true
	} else if !dc.config.EnableCCR {
		reason := "ccr disabled"
		stats.CCRSkippedReason = &reason
	} else {
		var ratio float64
		if originalLineCount == 0 {
			ratio = 1.0
		} else {
			ratio = float64(compressedLineCount) / float64(originalLineCount)
		}
		reason := "compression ratio " + strconv.FormatFloat(ratio, 'f', 3, 64) + " above threshold " + strconv.FormatFloat(savingsThreshold, 'f', 3, 64)
		stats.CCRSkippedReason = &reason
	}

	stats.OutputLines = compressedLineCount
	if originalLineCount == 0 {
		stats.CompressionRatio = 1.0
	} else {
		stats.CompressionRatio = float64(compressedLineCount) / float64(originalLineCount)
	}

	result := DiffCompressionResult{
		Compressed:          compressedOutput,
		OriginalLineCount:   originalLineCount,
		CompressedLineCount: compressedLineCount,
		FilesAffected:       filesAffected,
		Additions:           totalAdditions,
		Deletions:           totalDeletions,
		HunksKept:           hunksKeptTotal,
		HunksRemoved:        hunksRemovedTotal,
		CacheKey:            cacheKey,
	}

	return result, stats
}

// ── Internal types ──────────────────────────────────────────────────

type diffHunk struct {
	header       string
	lines        []string
	li           *textutil.LineIndex
	lineStart    int
	lineEnd      int
	additions    int
	deletions    int
	contextLines int
	score        float64
}

func (h *diffHunk) lineCount() int {
	if h.lines != nil {
		return len(h.lines)
	}
	return h.lineEnd - h.lineStart
}

func (h *diffHunk) lineAt(i int) string {
	if h.lines != nil {
		return h.lines[i]
	}
	return h.li.Line(h.lineStart + i)
}

type diffFile struct {
	header                      string
	oldFile                     string
	newFile                     string
	hunks                       []*diffHunk
	isBinary                    bool
	isNewFile                   bool
	isDeletedFile               bool
	isRenamed                   bool
	renameLines                 []string
	originalNewFileModeLine     *string
	originalDeletedFileModeLine *string
	originalBinaryLine          *string
}

func (f *diffFile) totalAdditions() int {
	sum := 0
	for _, h := range f.hunks {
		sum += h.additions
	}
	return sum
}

func (f *diffFile) totalDeletions() int {
	sum := 0
	for _, h := range f.hunks {
		sum += h.deletions
	}
	return sum
}

// ── Diff-line matchers (no regexp) ──────────────────────────────────

func isDigit(c byte) bool {
	return c >= '0' && c <= '9'
}

func skipDigits(s string, i int) int {
	for i < len(s) && isDigit(s[i]) {
		i++
	}
	return i
}

func skipRange(s string, i int) (int, bool) {
	if i >= len(s) || s[i] != '-' {
		return i, false
	}
	i++
	j := skipDigits(s, i)
	if j == i {
		return i, false
	}
	i = j
	if i < len(s) && s[i] == ',' {
		i++
		j = skipDigits(s, i)
		if j == i {
			return i, false
		}
		i = j
	}
	return i, true
}

func skipPlusRange(s string, i int) (int, bool) {
	if i >= len(s) || s[i] != '+' {
		return i, false
	}
	i++
	j := skipDigits(s, i)
	if j == i {
		return i, false
	}
	i = j
	if i < len(s) && s[i] == ',' {
		i++
		j = skipDigits(s, i)
		if j == i {
			return i, false
		}
		i = j
	}
	return i, true
}

func matchHunkHeader(line string) bool {
	if len(line) < 5 || line[0] != '@' || line[1] != '@' {
		return false
	}

	if line[2] == ' ' {
		i := 3
		var ok bool
		i, ok = skipRange(line, i)
		if !ok {
			return false
		}
		if i >= len(line) || line[i] != ' ' {
			return false
		}
		i++
		i, ok = skipPlusRange(line, i)
		if !ok {
			return false
		}
		if i >= len(line) || line[i] != ' ' {
			return false
		}
		i++
		if i+1 >= len(line) || line[i] != '@' || line[i+1] != '@' {
			return false
		}
		return true
	}

	if len(line) >= 6 && line[2] == '@' && line[3] == ' ' {
		i := 4
		var ok bool
		i, ok = skipRange(line, i)
		if !ok {
			return false
		}
		if i >= len(line) || line[i] != ' ' {
			return false
		}
		i++
		i, ok = skipRange(line, i)
		if !ok {
			return false
		}
		if i >= len(line) || line[i] != ' ' {
			return false
		}
		i++
		i, ok = skipPlusRange(line, i)
		if !ok {
			return false
		}
		if i >= len(line) || line[i] != ' ' {
			return false
		}
		i++
		if i+2 >= len(line) || line[i] != '@' || line[i+1] != '@' || line[i+2] != '@' {
			return false
		}
		return true
	}

	if len(line) >= 7 && line[2] == '@' && line[3] == '@' && line[4] == ' ' {
		i := 5
		var ok bool
		i, ok = skipRange(line, i)
		if !ok {
			return false
		}
		if i >= len(line) || line[i] != ' ' {
			return false
		}
		i++
		i, ok = skipRange(line, i)
		if !ok {
			return false
		}
		if i >= len(line) || line[i] != ' ' {
			return false
		}
		i++
		i, ok = skipRange(line, i)
		if !ok {
			return false
		}
		if i >= len(line) || line[i] != ' ' {
			return false
		}
		i++
		i, ok = skipPlusRange(line, i)
		if !ok {
			return false
		}
		if i >= len(line) || line[i] != ' ' {
			return false
		}
		i++
		if i+3 >= len(line) || line[i] != '@' || line[i+1] != '@' || line[i+2] != '@' || line[i+3] != '@' {
			return false
		}
		return true
	}

	return false
}

func matchDiffGit(line string) bool {
	return strings.HasPrefix(line, "diff --git a/") && strings.Contains(line[13:], " b/")
}

func matchDiffCombined(line string) bool {
	return strings.HasPrefix(line, "diff --combined ") && len(line) > 16
}

func matchDiffCC(line string) bool {
	return strings.HasPrefix(line, "diff --cc ") && len(line) > 10
}

func matchOldFile(line string) bool {
	return line == "--- /dev/null" || (strings.HasPrefix(line, "--- a/") && len(line) > 6)
}

func matchNewFile(line string) bool {
	return line == "+++ /dev/null" || (strings.HasPrefix(line, "+++ b/") && len(line) > 6)
}

func matchBinary(line string) bool {
	return strings.HasPrefix(line, "Binary files ") && strings.HasSuffix(line, " differ") && len(line) > 20
}

var priorityKeywords = [][]string{
	{"error", "exception", "failed", "failure", "fail", "fatal", "critical", "crash", "panic"},
	{"important", "note", "todo", "fixme", "hack", "xxx", "bug", "fix"},
	{"security", "auth", "password", "secret", "token"},
}

func isDiffHeader(line string) bool {
	if len(line) < 11 || line[0] != 'd' || line[4] != ' ' || line[5] != '-' || line[6] != '-' {
		return false
	}
	return matchDiffGit(line) || matchDiffCombined(line) || matchDiffCC(line)
}

// ── Parser ──────────────────────────────────────────────────────────

type parsedDiff struct {
	preDiffLines  []string
	files         []*diffFile
	parseWarnings []string
}

func parseDiff(li *textutil.LineIndex) *parsedDiff {
	files := make([]*diffFile, 0, 16)
	var currentFile *diffFile
	var currentHunk *diffHunk
	var preDiffLines []string
	var warnings []string

	n := li.LineCount()
	for idx := 0; idx < n; idx++ {
		line := li.Line(idx)
		lineLen := len(line)

		if currentHunk != nil && lineLen > 0 {
			c := line[0]
			switch c {
			case '+':
				if lineLen < 4 || line[1] != '+' || line[2] != '+' {
					currentHunk.additions++
					currentHunk.lineEnd = idx + 1
					continue
				}
			case '-':
				if lineLen < 4 || line[1] != '-' || line[2] != '-' {
					currentHunk.deletions++
					currentHunk.lineEnd = idx + 1
					continue
				}
			case ' ':
				currentHunk.contextLines++
				currentHunk.lineEnd = idx + 1
				continue
			case '\\':
				currentHunk.lineEnd = idx + 1
				continue
			}
		} else if currentHunk != nil {
			currentHunk.contextLines++
			currentHunk.lineEnd = idx + 1
			continue
		}

		if lineLen > 10 && line[0] == 'd' && line[1] == 'i' && isDiffHeader(line) {
			if currentHunk != nil {
				if currentFile != nil {
					currentFile.hunks = append(currentFile.hunks, currentHunk)
				}
				currentHunk = nil
			}
			if currentFile != nil {
				files = append(files, currentFile)
			}
			currentFile = &diffFile{
				header: line,
				hunks:  make([]*diffHunk, 0, 8),
			}
			continue
		}

		if currentFile == nil {
			preDiffLines = append(preDiffLines, line)
			continue
		}

		if lineLen > 0 && line[0] == '@' && matchHunkHeader(line) {
			if currentHunk != nil {
				currentFile.hunks = append(currentFile.hunks, currentHunk)
			}
			currentHunk = &diffHunk{
				header:    line,
				li:        li,
				lineStart: idx + 1,
				lineEnd:   idx + 1,
			}
			continue
		}

		if lineLen > 3 && line[0] == '-' && line[1] == '-' && line[2] == '-' && matchOldFile(line) {
			currentFile.oldFile = line
			continue
		}

		if lineLen > 3 && line[0] == '+' && line[1] == '+' && line[2] == '+' && matchNewFile(line) {
			currentFile.newFile = line
			continue
		}

		if lineLen > 0 {
			switch line[0] {
			case 'n':
				if strings.HasPrefix(line, "new file mode") {
					currentFile.isNewFile = true
					s := line
					currentFile.originalNewFileModeLine = &s
				}
			case 'd':
				if strings.HasPrefix(line, "deleted file mode") {
					currentFile.isDeletedFile = true
					s := line
					currentFile.originalDeletedFileModeLine = &s
				} else if strings.HasPrefix(line, "dissimilarity ") {
					currentFile.isRenamed = true
					currentFile.renameLines = append(currentFile.renameLines, line)
				}
			case 'r':
				if strings.HasPrefix(line, "rename ") {
					currentFile.isRenamed = true
					currentFile.renameLines = append(currentFile.renameLines, line)
				}
			case 's':
				if strings.HasPrefix(line, "similarity ") {
					currentFile.isRenamed = true
					currentFile.renameLines = append(currentFile.renameLines, line)
				}
			case 'c':
				if strings.HasPrefix(line, "copy ") {
					currentFile.isRenamed = true
					currentFile.renameLines = append(currentFile.renameLines, line)
				}
			case 'B':
				if matchBinary(line) {
					currentFile.isBinary = true
					s := line
					currentFile.originalBinaryLine = &s
				}
			}
		}

		if currentHunk != nil {
			currentHunk.lineEnd = idx + 1
		}
	}

	if currentHunk != nil && currentFile != nil {
		currentFile.hunks = append(currentFile.hunks, currentHunk)
	}
	if currentFile != nil {
		files = append(files, currentFile)
	}

	return &parsedDiff{
		preDiffLines:  preDiffLines,
		files:         files,
		parseWarnings: warnings,
	}
}

// ── Scoring ─────────────────────────────────────────────────────────

// containsFoldASCII performs case-insensitive substring search without
// allocating a lowered copy.  substr must already be lowercase.
func containsFoldASCII(s, substr string) bool {
	n := len(substr)
	if n == 0 {
		return true
	}
	if n > len(s) {
		return false
	}
	for i := 0; i <= len(s)-n; i++ {
		match := true
		for j := 0; j < n; j++ {
			c := s[i+j]
			if c >= 'A' && c <= 'Z' {
				c += 32
			}
			if c != substr[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// hunkContainsFold checks whether any line in the hunk contains substr
// (case-insensitive, ASCII).  substr must already be lowercase.
func hunkContainsFoldH(h *diffHunk, substr string) bool {
	n := h.lineCount()
	for i := 0; i < n; i++ {
		if containsFoldASCII(h.lineAt(i), substr) {
			return true
		}
	}
	return false
}

func hunkMatchesPriorityH(h *diffHunk) bool {
	n := h.lineCount()
	for i := 0; i < n; i++ {
		l := h.lineAt(i)
		for _, group := range priorityKeywords {
			if textutil.ContainsAnyWordCI(l, group) {
				return true
			}
		}
	}
	return false
}

func scoreHunks(files []*diffFile, context string) {
	var contextLower string
	if len(context) > 0 {
		contextLower = strings.ToLower(context)
	}

	for _, file := range files {
		for _, hunk := range file.hunks {
			score := float64(hunk.additions+hunk.deletions) * ScoreChangeDensityWeight
			if score > ScoreChangeDensityCap {
				score = ScoreChangeDensityCap
			}

			i := 0
			for i < len(contextLower) {
				for i < len(contextLower) && contextLower[i] == ' ' {
					i++
				}
				if i >= len(contextLower) {
					break
				}
				j := i
				for j < len(contextLower) && contextLower[j] != ' ' {
					j++
				}
				word := contextLower[i:j]
				if len(word) > ScoreContextMinWordLen && hunkContainsFoldH(hunk, word) {
					score += ScoreContextWordWeight
				}
				i = j
			}

			if hunkMatchesPriorityH(hunk) {
				score += ScorePriorityPatternBoost
			}

			if score > ScoreTotalCap {
				score = ScoreTotalCap
			}
			hunk.score = score
		}
	}
}

// ── Hunk selection ──────────────────────────────────────────────────

func selectHunks(hunks []*diffHunk, maxPerFile int) ([]*diffHunk, []*diffHunk) {
	if len(hunks) <= maxPerFile {
		return hunks, nil
	}
	if len(hunks) == 0 {
		return nil, nil
	}

	// Python keeps first + last + top-scored middle, then resorts by
	// hunk header start-line.
	type indexedHunk struct {
		idx  int
		hunk *diffHunk
	}

	indexed := make([]indexedHunk, len(hunks))
	for i, h := range hunks {
		indexed[i] = indexedHunk{i, h}
	}

	first := indexed[0]
	var last *indexedHunk
	if len(indexed) > 1 {
		l := indexed[len(indexed)-1]
		last = &l
	}

	middle := indexed[1:]
	if last != nil {
		middle = indexed[1 : len(indexed)-1]
	}

	remainingSlots := maxPerFile - 1
	if last != nil {
		remainingSlots = maxPerFile - 2
	}
	if remainingSlots < 0 {
		remainingSlots = 0
	}

	// Sort middle by score desc.
	sort.SliceStable(middle, func(i, j int) bool {
		return middle[i].hunk.score > middle[j].hunk.score
	})

	var keptMiddle []indexedHunk
	var droppedMiddle []*diffHunk
	for i, ih := range middle {
		if i < remainingSlots {
			keptMiddle = append(keptMiddle, ih)
		} else {
			droppedMiddle = append(droppedMiddle, ih.hunk)
		}
	}

	// Reassemble in original order using line numbers.
	selected := make([]indexedHunk, 0, maxPerFile)
	selected = append(selected, first)
	selected = append(selected, keptMiddle...)
	if last != nil {
		selected = append(selected, *last)
	}

	// Sort by hunk header line number (matches Python's _extract_line_number).
	sort.SliceStable(selected, func(i, j int) bool {
		return extractLineNumber(selected[i].hunk.header) < extractLineNumber(selected[j].hunk.header)
	})

	result := make([]*diffHunk, len(selected))
	for i, ih := range selected {
		result[i] = ih.hunk
	}
	return result, droppedMiddle
}

func extractLineNumber(header string) int {
	i := strings.IndexByte(header, '+')
	if i < 0 {
		return 0
	}
	i++
	j := i
	for j < len(header) && isDigit(header[j]) {
		j++
	}
	if j == i {
		return 0
	}
	n, _ := strconv.Atoi(header[i:j])
	return n
}

// ── Context trimming ────────────────────────────────────────────────

func reduceContext(hunk *diffHunk, maxContext int) *diffHunk {
	n := hunk.lineCount()

	changeCount := 0
	for i := 0; i < n; i++ {
		line := hunk.lineAt(i)
		if len(line) > 0 && (line[0] == '+' || line[0] == '-') {
			changeCount++
		}
	}

	if changeCount == 0 {
		take := maxContext
		if take > n {
			take = n
		}
		lines := make([]string, take)
		for i := 0; i < take; i++ {
			lines[i] = hunk.lineAt(i)
		}
		return &diffHunk{
			header:       hunk.header,
			lines:        lines,
			contextLines: take,
			score:        hunk.score,
		}
	}

	estCap := changeCount + changeCount*maxContext*2
	if estCap > n {
		estCap = n
	}

	if n <= 64 {
		return reduceContextSmall(hunk, maxContext, n, estCap)
	}

	keep := make([]bool, n)
	for i := 0; i < n; i++ {
		line := hunk.lineAt(i)
		if len(line) == 0 {
			continue
		}
		c := line[0]
		if c == '+' || c == '-' {
			keep[i] = true
			lo := i - maxContext
			if lo < 0 {
				lo = 0
			}
			for j := lo; j < i; j++ {
				keep[j] = true
			}
			hi := i + maxContext + 1
			if hi > n {
				hi = n
			}
			for j := i + 1; j < hi; j++ {
				keep[j] = true
			}
		} else if c == '\\' {
			keep[i] = true
		}
	}

	newLines := make([]string, 0, estCap)
	var additions, deletions, contextLines int
	for i := 0; i < n; i++ {
		if !keep[i] {
			continue
		}
		line := hunk.lineAt(i)
		newLines = append(newLines, line)
		if len(line) > 0 && line[0] == '+' {
			additions++
		} else if len(line) > 0 && line[0] == '-' {
			deletions++
		} else {
			contextLines++
		}
	}

	return &diffHunk{
		header:       hunk.header,
		lines:        newLines,
		additions:    additions,
		deletions:    deletions,
		contextLines: contextLines,
		score:        hunk.score,
	}
}

func reduceContextSmall(hunk *diffHunk, maxContext, n, estCap int) *diffHunk {
	var keepMask uint64
	for i := 0; i < n; i++ {
		line := hunk.lineAt(i)
		if len(line) == 0 {
			continue
		}
		c := line[0]
		if c == '+' || c == '-' {
			keepMask |= 1 << uint(i)
			lo := i - maxContext
			if lo < 0 {
				lo = 0
			}
			for j := lo; j < i; j++ {
				keepMask |= 1 << uint(j)
			}
			hi := i + maxContext + 1
			if hi > n {
				hi = n
			}
			for j := i + 1; j < hi; j++ {
				keepMask |= 1 << uint(j)
			}
		} else if c == '\\' {
			keepMask |= 1 << uint(i)
		}
	}

	newLines := make([]string, 0, estCap)
	var additions, deletions, contextLines int
	for i := 0; i < n; i++ {
		if keepMask&(1<<uint(i)) == 0 {
			continue
		}
		line := hunk.lineAt(i)
		newLines = append(newLines, line)
		if len(line) > 0 && line[0] == '+' {
			additions++
		} else if len(line) > 0 && line[0] == '-' {
			deletions++
		} else {
			contextLines++
		}
	}

	return &diffHunk{
		header:       hunk.header,
		lines:        newLines,
		additions:    additions,
		deletions:    deletions,
		contextLines: contextLines,
		score:        hunk.score,
	}
}

// ── Output formatter ────────────────────────────────────────────────

func formatOutput(preDiffLines []string, files []*diffFile, filesAffected, totalAdditions, totalDeletions, hunksRemoved int) string {
	// Estimate capacity: count total lines and approximate average line length.
	totalLines := len(preDiffLines)
	for _, f := range files {
		totalLines += 1
		totalLines += len(f.renameLines)
		totalLines += 3
		for _, h := range f.hunks {
			totalLines += 1 + h.lineCount()
		}
	}
	totalLines += 2 // summary footer

	var b strings.Builder
	b.Grow(totalLines * 80) // rough estimate

	for i, l := range preDiffLines {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(l)
	}

	needNewline := len(preDiffLines) > 0
	for _, f := range files {
		if needNewline {
			b.WriteByte('\n')
		}
		b.WriteString(f.header)
		needNewline = true

		for _, l := range f.renameLines {
			b.WriteByte('\n')
			b.WriteString(l)
		}

		if f.isNewFile {
			b.WriteString("\nnew file mode 100644")
		} else if f.isDeletedFile {
			b.WriteString("\ndeleted file mode 100644")
		}

		if f.isBinary {
			b.WriteString("\nBinary files differ")
			continue
		}

		if f.oldFile != "" {
			b.WriteByte('\n')
			b.WriteString(f.oldFile)
		}
		if f.newFile != "" {
			b.WriteByte('\n')
			b.WriteString(f.newFile)
		}

		for _, h := range f.hunks {
			b.WriteByte('\n')
			b.WriteString(h.header)
			nc := h.lineCount()
			for i := 0; i < nc; i++ {
				b.WriteByte('\n')
				b.WriteString(h.lineAt(i))
			}
		}
	}

	// Summary footer.
	if hunksRemoved > 0 || filesAffected > 0 {
		b.WriteByte('\n')
		b.WriteByte('[')
		b.WriteString(strconv.Itoa(filesAffected))
		b.WriteString(" files changed, +")
		b.WriteString(strconv.Itoa(totalAdditions))
		b.WriteString(" -")
		b.WriteString(strconv.Itoa(totalDeletions))
		b.WriteString(" lines")
		if hunksRemoved > 0 {
			b.WriteString(", ")
			b.WriteString(strconv.Itoa(hunksRemoved))
			b.WriteString(" hunks omitted")
		}
		b.WriteByte(']')
	}

	return b.String()
}

// ── Helpers ─────────────────────────────────────────────────────────

func passThroughResult(content string, lineCount int) DiffCompressionResult {
	return DiffCompressionResult{
		Compressed:          content,
		OriginalLineCount:   lineCount,
		CompressedLineCount: lineCount,
	}
}

func countSplitLines(s string) int {
	return strings.Count(s, "\n") + 1
}

func md5Hex24(s string) string {
	h := md5.Sum([]byte(s))
	var dst [32]byte
	hex.Encode(dst[:], h[:])
	return string(dst[:24])
}

