// Package diffcompressor compresses unified-diff output by parsing into
// files + hunks, capping file and hunk counts, trimming context lines,
// and optionally emitting a CCR cache key.
//
// Port of headroom-core/src/transforms/diff_compressor.rs.
package diffcompressor

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/uber/goheadroom/ccr"
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

	lines := splitLines(content)
	originalLineCount := len(lines)
	stats.InputLines = originalLineCount

	// Short-circuit 1: input below CCR threshold -> pass through unchanged.
	if originalLineCount < dc.config.MinLinesForCCR {
		stats.OutputLines = originalLineCount
		stats.CompressionRatio = 1.0
		reason := "input below min_lines_for_ccr"
		stats.CCRSkippedReason = &reason
		return passThroughResult(content, originalLineCount), stats
	}

	// Parse the unified diff.
	parsed := parseDiff(lines)
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
			stats.FilesDropped = append(stats.FilesDropped, fmt.Sprintf("%s -> %s", f.oldFile, f.newFile))
		}
		diffFiles = diffFiles[:dc.config.MaxFiles]
	}
	stats.FilesKept = len(diffFiles)

	// Capture lossy-emit signals.
	for _, file := range diffFiles {
		label := fmt.Sprintf("%s -> %s", file.oldFile, file.newFile)
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
		fileLabel := fmt.Sprintf("%s -> %s", file.oldFile, file.newFile)

		selected, dropped := selectHunks(file.hunks, dc.config.MaxHunksPerFile)
		droppedCount := len(dropped)
		if droppedCount > 0 {
			stats.HunksDroppedPerFile[fileLabel] = droppedCount
			for _, h := range dropped {
				if len(h.lines) > largestDropped {
					largestDropped = len(h.lines)
				}
			}
		}

		// Trim context inside each kept hunk.
		compressedHunks := make([]*diffHunk, 0, len(selected))
		for _, hunk := range selected {
			trimmed := reduceContext(hunk, dc.config.MaxContextLines)
			if len(trimmed.lines) > largestKept {
				largestKept = len(trimmed.lines)
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
		reason := fmt.Sprintf("compression ratio %.3f above threshold %.3f", ratio, savingsThreshold)
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
	additions    int
	deletions    int
	contextLines int
	score        float64
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

// ── Regex helpers ───────────────────────────────────────────────────

var (
	hunkHeaderRe    = regexp.MustCompile(`^(?:@@ -\d+(?:,\d+)? \+\d+(?:,\d+)? @@|@@@ -\d+(?:,\d+)? -\d+(?:,\d+)? \+\d+(?:,\d+)? @@@|@@@@ -\d+(?:,\d+)? -\d+(?:,\d+)? -\d+(?:,\d+)? \+\d+(?:,\d+)? @@@@)(.*)$`)
	hunkNewRangeRe  = regexp.MustCompile(`\+(\d+)`)
	diffGitRe       = regexp.MustCompile(`^diff --git a/(.+) b/(.+)$`)
	diffCombinedRe  = regexp.MustCompile(`^diff --combined (.+)$`)
	diffCCRe        = regexp.MustCompile(`^diff --cc (.+)$`)
	oldFileRe       = regexp.MustCompile(`^--- (a/(.+)|/dev/null)$`)
	newFileRe       = regexp.MustCompile(`^\+\+\+ (b/(.+)|/dev/null)$`)
	binaryRe        = regexp.MustCompile(`^Binary files .+ differ$`)

	priorityPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b(error|exception|fail(?:ed|ure)?|fatal|critical|crash|panic)\b`),
		regexp.MustCompile(`(?i)\b(important|note|todo|fixme|hack|xxx|bug|fix)\b`),
		regexp.MustCompile(`(?i)\b(security|auth|password|secret|token)\b`),
	}
)

func isDiffHeader(line string) bool {
	return diffGitRe.MatchString(line) || diffCombinedRe.MatchString(line) || diffCCRe.MatchString(line)
}

// ── Parser ──────────────────────────────────────────────────────────

type parsedDiff struct {
	preDiffLines  []string
	files         []*diffFile
	parseWarnings []string
}

func parseDiff(lines []string) *parsedDiff {
	var files []*diffFile
	var currentFile *diffFile
	var currentHunk *diffHunk
	var preDiffLines []string
	var warnings []string

	for _, line := range lines {
		if isDiffHeader(line) {
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
			}
			continue
		}

		if currentFile == nil {
			preDiffLines = append(preDiffLines, line)
			continue
		}

		// File-level markers.
		if strings.HasPrefix(line, "new file mode") {
			currentFile.isNewFile = true
			s := line
			currentFile.originalNewFileModeLine = &s
		} else if strings.HasPrefix(line, "deleted file mode") {
			currentFile.isDeletedFile = true
			s := line
			currentFile.originalDeletedFileModeLine = &s
		} else if strings.HasPrefix(line, "rename ") || strings.HasPrefix(line, "similarity ") ||
			strings.HasPrefix(line, "copy ") || strings.HasPrefix(line, "dissimilarity ") {
			currentFile.isRenamed = true
			currentFile.renameLines = append(currentFile.renameLines, line)
		} else if binaryRe.MatchString(line) {
			currentFile.isBinary = true
			s := line
			currentFile.originalBinaryLine = &s
		}

		// --- a/file
		if oldFileRe.MatchString(line) {
			currentFile.oldFile = line
			continue
		}

		// +++ b/file
		if newFileRe.MatchString(line) {
			currentFile.newFile = line
			continue
		}

		// Hunk header.
		if hunkHeaderRe.MatchString(line) {
			if currentHunk != nil {
				if currentFile != nil {
					currentFile.hunks = append(currentFile.hunks, currentHunk)
				}
			}
			currentHunk = &diffHunk{
				header: line,
			}
			continue
		}

		// Hunk content.
		if currentHunk != nil {
			if len(line) > 0 && line[0] == '+' && !strings.HasPrefix(line, "+++") {
				currentHunk.additions++
				currentHunk.lines = append(currentHunk.lines, line)
			} else if len(line) > 0 && line[0] == '-' && !strings.HasPrefix(line, "---") {
				currentHunk.deletions++
				currentHunk.lines = append(currentHunk.lines, line)
			} else if (len(line) > 0 && line[0] == ' ') || line == "" {
				currentHunk.contextLines++
				currentHunk.lines = append(currentHunk.lines, line)
			} else {
				// "Other" line: `\ No newline at end of file`, etc.
				currentHunk.lines = append(currentHunk.lines, line)
			}
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

func scoreHunks(files []*diffFile, context string) {
	contextLower := strings.ToLower(context)
	contextWords := strings.Fields(contextLower)

	for _, file := range files {
		for _, hunk := range file.hunks {
			score := float64(hunk.additions+hunk.deletions) * ScoreChangeDensityWeight
			if score > ScoreChangeDensityCap {
				score = ScoreChangeDensityCap
			}

			// Build lowered hunk content once using a builder instead of
			// strings.Join + strings.ToLower which allocates two large strings.
			var b strings.Builder
			totalLen := 0
			for _, l := range hunk.lines {
				totalLen += len(l) + 1
			}
			b.Grow(totalLen)
			for i, l := range hunk.lines {
				if i > 0 {
					b.WriteByte('\n')
				}
				b.WriteString(strings.ToLower(l))
			}
			hunkContentLower := b.String()

			for _, word := range contextWords {
				if len(word) > ScoreContextMinWordLen && strings.Contains(hunkContentLower, word) {
					score += ScoreContextWordWeight
				}
			}

			for _, pat := range priorityPatterns {
				if pat.MatchString(hunkContentLower) {
					score += ScorePriorityPatternBoost
					break
				}
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
	m := hunkNewRangeRe.FindStringSubmatch(header)
	if m != nil {
		n, _ := strconv.Atoi(m[1])
		return n
	}
	return 0
}

// ── Context trimming ────────────────────────────────────────────────

func reduceContext(hunk *diffHunk, maxContext int) *diffHunk {
	// Find indices of +/- lines.
	var changePositions []int
	for i, line := range hunk.lines {
		if len(line) > 0 && (line[0] == '+' || line[0] == '-') {
			changePositions = append(changePositions, i)
		}
	}

	if len(changePositions) == 0 {
		// No changes -> keep up to maxContext leading lines.
		take := maxContext
		if take > len(hunk.lines) {
			take = len(hunk.lines)
		}
		lines := make([]string, take)
		copy(lines, hunk.lines[:take])
		return &diffHunk{
			header:       hunk.header,
			lines:        lines,
			contextLines: take,
			score:        hunk.score,
		}
	}

	// Build bitset of indices to keep (avoids map allocation).
	n := len(hunk.lines)
	keep := make([]bool, n)
	for _, pos := range changePositions {
		keep[pos] = true
		lo := pos - maxContext
		if lo < 0 {
			lo = 0
		}
		for i := lo; i < pos; i++ {
			keep[i] = true
		}
		hi := pos + maxContext + 1
		if hi > n {
			hi = n
		}
		for i := pos + 1; i < hi; i++ {
			keep[i] = true
		}
	}

	// Always keep `\ No newline at end of file` markers.
	for i, line := range hunk.lines {
		if len(line) > 0 && line[0] == '\\' {
			keep[i] = true
		}
	}

	// Collect kept lines in order.
	newLines := make([]string, 0, len(changePositions)*3)
	var additions, deletions, contextLines int
	for i := 0; i < n; i++ {
		if !keep[i] {
			continue
		}
		line := hunk.lines[i]
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
		totalLines += 1 // header
		totalLines += len(f.renameLines)
		totalLines += 3 // mode/binary/old/new file lines
		for _, h := range f.hunks {
			totalLines += 1 + len(h.lines)
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
			for _, l := range h.lines {
				b.WriteByte('\n')
				b.WriteString(l)
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
	dst := make([]byte, hex.EncodedLen(len(h)))
	hex.Encode(dst, h[:])
	return string(dst[:24])
}

// splitLines splits content on newlines, returning substrings of the
// original string (no copies). This is the same as strings.Split but
// avoids allocating new string headers for every line -- the returned
// strings share the backing array of content.
func splitLines(content string) []string {
	n := strings.Count(content, "\n") + 1
	lines := make([]string, 0, n)
	for {
		idx := strings.IndexByte(content, '\n')
		if idx < 0 {
			lines = append(lines, content)
			break
		}
		lines = append(lines, content[:idx])
		content = content[idx+1:]
	}
	return lines
}
