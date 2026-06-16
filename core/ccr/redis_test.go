package ccr

import (
	"context"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func skipIfNoRedis(t *testing.T) {
	t.Helper()
	c := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	if err := c.Ping(context.Background()).Err(); err != nil {
		t.Skipf("Redis not available: %v", err)
	}
	c.Close()
}

func TestRedisPutGet(t *testing.T) {
	skipIfNoRedis(t)
	s, err := NewRedisStore("localhost:6379", "ccr-test", 300*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	s.Put("rk1", []byte("rv1"))
	got, ok := s.Get("rk1")
	if !ok || string(got) != "rv1" {
		t.Errorf("Get = (%q, %v), want (rv1, true)", got, ok)
	}
}
