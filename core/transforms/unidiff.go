// Package transforms provides content detection and transformation helpers
// for the headroom compression pipeline.
package transforms

import (
	"regexp"
	"strings"
)

// DiffResult holds parsed diff metadata.
type DiffResult struct {
	IsDiff    bool
	FileCount int
	Additions int
	Deletions int
}

// Compiled patterns for diff detection.
var (
	diffFileHeaderRe = regexp.MustCompile(`^---\s+\S+`)
	diffFileNewRe    = regexp.MustCompile(`^\+\+\+\s+\S+`)
	diffHunkRe       = regexp.MustCompile(`^@@\s+`)
	diffGitHeaderRe  = regexp.MustCompile(`^diff --git\s+`)
	diffBinaryRe     = regexp.MustCompile(`^Binary files .+ differ$`)
)

// IsDiff returns true if the text appears to be a unified diff.
// Uses heuristic parsing matching the Rust unidiff_detector behavior:
// requires at least one file with at least one hunk.
func IsDiff(text string) bool {
	if text == "" {
		return false
	}
	result, _ := DetectDiff(text)
	return result.IsDiff
}

// DetectDiff parses text as a unified diff and returns metadata.
// Port of the Rust unidiff_detector logic using line-by-line parsing.
func DetectDiff(text string) (DiffResult, error) {
	result := DiffResult{}
	if text == "" {
		return result, nil
	}

	lines := strings.Split(text, "\n")

	type fileInfo struct {
		hasHunk bool
		adds    int
		dels    int
	}

	var files []fileInfo
	var currentFile *fileInfo
	inHunk := false
	hasMinus := false

	for _, line := range lines {
		// Git diff header starts a new file
		if diffGitHeaderRe.MatchString(line) {
			if currentFile != nil && currentFile.hasHunk {
				files = append(files, *currentFile)
			}
			f := fileInfo{}
			currentFile = &f
			inHunk = false
			hasMinus = false
			continue
		}

		// Binary file marker
		if diffBinaryRe.MatchString(line) {
			if currentFile == nil {
				f := fileInfo{hasHunk: true}
				currentFile = &f
			} else {
				currentFile.hasHunk = true
			}
			continue
		}

		// --- header: starts a new file section.
		// Can appear even inside a hunk when multiple files are concatenated.
		if diffFileHeaderRe.MatchString(line) {
			// If we had a previous file with hunks, save it
			if currentFile != nil && currentFile.hasHunk {
				files = append(files, *currentFile)
				currentFile = nil
			}
			hasMinus = true
			inHunk = false
			continue
		}

		// +++ header (only after ---)
		if diffFileNewRe.MatchString(line) && hasMinus {
			if currentFile == nil {
				f := fileInfo{}
				currentFile = &f
			}
			inHunk = false
			continue
		}

		// Hunk header
		if diffHunkRe.MatchString(line) {
			if currentFile == nil {
				// Hunk without git header but after --- and +++ is valid
				f := fileInfo{}
				currentFile = &f
			}
			currentFile.hasHunk = true
			inHunk = true
			continue
		}

		// Count additions and deletions inside hunks
		if inHunk && currentFile != nil {
			if len(line) > 0 {
				switch line[0] {
				case '+':
					currentFile.adds++
				case '-':
					currentFile.dels++
				case ' ':
					// context line
				default:
					// End of hunk content if we hit something else
					// (but we keep inHunk true in case more hunks follow)
				}
			}
		}
	}

	// Don't forget the last file
	if currentFile != nil && currentFile.hasHunk {
		files = append(files, *currentFile)
	}

	if len(files) == 0 {
		return result, nil
	}

	result.IsDiff = true
	result.FileCount = len(files)
	for _, f := range files {
		result.Additions += f.adds
		result.Deletions += f.dels
	}

	return result, nil
}
