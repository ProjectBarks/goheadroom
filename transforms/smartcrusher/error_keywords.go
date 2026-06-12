package smartcrusher

// ErrorKeywords is the canonical set of error/failure keywords for item preservation.
// Matches Python's ERROR_KEYWORDS from error_detection.py.
var ErrorKeywords = []string{
	"error",
	"exception",
	"failed",
	"failure",
	"critical",
	"fatal",
	"crash",
	"panic",
	"abort",
	"timeout",
	"denied",
	"rejected",
}
