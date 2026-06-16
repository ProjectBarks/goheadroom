package ccr

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestInMemoryPutThenGet(t *testing.T) {
	s := NewInMemoryStore()
	s.Put("k1", []byte("v1"))
	got, ok := s.Get("k1")
	if !ok {
		t.Fatal("expected key k1 to exist")
	}
	if string(got) != "v1" {
		t.Errorf("Get(k1) = %q, want %q", got, "v1")
	}
}

func TestInMemoryMissing(t *testing.T) {
	s := NewInMemoryStore()
	_, ok := s.Get("nope")
	if ok {
		t.Error("expected missing key to return false")
	}
}

func TestInMemoryOverwrite(t *testing.T) {
	s := NewInMemoryStore()
	s.Put("k1", []byte("v1"))
	s.Put("k1", []byte("v2"))
	got, ok := s.Get("k1")
	if !ok {
		t.Fatal("expected key k1 to exist after overwrite")
	}
	if string(got) != "v2" {
		t.Errorf("Get(k1) = %q, want %q after overwrite", got, "v2")
	}
	if s.Len() != 1 {
		t.Errorf("Len() = %d, want 1 after overwrite", s.Len())
	}
}

func TestInMemoryCapacityEviction(t *testing.T) {
	s := NewInMemoryStoreWithOptions(2, time.Hour)
	s.Put("a", []byte("1"))
	s.Put("b", []byte("2"))
	s.Put("c", []byte("3"))

	if _, ok := s.Get("a"); ok {
		t.Error("expected key 'a' to be evicted")
	}
	if _, ok := s.Get("b"); !ok {
		t.Error("expected key 'b' to still exist")
	}
	if _, ok := s.Get("c"); !ok {
		t.Error("expected key 'c' to still exist")
	}
	if s.Len() != 2 {
		t.Errorf("Len() = %d, want 2", s.Len())
	}
}

func TestInMemoryTTLExpiry(t *testing.T) {
	s := NewInMemoryStoreWithOptions(100, 10*time.Millisecond)
	s.Put("k", []byte("v"))
	time.Sleep(25 * time.Millisecond)
	_, ok := s.Get("k")
	if ok {
		t.Error("expected key to be expired after TTL")
	}
}

func TestInMemoryConcurrentSafety(t *testing.T) {
	s := NewInMemoryStoreWithOptions(500, time.Hour)
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				key := fmt.Sprintf("g%d-k%d", id, i)
				s.Put(key, []byte("val"))
				s.Get(key)
			}
		}(g)
	}
	wg.Wait()
	// If we get here without a race detector complaint, concurrency is safe.
	if s.Len() > 500 {
		t.Errorf("Len() = %d, exceeds capacity 500", s.Len())
	}
}

func TestInMemoryTraitObject(t *testing.T) {
	var store CcrStore = NewInMemoryStore()
	store.Put("k", []byte("v"))
	got, ok := store.Get("k")
	if !ok || string(got) != "v" {
		t.Errorf("CcrStore interface usage failed: ok=%v, got=%q", ok, got)
	}
}
