package cachecontrol

import (
	"encoding/json"
	"testing"
)

func mustParse(t *testing.T, s string) map[string]interface{} {
	var v map[string]interface{}
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestNoMarkersYieldsZero(t *testing.T) {
	body := mustParse(t, `{
		"model": "claude-3-5-sonnet-20241022",
		"messages": [
			{"role": "user", "content": "hi"},
			{"role": "assistant", "content": "hello"}
		]
	}`)
	if got := ComputeFrozenCount(body); got != 0 {
		t.Errorf("no markers: got %d, want 0", got)
	}
}

func TestMarkerAtMessageZeroYieldsOne(t *testing.T) {
	body := mustParse(t, `{
		"messages": [
			{"role": "user", "content": [
				{"type": "text", "text": "first", "cache_control": {"type": "ephemeral"}}
			]},
			{"role": "assistant", "content": "second"}
		]
	}`)
	if got := ComputeFrozenCount(body); got != 1 {
		t.Errorf("marker at [0]: got %d, want 1", got)
	}
}

func TestMarkerInSystemDoesNotBump(t *testing.T) {
	body := mustParse(t, `{
		"system": [
			{"type": "text", "text": "you are helpful", "cache_control": {"type": "ephemeral"}}
		],
		"messages": [
			{"role": "user", "content": "hi"}
		]
	}`)
	if got := ComputeFrozenCount(body); got != 0 {
		t.Errorf("system marker: got %d, want 0", got)
	}
}

func TestMarkerInToolsDoesNotBump(t *testing.T) {
	body := mustParse(t, `{
		"tools": [
			{"name": "search", "description": "search", "cache_control": {"type": "ephemeral"}}
		],
		"messages": [
			{"role": "user", "content": "hi"}
		]
	}`)
	if got := ComputeFrozenCount(body); got != 0 {
		t.Errorf("tools marker: got %d, want 0", got)
	}
}

func TestMissingMessagesYieldsZero(t *testing.T) {
	body := mustParse(t, `{"model": "claude"}`)
	if got := ComputeFrozenCount(body); got != 0 {
		t.Errorf("missing messages: got %d, want 0", got)
	}
}

func TestStringContentYieldsZero(t *testing.T) {
	body := mustParse(t, `{
		"messages": [
			{"role": "user", "content": "plain string"},
			{"role": "assistant", "content": "another string"}
		]
	}`)
	if got := ComputeFrozenCount(body); got != 0 {
		t.Errorf("string content: got %d, want 0", got)
	}
}

func TestExtractTtlWhenPresent(t *testing.T) {
	marker := map[string]interface{}{"type": "ephemeral", "ttl": "1h"}
	ttl := extractTTL(marker)
	if ttl == nil || *ttl != "1h" {
		t.Errorf("extractTTL = %v, want '1h'", ttl)
	}
}

func TestExtractTtlMissingReturnsNil(t *testing.T) {
	marker := map[string]interface{}{"type": "ephemeral"}
	if ttl := extractTTL(marker); ttl != nil {
		t.Errorf("extractTTL = %v, want nil", *ttl)
	}
}

func TestTtlWalkerAccepts1hBefore5m(t *testing.T) {
	w := newTTLOrderingWalk()
	s1h := "1h"
	s5m := "5m"
	w.observe(&s1h)
	w.observe(&s5m)
	if w.violated {
		t.Error("1h before 5m should not violate")
	}
}

func TestTtlWalkerFlags5mBefore1h(t *testing.T) {
	w := newTTLOrderingWalk()
	s5m := "5m"
	s1h := "1h"
	w.observe(&s5m)
	w.observe(&s1h)
	if !w.violated {
		t.Error("5m before 1h should violate")
	}
}

func TestTtlWalkerTreatsDefaultAs5m(t *testing.T) {
	w := newTTLOrderingWalk()
	s1h := "1h"
	w.observe(nil) // default = 5m
	w.observe(&s1h)
	if !w.violated {
		t.Error("default(5m) before 1h should violate")
	}
}
