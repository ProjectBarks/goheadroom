package ccr

import (
	"path/filepath"
	"testing"
)

func TestFromConfigInMemory(t *testing.T) {
	cfg := CcrBackendConfig{Backend: "inmemory"}
	store, err := FromConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if store == nil {
		t.Fatal("store is nil")
	}
	store.Put("x", []byte("y"))
	got, ok := store.Get("x")
	if !ok || string(got) != "y" {
		t.Error("inmemory backend failed basic put/get")
	}
}

func TestFromConfigSqlite(t *testing.T) {
	cfg := CcrBackendConfig{
		Backend:    "sqlite",
		SqlitePath: filepath.Join(t.TempDir(), "ccr.db"),
	}
	store, err := FromConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	store.Put("x", []byte("y"))
	got, ok := store.Get("x")
	if !ok || string(got) != "y" {
		t.Error("sqlite backend failed basic put/get")
	}
}

func TestFromConfigDefault(t *testing.T) {
	cfg := CcrBackendConfig{}
	store, err := FromConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if store == nil {
		t.Fatal("default config should create inmemory store")
	}
}

func TestFromConfigUnknown(t *testing.T) {
	cfg := CcrBackendConfig{Backend: "unknown"}
	_, err := FromConfig(cfg)
	if err == nil {
		t.Error("unknown backend should error")
	}
}
