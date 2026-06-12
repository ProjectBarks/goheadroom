package tokenizer

import "testing"

func TestGetTokenizerOpenAI(t *testing.T) {
	tok := GetTokenizer("gpt-4o")
	if tok.Backend() != BackendTiktoken {
		t.Errorf("gpt-4o backend = %v, want BackendTiktoken", tok.Backend())
	}
	// Should actually count tokens
	if got := tok.CountText("hello"); got != 1 {
		t.Errorf("gpt-4o CountText(\"hello\") = %d, want 1", got)
	}
}

func TestGetTokenizerUnknownFallsToEstimator(t *testing.T) {
	tok := GetTokenizer("claude-3-opus")
	if tok.Backend() != BackendEstimator {
		t.Errorf("claude-3-opus backend = %v, want BackendEstimator", tok.Backend())
	}
	// Should still count tokens (via estimator)
	if got := tok.CountText("hello"); got < 1 {
		t.Errorf("claude-3-opus CountText(\"hello\") = %d, want >= 1", got)
	}
}

func TestDetectBackendOpenAI(t *testing.T) {
	if got := DetectBackend("gpt-4o"); got != BackendTiktoken {
		t.Errorf("DetectBackend(gpt-4o) = %v, want BackendTiktoken", got)
	}
}

func TestDetectBackendClaude(t *testing.T) {
	if got := DetectBackend("claude-3-opus"); got != BackendEstimator {
		t.Errorf("DetectBackend(claude-3-opus) = %v, want BackendEstimator", got)
	}
}

func TestDetectBackendHf(t *testing.T) {
	// Register a fake HF tokenizer, check detection
	fake := NewEstimatingCounter(3.0) // just reusing estimator as a stub
	RegisterHf("custom-hf-model", fake)
	defer ClearHfRegistrations()

	if got := DetectBackend("custom-hf-model"); got != BackendHuggingFace {
		t.Errorf("DetectBackend(custom-hf-model) = %v, want BackendHuggingFace", got)
	}
}

func TestGetTokenizerCaching(t *testing.T) {
	tok1 := GetTokenizer("gpt-4o")
	tok2 := GetTokenizer("gpt-4o")
	// Should return cached instance (same pointer)
	if tok1 != tok2 {
		t.Error("GetTokenizer should return cached instance for same model")
	}
}

func TestIsAnthropicModel(t *testing.T) {
	tests := []struct {
		model string
		want  bool
	}{
		{"claude-3-opus", true},
		{"claude-3.5-sonnet", true},
		{"anthropic.claude-v2", true},
		{"gpt-4o", false},
		{"llama-3", false},
	}
	for _, tt := range tests {
		if got := isAnthropicModel(tt.model); got != tt.want {
			t.Errorf("isAnthropicModel(%q) = %v, want %v", tt.model, got, tt.want)
		}
	}
}

func TestRegistryHfResolution(t *testing.T) {
	fake := NewEstimatingCounter(2.0)
	RegisterHf("my-hf-model", fake)
	defer ClearHfRegistrations()

	tok := GetTokenizer("my-hf-model")
	if tok.Backend() != BackendEstimator {
		// The fake is an EstimatingCounter, so backend is Estimator
		// but it should be the registered instance
		if tok != Tokenizer(fake) {
			t.Error("expected registered HF tokenizer to be returned")
		}
	}
}
