//go:build cgo

package tokenizer

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	hftokenizers "github.com/daulet/tokenizers"
)

type ffiEncoding struct {
	pattern       string
	specialTokens map[string]int
	modelFile     string
}

var ffiEncodings = map[string]ffiEncoding{
	"o200k_base": {
		pattern: `[^\r\n\p{L}\p{N}]?[\p{Lu}\p{Lt}\p{Lm}\p{Lo}\p{M}]*[\p{Ll}\p{Lm}\p{Lo}\p{M}]+(?i:'s|'t|'re|'ve|'m|'ll|'d)?|[^\r\n\p{L}\p{N}]?[\p{Lu}\p{Lt}\p{Lm}\p{Lo}\p{M}]+[\p{Ll}\p{Lm}\p{Lo}\p{M}]*(?i:'s|'t|'re|'ve|'m|'ll|'d)?|\p{N}{1,3}| ?[^\s\p{L}\p{N}]+[\r\n/]*|\s*[\r\n]+|\s+(?!\S)|\s+`,
		specialTokens: map[string]int{
			"<|endoftext|>":   199999,
			"<|endofprompt|>": 200018,
		},
		modelFile: "o200k_base.tiktoken",
	},
	"cl100k_base": {
		pattern: `(?i:'s|'t|'re|'ve|'m|'ll|'d)|[^\r\n\p{L}\p{N}]?\p{L}+|\p{N}{1,3}| ?[^\s\p{L}\p{N}]+[\r\n]*|\s*[\r\n]+|\s+(?!\S)|\s+`,
		specialTokens: map[string]int{
			"<|endoftext|>":   100257,
			"<|fim_prefix|>":  100258,
			"<|fim_middle|>":  100259,
			"<|fim_suffix|>":  100260,
			"<|endofprompt|>": 100276,
		},
		modelFile: "cl100k_base.tiktoken",
	},
	"p50k_base": {
		pattern: `'s|'t|'re|'ve|'m|'ll|'d| ?\p{L}+| ?\p{N}+| ?[^\s\p{L}\p{N}]+|\s+(?!\S)|\s+`,
		specialTokens: map[string]int{
			"<|endoftext|>": 50256,
		},
		modelFile: "p50k_base.tiktoken",
	},
	"r50k_base": {
		pattern: `'s|'t|'re|'ve|'m|'ll|'d| ?\p{L}+| ?\p{N}+| ?[^\s\p{L}\p{N}]+|\s+(?!\S)|\s+`,
		specialTokens: map[string]int{
			"<|endoftext|>": 50256,
		},
		modelFile: "r50k_base.tiktoken",
	},
}

var (
	ffiCache   = make(map[string]*hftokenizers.Tokenizer)
	ffiCacheMu sync.Mutex
)

type TiktokenFFICounter struct {
	model        string
	encodingName string
	tok          *hftokenizers.Tokenizer
}

func findDataDir() string {
	_, thisFile, _, ok := runtime.Caller(0)
	if ok {
		d := filepath.Join(filepath.Dir(thisFile), "data")
		if info, err := os.Stat(d); err == nil && info.IsDir() {
			return d
		}
	}
	candidates := []string{
		"tokenizer/data",
		"data",
	}
	for _, c := range candidates {
		if info, err := os.Stat(c); err == nil && info.IsDir() {
			abs, _ := filepath.Abs(c)
			return abs
		}
	}
	return ""
}

var dataDir = findDataDir()

func writeConfigJSON(enc ffiEncoding) (string, error) {
	f, err := os.CreateTemp("", "tiktoken-config-*.json")
	if err != nil {
		return "", err
	}
	defer f.Close()

	f.WriteString(`{"added_tokens_decoder":{`)
	first := true
	for token, id := range enc.specialTokens {
		if !first {
			f.WriteString(",")
		}
		first = false
		fmt.Fprintf(f, `"%d":{"content":"%s","lstrip":false,"normalized":false,"rstrip":false,"single_word":false,"special":true}`, id, token)
	}
	f.WriteString(`},"bos_token":"<|endoftext|>","eos_token":"<|endoftext|>","model_max_length":1000000000000000000000,"tokenizer_class":"GPT2TokenizerFast"}`)
	return f.Name(), nil
}

func newTiktokenFFI(model string) (*TiktokenFFICounter, error) {
	encName, err := encodingFor(model)
	if err != nil {
		return nil, err
	}

	ffiCacheMu.Lock()
	defer ffiCacheMu.Unlock()

	if tok, ok := ffiCache[encName]; ok {
		return &TiktokenFFICounter{model: model, encodingName: encName, tok: tok}, nil
	}

	enc, ok := ffiEncodings[encName]
	if !ok {
		return nil, fmt.Errorf("tiktoken FFI: unsupported encoding %q", encName)
	}

	if dataDir == "" {
		return nil, fmt.Errorf("tiktoken FFI: data directory not found")
	}

	modelPath := filepath.Join(dataDir, enc.modelFile)
	if _, err := os.Stat(modelPath); err != nil {
		return nil, fmt.Errorf("tiktoken FFI: model file not found: %s", modelPath)
	}

	configPath, err := writeConfigJSON(enc)
	if err != nil {
		return nil, fmt.Errorf("tiktoken FFI: config write: %w", err)
	}
	defer os.Remove(configPath)

	tok, err := hftokenizers.FromTiktoken(modelPath, configPath, enc.pattern)
	if err != nil {
		return nil, fmt.Errorf("tiktoken FFI: %w", err)
	}

	ffiCache[encName] = tok
	return &TiktokenFFICounter{model: model, encodingName: encName, tok: tok}, nil
}

func (t *TiktokenFFICounter) CountText(text string) int {
	if text == "" {
		return 0
	}
	ids, _ := t.tok.Encode(text, false)
	return len(ids)
}

func (t *TiktokenFFICounter) Backend() Backend    { return BackendTiktoken }
func (t *TiktokenFFICounter) Model() string       { return t.model }
func (t *TiktokenFFICounter) EncodingName() string { return t.encodingName }
