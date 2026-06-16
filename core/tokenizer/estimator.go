package tokenizer

import "math"

type EstimatingCounter struct {
	charsPerToken float64
}

func NewEstimatingCounter(charsPerToken float64) *EstimatingCounter {
	if charsPerToken <= 0 {
		charsPerToken = 4.0
	}
	return &EstimatingCounter{charsPerToken: charsPerToken}
}

func (e *EstimatingCounter) CountText(text string) int {
	if text == "" {
		return 0
	}
	chars := len([]rune(text))
	return int(math.Ceil(float64(chars) / e.charsPerToken))
}

func (e *EstimatingCounter) Backend() Backend { return BackendEstimator }
