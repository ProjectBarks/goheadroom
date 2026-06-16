package ccr

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisStore is a CCR store backed by Redis with per-key TTL.
type RedisStore struct {
	client    *redis.Client
	keyPrefix string
	ttl       time.Duration
}

// NewRedisStore connects to Redis at addr and returns a store.
func NewRedisStore(addr, keyPrefix string, ttl time.Duration) (*RedisStore, error) {
	client := redis.NewClient(&redis.Options{Addr: addr})
	if err := client.Ping(context.Background()).Err(); err != nil {
		return nil, fmt.Errorf("redis ping: %w", err)
	}
	return &RedisStore{client: client, keyPrefix: keyPrefix, ttl: ttl}, nil
}

func (s *RedisStore) Put(key string, value []byte) {
	k := s.keyPrefix + ":" + key
	s.client.SetEx(context.Background(), k, value, s.ttl)
}

func (s *RedisStore) Get(key string) ([]byte, bool) {
	k := s.keyPrefix + ":" + key
	val, err := s.client.Get(context.Background(), k).Bytes()
	if err != nil {
		return nil, false
	}
	return val, true
}

func (s *RedisStore) Len() int {
	return 0
}
