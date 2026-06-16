package authmode

import (
	"net/http"
	"testing"
)

func TestPaygWithAPIKey(t *testing.T) {
	h := http.Header{}
	h.Set("x-api-key", "sk-ant-api-xxxxx")
	if got := Classify(h); got != Payg {
		t.Errorf("x-api-key -> %v, want Payg", got)
	}
}

func TestPaygWithBearerSk(t *testing.T) {
	h := http.Header{}
	h.Set("Authorization", "Bearer sk-proj-xxxxx")
	if got := Classify(h); got != Payg {
		t.Errorf("Bearer sk- -> %v, want Payg", got)
	}
}

func TestOAuthWithBearerOat(t *testing.T) {
	h := http.Header{}
	h.Set("Authorization", "Bearer sk-ant-oat-xxxxx")
	if got := Classify(h); got != OAuth {
		t.Errorf("Bearer sk-ant-oat -> %v, want OAuth", got)
	}
}

func TestOAuthWithJWT(t *testing.T) {
	h := http.Header{}
	h.Set("Authorization", "Bearer eyJ.eyJ.sig")
	if got := Classify(h); got != OAuth {
		t.Errorf("JWT -> %v, want OAuth", got)
	}
}

func TestOAuthWithAWSSigV4(t *testing.T) {
	h := http.Header{}
	h.Set("Authorization", "AWS4-HMAC-SHA256 Credential=xxx")
	if got := Classify(h); got != OAuth {
		t.Errorf("SigV4 -> %v, want OAuth", got)
	}
}

func TestSubscriptionWithClaudeCode(t *testing.T) {
	h := http.Header{}
	h.Set("User-Agent", "claude-code/1.2.3")
	h.Set("Authorization", "Bearer sk-ant-oat-xxxxx")
	if got := Classify(h); got != Subscription {
		t.Errorf("Claude Code UA -> %v, want Subscription", got)
	}
}

func TestSubscriptionWithCursor(t *testing.T) {
	h := http.Header{}
	h.Set("User-Agent", "Cursor/0.50.0")
	if got := Classify(h); got != Subscription {
		t.Errorf("Cursor UA -> %v, want Subscription", got)
	}
}

func TestPaygWithGoogleKey(t *testing.T) {
	h := http.Header{}
	h.Set("x-goog-api-key", "AIza-xxxxx")
	if got := Classify(h); got != Payg {
		t.Errorf("x-goog-api-key -> %v, want Payg", got)
	}
}

func TestDefaultIsPayg(t *testing.T) {
	h := http.Header{}
	if got := Classify(h); got != Payg {
		t.Errorf("empty headers -> %v, want Payg", got)
	}
}

func TestAsStr(t *testing.T) {
	if Payg.String() != "payg" {
		t.Error("Payg.String()")
	}
	if OAuth.String() != "oauth" {
		t.Error("OAuth.String()")
	}
	if Subscription.String() != "subscription" {
		t.Error("Subscription.String()")
	}
}
