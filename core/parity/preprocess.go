package parity

import "regexp"

var ccrMarkerRe = regexp.MustCompile(`,?\{"_ccr_dropped":"<<ccr:[^>]+>>"\}`)

func StripCCRMarkers(b []byte) []byte {
	return ccrMarkerRe.ReplaceAll(b, nil)
}
