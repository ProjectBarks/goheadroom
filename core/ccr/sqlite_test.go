package ccr

import (
	"path/filepath"
	"testing"
)

func TestSqlitePutGet(t *testing.T) {
	s, err := NewSqliteStore(filepath.Join(t.TempDir(), "test.db"), DefaultTTL)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.Put("key1", []byte("value1"))
	got, ok := s.Get("key1")
	if !ok || string(got) != "value1" {
		t.Errorf("Get = (%q, %v), want (value1, true)", got, ok)
	}
}

func TestSqliteMissing(t *testing.T) {
	s, err := NewSqliteStore(filepath.Join(t.TempDir(), "test.db"), DefaultTTL)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_, ok := s.Get("missing")
	if ok {
		t.Error("missing key should return false")
	}
}

func TestSqliteOverwrite(t *testing.T) {
	s, err := NewSqliteStore(filepath.Join(t.TempDir(), "test.db"), DefaultTTL)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.Put("k", []byte("v1"))
	s.Put("k", []byte("v2"))
	got, ok := s.Get("k")
	if !ok || string(got) != "v2" {
		t.Errorf("overwrite: got (%q, %v)", got, ok)
	}
}

func TestSqliteSurvivesReopen(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	s1, _ := NewSqliteStore(dbPath, DefaultTTL)
	s1.Put("persist", []byte("yes"))
	s1.Close()
	s2, _ := NewSqliteStore(dbPath, DefaultTTL)
	defer s2.Close()
	got, ok := s2.Get("persist")
	if !ok || string(got) != "yes" {
		t.Errorf("data did not survive reopen: got (%q, %v)", got, ok)
	}
}

func TestSqliteImplementsCcrStore(t *testing.T) {
	s, _ := NewSqliteStore(filepath.Join(t.TempDir(), "test.db"), DefaultTTL)
	defer s.Close()
	var _ CcrStore = s
}
