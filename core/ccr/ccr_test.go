package ccr

import (
	"regexp"
	"testing"
	"time"
)

func TestComputeKeyLength(t *testing.T) {
	key := ComputeKey([]byte("test"))
	if len(key) != 24 {
		t.Errorf("ComputeKey length = %d, want 24", len(key))
	}
}

func TestComputeKeyIsLowercaseHex(t *testing.T) {
	key := ComputeKey([]byte("test"))
	matched, _ := regexp.MatchString("^[0-9a-f]{24}$", key)
	if !matched {
		t.Errorf("ComputeKey(%q) = %q, want lowercase hex", "test", key)
	}
}

func TestComputeKeyDeterministic(t *testing.T) {
	a := ComputeKey([]byte("same input"))
	b := ComputeKey([]byte("same input"))
	if a != b {
		t.Errorf("ComputeKey not deterministic: %q != %q", a, b)
	}
}

func TestComputeKeyDiverges(t *testing.T) {
	a := ComputeKey([]byte("input one"))
	b := ComputeKey([]byte("input two"))
	if a == b {
		t.Errorf("ComputeKey should diverge for different inputs, both = %q", a)
	}
}

func TestComputeKeyParityWithRust(t *testing.T) {
	got := ComputeKey([]byte("hello world"))
	want := "d74981efa70a0c880b8d8c19"
	if got != want {
		t.Errorf("ComputeKey(\"hello world\") = %q, want %q", got, want)
	}
}

func TestMarkerForFormat(t *testing.T) {
	got := MarkerFor("abc123")
	want := "<<ccr:abc123>>"
	if got != want {
		t.Errorf("MarkerFor(\"abc123\") = %q, want %q", got, want)
	}
}

func TestDefaultConstants(t *testing.T) {
	if DefaultCapacity != 1000 {
		t.Errorf("DefaultCapacity = %d, want 1000", DefaultCapacity)
	}
	if DefaultTTL != 5*time.Minute {
		t.Errorf("DefaultTTL = %v, want 5m", DefaultTTL)
	}
}
