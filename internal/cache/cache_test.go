package cache

import (
	"context"
	"os"
	"testing"
	"time"
)

func redisURL(t *testing.T) string {
	url := os.Getenv("REDIS_URL")
	if url == "" {
		url = "redis://localhost:16379"
	}
	return url
}

func newTestCache(t *testing.T) *Cache {
	t.Helper()
	c, err := New(redisURL(t))
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := c.Ping(ctx); err != nil {
		t.Skipf("redis not reachable, skipping: %v", err)
	}
	return c
}

func TestSetGetDelete(t *testing.T) {
	c := newTestCache(t)
	ctx := context.Background()

	if err := c.Set(ctx, "t:key", []byte("value"), time.Minute); err != nil {
		t.Fatalf("Set() error: %v", err)
	}
	got, err := c.Get(ctx, "t:key")
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	if string(got) != "value" {
		t.Errorf("Get() = %q, want %q", got, "value")
	}
	if err := c.Delete(ctx, "t:key"); err != nil {
		t.Fatalf("Delete() error: %v", err)
	}
	if _, err := c.Get(ctx, "t:key"); err == nil {
		t.Error("Get() after delete should error")
	}
}

func TestInvalidatePrefix(t *testing.T) {
	c := newTestCache(t)
	ctx := context.Background()
	defer func() {
		c.Delete(ctx, "pfx:1", "pfx:2", "other:1")
	}()

	c.Set(ctx, "pfx:1", []byte("a"), time.Minute)
	c.Set(ctx, "pfx:2", []byte("b"), time.Minute)
	c.Set(ctx, "other:1", []byte("c"), time.Minute)

	if err := c.InvalidatePrefix(ctx, "pfx:"); err != nil {
		t.Fatalf("InvalidatePrefix() error: %v", err)
	}
	if _, err := c.Get(ctx, "pfx:1"); err == nil {
		t.Error("pfx:1 should be deleted")
	}
	if _, err := c.Get(ctx, "pfx:2"); err == nil {
		t.Error("pfx:2 should be deleted")
	}
	if _, err := c.Get(ctx, "other:1"); err != nil {
		t.Error("other:1 should not be deleted")
	}
}
