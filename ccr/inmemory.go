package ccr

import (
	"sync"
	"time"
)

type entry struct {
	value     []byte
	expiresAt time.Time
}

// InMemoryStore is a thread-safe, capacity-bounded, TTL-aware CCR store.
type InMemoryStore struct {
	mu       sync.RWMutex
	data     map[string]entry
	order    []string // FIFO order for eviction
	capacity int
	ttl      time.Duration
}

// NewInMemoryStore creates an InMemoryStore with default capacity and TTL.
func NewInMemoryStore() *InMemoryStore {
	return NewInMemoryStoreWithOptions(DefaultCapacity, DefaultTTL)
}

// NewInMemoryStoreWithOptions creates an InMemoryStore with the given capacity and TTL.
func NewInMemoryStoreWithOptions(capacity int, ttl time.Duration) *InMemoryStore {
	return &InMemoryStore{
		data:     make(map[string]entry, capacity),
		order:    make([]string, 0, capacity),
		capacity: capacity,
		ttl:      ttl,
	}
}

// Put stores a value. Idempotent on the same key (overwrites in place).
// If capacity is exceeded, the oldest entry is evicted (FIFO).
func (s *InMemoryStore) Put(key string, value []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.data[key]; exists {
		// Overwrite in place; no change to order.
		s.data[key] = entry{value: value, expiresAt: time.Now().Add(s.ttl)}
		return
	}

	// Evict oldest if at capacity.
	for len(s.data) >= s.capacity && len(s.order) > 0 {
		oldest := s.order[0]
		s.order = s.order[1:]
		delete(s.data, oldest)
	}

	s.data[key] = entry{value: value, expiresAt: time.Now().Add(s.ttl)}
	s.order = append(s.order, key)
}

// Get retrieves a value. Returns (nil, false) on miss or expiry.
// Uses TOCTOU-safe double-check: read lock first, then write lock to delete if expired.
func (s *InMemoryStore) Get(key string) ([]byte, bool) {
	s.mu.RLock()
	e, ok := s.data[key]
	s.mu.RUnlock()

	if !ok {
		return nil, false
	}

	if time.Now().After(e.expiresAt) {
		// Double-check under write lock to avoid TOCTOU race.
		s.mu.Lock()
		e2, stillThere := s.data[key]
		if stillThere && time.Now().After(e2.expiresAt) {
			delete(s.data, key)
			// Remove from order slice.
			for i, k := range s.order {
				if k == key {
					s.order = append(s.order[:i], s.order[i+1:]...)
					break
				}
			}
		}
		s.mu.Unlock()
		return nil, false
	}

	return e.value, true
}

// Len returns the count of live (non-expired) entries.
func (s *InMemoryStore) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	count := 0
	now := time.Now()
	for _, e := range s.data {
		if now.Before(e.expiresAt) {
			count++
		}
	}
	return count
}
