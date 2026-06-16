//go:build onnx

package kompress

import (
	"fmt"
	"strings"
	"sync"

	"github.com/daulet/tokenizers"
	ort "github.com/yalue/onnxruntime_go"
)

var (
	initOnce    sync.Once
	initErr     error
	ortEnvReady bool
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
	initOnce.Do(func() {
		initErr = ort.InitializeEnvironment()
		if initErr == nil {
			ortEnvReady = true
		}
	})
	if initErr != nil {
		return nil, fmt.Errorf("ort init: %w", initErr)
	}

	tok, err := tokenizers.FromFile(tokenizerPath)
	if err != nil {
		return nil, fmt.Errorf("tokenizer load: %w", err)
	}
	defer tok.Close()

	words := strings.Fields(text)
	encoding, err := tok.Encode(strings.Join(words, " "), true)
	if err != nil {
		return nil, fmt.Errorf("encode: %w", err)
	}

	seqLen := len(encoding.IDs)
	inputIDs := make([]int64, seqLen)
	attentionMask := make([]int64, seqLen)
	for i, id := range encoding.IDs {
		inputIDs[i] = int64(id)
		attentionMask[i] = int64(encoding.AttentionMask[i])
	}

	inputShape := ort.Shape{1, int64(seqLen)}
	idsTensor, err := ort.NewTensor(inputShape, inputIDs)
	if err != nil {
		return nil, fmt.Errorf("ids tensor: %w", err)
	}
	defer idsTensor.Destroy()

	maskTensor, err := ort.NewTensor(inputShape, attentionMask)
	if err != nil {
		return nil, fmt.Errorf("mask tensor: %w", err)
	}
	defer maskTensor.Destroy()

	outputTensor, err := ort.NewEmptyTensor[float32](inputShape)
	if err != nil {
		return nil, fmt.Errorf("output tensor: %w", err)
	}
	defer outputTensor.Destroy()

	session, err := ort.NewAdvancedSession(modelPath,
		[]string{"input_ids", "attention_mask"},
		[]string{"final_scores"},
		[]ort.ArbitraryTensor{idsTensor, maskTensor},
		[]ort.ArbitraryTensor{outputTensor},
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("session: %w", err)
	}
	defer session.Destroy()

	if err := session.Run(); err != nil {
		return nil, fmt.Errorf("run: %w", err)
	}

	scores := outputTensor.GetData()

	wordScores := make([]float32, len(words))
	wordIDs := encoding.WordIDs
	for tokenIdx, wordIdx := range wordIDs {
		if wordIdx < 0 || wordIdx >= len(words) {
			continue
		}
		if tokenIdx < len(scores) && scores[tokenIdx] > wordScores[wordIdx] {
			wordScores[wordIdx] = scores[tokenIdx]
		}
	}

	keepMask := make([]bool, len(words))
	for i, s := range wordScores {
		keepMask[i] = s > 0.5
	}

	return &TokenScores{
		Words:    words,
		Scores:   wordScores,
		KeepMask: keepMask,
	}, nil
}

func Available() bool { return true }
