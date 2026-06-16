package smartcrusher

import (
	"regexp"
	"strings"
)

// Regex patterns for query anchor extraction.
// Direct port of Python smart_crusher.py:85-93.
var (
	uuidPattern        = regexp.MustCompile(`\b[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}\b`)
	numericIDPattern   = regexp.MustCompile(`\b\d{4,}\b`)
	hostnamePattern    = regexp.MustCompile(`\b[a-zA-Z0-9][-a-zA-Z0-9]*\.[a-zA-Z0-9][-a-zA-Z0-9]*(?:\.[a-zA-Z]{2,})?\b`)
	quotedStrPattern   = regexp.MustCompile(`['"]([^'"]{1,50})['"]`)
	emailPattern       = regexp.MustCompile(`\b[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Z|a-z]{2,}\b`)
	hostnameFalsePos   = map[string]bool{"e.g": true, "i.e": true, "etc.": true}
)

// ExtractQueryAnchors extracts query anchors from user text.
// Returns a set of lowercased anchor strings.
// Port of Python extract_query_anchors (smart_crusher.py:99-135).
func ExtractQueryAnchors(text string) map[string]bool {
	anchors := make(map[string]bool)
	if text == "" {
		return anchors
	}

	// UUIDs.
	for _, m := range uuidPattern.FindAllString(text, -1) {
		anchors[strings.ToLower(m)] = true
	}

	// Numeric IDs (4+ digits).
	for _, m := range numericIDPattern.FindAllString(text, -1) {
		anchors[m] = true
	}

	// Hostnames.
	for _, m := range hostnamePattern.FindAllString(text, -1) {
		lc := strings.ToLower(m)
		if !hostnameFalsePos[lc] {
			anchors[lc] = true
		}
	}

	// Quoted strings.
	for _, sub := range quotedStrPattern.FindAllStringSubmatch(text, -1) {
		if len(sub) > 1 && len(strings.TrimSpace(sub[1])) >= 2 {
			anchors[strings.ToLower(sub[1])] = true
		}
	}

	// Emails.
	for _, m := range emailPattern.FindAllString(text, -1) {
		anchors[strings.ToLower(m)] = true
	}

	return anchors
}

// ItemMatchesAnchors checks if a JSON item (as raw string) matches any query anchors.
// Port of Python item_matches_anchors (smart_crusher.py:152-168).
// Uses lowercased string comparison.
func ItemMatchesAnchors(itemStr string, anchors map[string]bool) bool {
	if len(anchors) == 0 {
		return false
	}
	lower := strings.ToLower(itemStr)
	for a := range anchors {
		if strings.Contains(lower, a) {
			return true
		}
	}
	return false
}
