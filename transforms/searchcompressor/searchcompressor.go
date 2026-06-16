// Package searchcompressor compresses grep/ripgrep search output by
// parsing, scoring, selecting matches, and formatting compressed results.
//
// Port of headroom-core/src/transforms/search_compressor.rs.
package searchcompressor

import (
	"crypto/md5"
	"fmt"
	"hash/fnv"
	"sort"
	"strings"

	"github.com/uber/goheadroom/ccr"
	"github.com/uber/goheadroom/signals"
	"github.com/uber/goheadroom/transforms/adaptivesizer"
)

// ── Types ───────────────────────────────────────────────────────────

// SearchMatch is a single grep-style hit.
type SearchMatch struct {
	File       string
	LineNumber uint64
	Content    string
	Score      float32
}

// FileMatches groups all matches under a single file.
type FileMatches struct {
	File    string
	Matches []SearchMatch
}

// TotalScore returns the sum of all match scores.
func (fm *FileMatches) TotalScore() float32 {
	var sum float32
	for _, m := range fm.Matches {
		sum += m.Score
	}
	return sum
}

// ── Config ──────────────────────────────────────────────────────────

// SearchCompressorConfig mirrors Rust SearchCompressorConfig.
type SearchCompressorConfig struct {
	MaxMatchesPerFile          int
	AlwaysKeepFirst            bool
	AlwaysKeepLast             bool
	MaxTotalMatches            int
	MaxFiles                   int
	ContextKeywords            []string
	BoostErrors                bool
	EnableCCR                  bool
	MinMatchesForCCR           int
	MinCompressionRatioForCCR  float64
}

// DefaultConfig returns defaults matching Rust/Python.
func DefaultConfig() SearchCompressorConfig {
	return SearchCompressorConfig{
		MaxMatchesPerFile:         5,
		AlwaysKeepFirst:           true,
		AlwaysKeepLast:            true,
		MaxTotalMatches:           30,
		MaxFiles:                  15,
		BoostErrors:               true,
		EnableCCR:                 true,
		MinMatchesForCCR:          10,
		MinCompressionRatioForCCR: 0.8,
	}
}

// ── Result types ────────────────────────────────────────────────────

// SearchCompressionResult is the compression result.
type SearchCompressionResult struct {
	Compressed           string
	Original             string
	OriginalMatchCount   int
	CompressedMatchCount int
	FilesAffected        int
	CompressionRatio     float64
	CacheKey             *string
	Summaries            map[string]string
}

// SearchCompressorStats carries sidecar diagnostics.
type SearchCompressorStats struct {
	LinesScanned                 int
	LinesUnparsed                int
	FilesDropped                 int
	MatchesDroppedByPerFileCap   int
	MatchesDroppedByGlobalCap    int
	CCREmitted                   bool
	CCRSkipReason                *string
}

// ── Compressor ──────────────────────────────────────────────────────

// SearchCompressor compresses grep/ripgrep search output.
type SearchCompressor struct {
	config     SearchCompressorConfig
	importance signals.LineImportanceDetector
}

// New creates a SearchCompressor with the default KeywordDetector.
func New(config SearchCompressorConfig) *SearchCompressor {
	return &SearchCompressor{
		config:     config,
		importance: signals.NewKeywordDetector(),
	}
}

// NewWithDetector creates a SearchCompressor with a custom detector.
func NewWithDetector(config SearchCompressorConfig, detector signals.LineImportanceDetector) *SearchCompressor {
	return &SearchCompressor{
		config:     config,
		importance: detector,
	}
}

// Compress compresses without persisting CCR.
func (sc *SearchCompressor) Compress(content, context string, bias float64) (SearchCompressionResult, SearchCompressorStats) {
	return sc.CompressWithStore(content, context, bias, nil)
}

// CompressWithStore compresses with optional CCR persistence.
func (sc *SearchCompressor) CompressWithStore(content, context string, bias float64, store ccr.CcrStore) (SearchCompressionResult, SearchCompressorStats) {
	var stats SearchCompressorStats
	parsed := sc.parseSearchResults(content, &stats)

	if len(parsed) == 0 {
		return SearchCompressionResult{
			Compressed:       content,
			Original:         content,
			CompressionRatio: 1.0,
			Summaries:        make(map[string]string),
		}, stats
	}

	originalCount := 0
	for _, fm := range parsed {
		originalCount += len(fm.Matches)
	}

	sc.scoreMatches(parsed, context)
	selected := sc.selectMatches(parsed, bias, &stats)

	compressedBody, summaries := sc.formatOutput(selected, parsed)
	compressedCount := 0
	for _, fm := range selected {
		compressedCount += len(fm.Matches)
	}

	maxLen := len(content)
	if maxLen == 0 {
		maxLen = 1
	}
	ratio := float64(len(compressedBody)) / float64(maxLen)

	compressed := compressedBody
	var cacheKey *string
	if sc.config.EnableCCR {
		if originalCount < sc.config.MinMatchesForCCR {
			reason := "below min_matches_for_ccr"
			stats.CCRSkipReason = &reason
		} else if ratio >= sc.config.MinCompressionRatioForCCR {
			reason := "compression ratio too high"
			stats.CCRSkipReason = &reason
		} else {
			key := md5Hex24(content)
			marker := fmt.Sprintf("\n[%d matches compressed to %d. Retrieve more: hash=%s]",
				originalCount, compressedCount, key)
			compressed += marker
			if store != nil {
				store.Put(key, []byte(content))
			}
			cacheKey = &key
			stats.CCREmitted = true
		}
	} else {
		reason := "ccr disabled in config"
		stats.CCRSkipReason = &reason
	}

	result := SearchCompressionResult{
		Compressed:           compressed,
		Original:             content,
		OriginalMatchCount:   originalCount,
		CompressedMatchCount: compressedCount,
		FilesAffected:        len(parsed),
		CompressionRatio:     ratio,
		CacheKey:             cacheKey,
		Summaries:            summaries,
	}
	return result, stats
}

// ── Parser ──────────────────────────────────────────────────────────

func (sc *SearchCompressor) parseSearchResults(content string, stats *SearchCompressorStats) map[string]*FileMatches {
	out := make(map[string]*FileMatches)
	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		stats.LinesScanned++
		file, lineNo, body, ok := parseMatchLine(line)
		if !ok {
			stats.LinesUnparsed++
			continue
		}
		fm, exists := out[file]
		if !exists {
			fm = &FileMatches{File: file}
			out[file] = fm
		}
		fm.Matches = append(fm.Matches, SearchMatch{
			File:       file,
			LineNumber: lineNo,
			Content:    body,
		})
	}
	return out
}

func parseMatchLine(line string) (file string, lineNo uint64, content string, ok bool) {
	bytes := []byte(line)
	// Windows drive prefix detection.
	scanStart := 0
	if len(bytes) >= 3 && isAlpha(bytes[0]) && bytes[1] == ':' && (bytes[2] == '\\' || bytes[2] == '/') {
		scanStart = 2
	}

	for i := scanStart; i < len(bytes); i++ {
		if bytes[i] != ':' && bytes[i] != '-' {
			continue
		}
		// Reject adjacent separators.
		if i > 0 && (bytes[i-1] == ':' || bytes[i-1] == '-') {
			continue
		}
		// Try digits.
		digitsStart := i + 1
		j := digitsStart
		for j < len(bytes) && bytes[j] >= '0' && bytes[j] <= '9' {
			j++
		}
		if j > digitsStart && j < len(bytes) && (bytes[j] == ':' || bytes[j] == '-') {
			if i == 0 {
				return "", 0, "", false
			}
			file = line[:i]
			n := uint64(0)
			for _, b := range bytes[digitsStart:j] {
				n = n*10 + uint64(b-'0')
			}
			content = line[j+1:]
			return file, n, content, true
		}
	}
	return "", 0, "", false
}

func isAlpha(b byte) bool {
	return (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z')
}

// ── Scoring ─────────────────────────────────────────────────────────

func (sc *SearchCompressor) scoreMatches(files map[string]*FileMatches, context string) {
	contextLower := strings.ToLower(context)
	contextWords := make([]string, 0)
	for _, w := range strings.Fields(contextLower) {
		if len(w) > 2 {
			contextWords = append(contextWords, w)
		}
	}

	for _, fm := range files {
		for i := range fm.Matches {
			m := &fm.Matches[i]
			var score float32
			contentLower := strings.ToLower(m.Content)

			for _, w := range contextWords {
				if strings.Contains(contentLower, w) {
					score += 0.3
				}
			}

			if sc.config.BoostErrors {
				signal := sc.importance.DetectImportance(m.Content, signals.ImportanceContextSearch)
				if signal.HasMatch {
					var bump float32
					switch signal.Category {
					case signals.ImportanceCategoryError:
						bump = 0.5
					case signals.ImportanceCategoryWarning:
						bump = 0.4
					case signals.ImportanceCategoryImportance:
						bump = 0.3
					}
					score += bump
				}
			}

			for _, kw := range sc.config.ContextKeywords {
				if strings.Contains(contentLower, strings.ToLower(kw)) {
					score += 0.4
				}
			}

			if score > 1.0 {
				score = 1.0
			}
			m.Score = score
		}
	}
}

// ── Selection ───────────────────────────────────────────────────────

func (sc *SearchCompressor) selectMatches(files map[string]*FileMatches, bias float64, stats *SearchCompressorStats) map[string]*FileMatches {
	// Sort files by total score desc.
	type fileEntry struct {
		name string
		fm   *FileMatches
	}
	sorted := make([]fileEntry, 0, len(files))
	for name, fm := range files {
		sorted = append(sorted, fileEntry{name, fm})
	}
	sort.SliceStable(sorted, func(i, j int) bool {
		si, sj := sorted[i].fm.TotalScore(), sorted[j].fm.TotalScore()
		if si != sj {
			return si > sj
		}
		return sorted[i].name < sorted[j].name // deterministic tiebreak
	})

	if len(sorted) > sc.config.MaxFiles {
		stats.FilesDropped += len(sorted) - sc.config.MaxFiles
		sorted = sorted[:sc.config.MaxFiles]
	}

	// Build global match list for adaptive sizer.
	var allMatchStrings []string
	for _, fe := range sorted {
		for _, m := range fe.fm.Matches {
			allMatchStrings = append(allMatchStrings, fmt.Sprintf("%s:%d:%s", fe.name, m.LineNumber, m.Content))
		}
	}
	adaptiveTotal := adaptivesizer.ComputeOptimalK(allMatchStrings, bias, 5, &sc.config.MaxTotalMatches)

	selected := make(map[string]*FileMatches)
	totalSelected := 0

	for _, fe := range sorted {
		if totalSelected >= adaptiveTotal {
			stats.MatchesDroppedByGlobalCap += len(fe.fm.Matches)
			continue
		}

		// Sort by score desc, ties by line number asc.
		sortedMatches := make([]SearchMatch, len(fe.fm.Matches))
		copy(sortedMatches, fe.fm.Matches)
		sort.SliceStable(sortedMatches, func(i, j int) bool {
			if sortedMatches[i].Score != sortedMatches[j].Score {
				return sortedMatches[i].Score > sortedMatches[j].Score
			}
			return sortedMatches[i].LineNumber < sortedMatches[j].LineNumber
		})

		remainingCap := sc.config.MaxMatchesPerFile
		if ar := adaptiveTotal - totalSelected; ar < remainingCap {
			remainingCap = ar
		}

		var fileSelected []SearchMatch
		seen := make(map[[2]uint64]bool)

		pushUnique := func(m *SearchMatch) bool {
			key := [2]uint64{m.LineNumber, hashU64(m.Content)}
			if seen[key] {
				return false
			}
			seen[key] = true
			fileSelected = append(fileSelected, *m)
			return true
		}

		if sc.config.AlwaysKeepFirst && len(fe.fm.Matches) > 0 {
			if len(fileSelected) < remainingCap {
				pushUnique(&fe.fm.Matches[0])
			}
		}
		if sc.config.AlwaysKeepLast && len(fe.fm.Matches) > 1 {
			if len(fileSelected) < remainingCap {
				pushUnique(&fe.fm.Matches[len(fe.fm.Matches)-1])
			}
		}

		for i := range sortedMatches {
			if len(fileSelected) >= remainingCap {
				break
			}
			pushUnique(&sortedMatches[i])
		}

		// Restore line order.
		sort.SliceStable(fileSelected, func(i, j int) bool {
			return fileSelected[i].LineNumber < fileSelected[j].LineNumber
		})

		dropped := len(fe.fm.Matches) - len(fileSelected)
		stats.MatchesDroppedByPerFileCap += dropped
		totalSelected += len(fileSelected)
		selected[fe.name] = &FileMatches{
			File:    fe.name,
			Matches: fileSelected,
		}
	}

	return selected
}

// ── Formatting ──────────────────────────────────────────────────────

func (sc *SearchCompressor) formatOutput(selected, original map[string]*FileMatches) (string, map[string]string) {
	var lines []string
	summaries := make(map[string]string)

	// Sort selected file names for deterministic output.
	fileNames := make([]string, 0, len(selected))
	for name := range selected {
		fileNames = append(fileNames, name)
	}
	sort.Strings(fileNames)

	for _, file := range fileNames {
		fm := selected[file]
		for _, m := range fm.Matches {
			lines = append(lines, fmt.Sprintf("%s:%d:%s", m.File, m.LineNumber, m.Content))
		}
		if origFM, ok := original[file]; ok {
			if len(origFM.Matches) > len(fm.Matches) {
				omitted := len(origFM.Matches) - len(fm.Matches)
				summary := fmt.Sprintf("[... and %d more matches in %s]", omitted, file)
				lines = append(lines, summary)
				summaries[file] = summary
			}
		}
	}

	return strings.Join(lines, "\n"), summaries
}

// ── Helpers ─────────────────────────────────────────────────────────

func hashU64(s string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(s))
	return h.Sum64()
}

func md5Hex24(s string) string {
	h := md5.Sum([]byte(s))
	hex := fmt.Sprintf("%x", h)
	return hex[:24]
}
