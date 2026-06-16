package core

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/projectbarks/goheadroom/core/authmode"
	"github.com/projectbarks/goheadroom/core/cachecontrol"
	"github.com/projectbarks/goheadroom/core/ccr"
	"github.com/projectbarks/goheadroom/core/compressionpolicy"
	"github.com/projectbarks/goheadroom/core/tokenizer"
)

func TestHello(t *testing.T) {
	got := Hello()
	want := "headroom-core"
	if got != want {
		t.Errorf("Hello() = %q, want %q", got, want)
	}
}

func TestFoundationIntegration(t *testing.T) {
	// CCR: compute key, store, retrieve
	key := ccr.ComputeKey([]byte("test payload"))
	store := ccr.NewInMemoryStore()
	store.Put(key, []byte("original content"))
	got, ok := store.Get(key)
	if !ok || string(got) != "original content" {
		t.Fatal("CCR round-trip failed")
	}
	marker := ccr.MarkerFor(key)
	if len(marker) == 0 {
		t.Fatal("marker is empty")
	}

	// Tokenizer: count tokens
	tok := tokenizer.GetTokenizer("gpt-4o")
	n := tok.CountText("Hello, world!")
	if n != 4 {
		t.Errorf("token count = %d, want 4", n)
	}

	// AuthMode: classify empty headers = Payg
	h := http.Header{}
	if authmode.Classify(h) != authmode.Payg {
		t.Error("empty headers should be Payg")
	}

	// CompressionPolicy: Payg is aggressive
	p := compressionpolicy.ForMode(authmode.Payg)
	if p.LiveZoneOnly {
		t.Error("Payg should not be live-zone-only")
	}

	// CacheControl: no markers = 0 frozen
	var body map[string]interface{}
	json.Unmarshal([]byte(`{"messages": [{"role": "user", "content": "hi"}]}`), &body)
	if cachecontrol.ComputeFrozenCount(body) != 0 {
		t.Error("no markers should yield 0 frozen")
	}
}
