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
// Uses a ring buffer for FIFO eviction order to avoid unbounded backing-array growth.
type InMemoryStore struct {
	mu       sync.RWMutex
	data     map[string]entry
	ring     []string // circular buffer for eviction order
	head     int      // index of oldest entry in ring
	count    int      // number of valid entries in ring
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
		ring:     make([]string, capacity),
		capacity: capacity,
		ttl:      ttl,
	}
}

// Put stores a value. Idempotent on the same key (overwrites in place).
// If capacity is exceeded, the oldest entry is evicted (FIFO).
func (s *InMemoryStore) Put(key string, value []byte) {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.data[key]; exists {
		// Overwrite in place; no change to order.
		s.data[key] = entry{value: value, expiresAt: now.Add(s.ttl)}
		return
	}

	// Evict oldest if at capacity.
	for len(s.data) >= s.capacity && s.count > 0 {
		oldest := s.ring[s.head]
		s.head = (s.head + 1) % s.capacity
		s.count--
		delete(s.data, oldest)
	}

	s.data[key] = entry{value: value, expiresAt: now.Add(s.ttl)}
	tail := (s.head + s.count) % s.capacity
	s.ring[tail] = key
	s.count++
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

	now := time.Now()
	if now.After(e.expiresAt) {
		// Double-check under write lock to avoid TOCTOU race.
		s.mu.Lock()
		e2, stillThere := s.data[key]
		if stillThere && now.After(e2.expiresAt) {
			delete(s.data, key)
			// Leave stale key in ring; eviction loop in Put handles it
			// via delete(s.data, oldest) being a no-op for missing keys.
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
