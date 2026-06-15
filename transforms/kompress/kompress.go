//go:build !onnx

package kompress

import (
	"fmt"
	"strings"
)

type TokenScores struct {
	Words    []string
	Scores   []float32
	KeepMask []bool
}

func (ts *TokenScores) Compressed() string {
	var kept []string
	for i, w := range ts.Words {
		if ts.KeepMask[i] {
			kept = append(kept, w)
		}
	}
	return strings.Join(kept, " ")
}

func ScoreText(text string, modelPath string, tokenizerPath string) (*TokenScores, error) {
	return nil, fmt.Errorf("kompress requires ONNX runtime (build with -tags onnx)")
}

func Available() bool { return false }
