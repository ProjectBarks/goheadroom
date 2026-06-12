package transforms

import (
	"github.com/uber/goheadroom/transforms/contentdetector"
)

// TieredDetectionResult extends DetectionResult with the tier that produced it.
type TieredDetectionResult struct {
	contentdetector.DetectionResult
	Tier int // 1=Magika, 2=unidiff, 3=keyword/content_detector
}

// Detect runs tiered content detection.
// Tier 1: Magika ONNX (if modelPath provided)
// Tier 2: Unidiff parser
// Tier 3: Keyword/content_detector heuristics
// Port of Rust detect().
func Detect(text string, modelPath string) TieredDetectionResult {
	if text == "" {
		return TieredDetectionResult{
			DetectionResult: contentdetector.DetectionResult{
				ContentType: contentdetector.PlainText,
				Confidence:  0.0,
				Metadata:    map[string]interface{}{},
			},
			Tier: 3,
		}
	}

	// Tier 1: try Magika if model path is available
	if modelPath != "" {
		ct, err := contentdetector.MagikaDetect(text, modelPath)
		if err == nil && ct != contentdetector.PlainText {
			return TieredDetectionResult{
				DetectionResult: contentdetector.DetectionResult{
					ContentType: ct,
					Confidence:  0.9,
					Metadata:    map[string]interface{}{"tier": "magika"},
				},
				Tier: 1,
			}
		}
		// On error or PlainText, fall through to Tier 2
	}

	// Tier 2: unidiff parser
	if IsDiff(text) {
		return TieredDetectionResult{
			DetectionResult: contentdetector.DetectionResult{
				ContentType: contentdetector.GitDiff,
				Confidence:  0.95,
				Metadata:    map[string]interface{}{"tier": "unidiff"},
			},
			Tier: 2,
		}
	}

	// Tier 3: keyword/content_detector heuristics
	result := contentdetector.DetectContentType(text)
	return TieredDetectionResult{
		DetectionResult: result,
		Tier:            3,
	}
}
