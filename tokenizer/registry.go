package tokenizer

import "sync"

var (
	cache   = make(map[string]Tokenizer)
	cacheMu sync.RWMutex
	hfRegistry   = make(map[string]Tokenizer)
	hfRegistryMu sync.RWMutex
)

func GetTokenizer(model string) Tokenizer {
	cacheMu.RLock()
	if tok, ok := cache[model]; ok {
		cacheMu.RUnlock()
		return tok
	}
	cacheMu.RUnlock()
	tok := resolveTokenizer(model)
	cacheMu.Lock()
	cache[model] = tok
	cacheMu.Unlock()
	return tok
}

func resolveTokenizer(model string) Tokenizer {
	if t, err := NewTiktokenCounter(model); err == nil {
		return t
	}
	hfRegistryMu.RLock()
	if t, ok := hfRegistry[model]; ok {
		hfRegistryMu.RUnlock()
		return t
	}
	hfRegistryMu.RUnlock()
	cpt := 4.0
	if isAnthropicModel(model) {
		cpt = 3.5
	}
	return NewEstimatingCounter(cpt)
}

func RegisterHf(model string, tok Tokenizer) {
	hfRegistryMu.Lock()
	defer hfRegistryMu.Unlock()
	hfRegistry[model] = tok
}

func ClearHfRegistrations() {
	hfRegistryMu.Lock()
	defer hfRegistryMu.Unlock()
	hfRegistry = make(map[string]Tokenizer)
}

func DetectBackend(model string) Backend {
	if _, err := encodingFor(model); err == nil {
		return BackendTiktoken
	}
	hfRegistryMu.RLock()
	_, ok := hfRegistry[model]
	hfRegistryMu.RUnlock()
	if ok {
		return BackendHuggingFace
	}
	return BackendEstimator
}

func isAnthropicModel(model string) bool {
	for _, prefix := range []string{"claude", "anthropic"} {
		if len(model) >= len(prefix) && model[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}
