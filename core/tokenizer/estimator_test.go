package tokenizer

import (
	"strings"
	"testing"
)

func TestEstimatorDefault(t *testing.T) {
	// Passing 0 should default to 4.0 chars per token
	// "abcd" is 4 chars, with cpt=4.0 -> ceil(4/4.0) = 1
	ec := NewEstimatingCounter(0)
	if got := ec.CountText("abcd"); got != 1 {
		t.Errorf("default cpt: CountText(\"abcd\") = %d, want 1", got)
	}
}

func TestEstimatorEmptyIsZero(t *testing.T) {
	ec := NewEstimatingCounter(4.0)
	if got := ec.CountText(""); got != 0 {
		t.Errorf("empty string: got %d, want 0", got)
	}
}

func TestEstimatorCountsUnicodeChars(t *testing.T) {
	// "hello" = 5 runes, cpt=4.0 -> ceil(5/4.0) = 2
	ec := NewEstimatingCounter(4.0)
	if got := ec.CountText("hello"); got != 2 {
		t.Errorf("CountText(\"hello\") with cpt=4.0 = %d, want 2", got)
	}
}

func TestEstimatorUnicodeNotBytes(t *testing.T) {
	// "你好" = 2 runes (6 bytes), cpt=1.0 -> ceil(2/1.0) = 2
	ec := NewEstimatingCounter(1.0)
	if got := ec.CountText("你好"); got != 2 {
		t.Errorf("CountText(\"你好\") with cpt=1.0 = %d, want 2 (not 6 bytes)", got)
	}
}

func TestEstimatorClaude(t *testing.T) {
	// "Hello, world!" = 13 runes, cpt=3.5 -> ceil(13/3.5) = ceil(3.714) = 4
	ec := NewEstimatingCounter(3.5)
	if got := ec.CountText("Hello, world!"); got != 4 {
		t.Errorf("CountText(\"Hello, world!\") with cpt=3.5 = %d, want 4", got)
	}
}

func TestEstimatorNonEmpty(t *testing.T) {
	ec := NewEstimatingCounter(4.0)
	if got := ec.CountText("a"); got < 1 {
		t.Errorf("single char: got %d, want >= 1", got)
	}
}

func TestEstimatorLargeInput(t *testing.T) {
	ec := NewEstimatingCounter(4.0)
	large := strings.Repeat("a", 100000)
	// 100000 / 4.0 = 25000
	if got := ec.CountText(large); got != 25000 {
		t.Errorf("large input: got %d, want 25000", got)
	}
}

func TestEstimatorBackend(t *testing.T) {
	ec := NewEstimatingCounter(4.0)
	if ec.Backend() != BackendEstimator {
		t.Errorf("Backend() = %v, want BackendEstimator", ec.Backend())
	}
}
