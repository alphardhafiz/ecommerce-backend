package cache

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

type Cache struct {
	client *redis.Client
}

func New(redisURL string) (*Cache, error) {
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, err
	}
	return &Cache{client: redis.NewClient(opts)}, nil
}

func (c *Cache) Get(ctx context.Context, key string) ([]byte, error) {
	return c.client.Get(ctx, key).Bytes()
}

func (c *Cache) Set(ctx context.Context, key string, val []byte, ttl time.Duration) error {
	return c.client.Set(ctx, key, val, ttl).Err()
}

func (c *Cache) Delete(ctx context.Context, keys ...string) error {
	return c.client.Del(ctx, keys...).Err()
}

// InvalidatePrefix deletes all keys under a prefix using SCAN+DEL (never KEYS,
// to avoid blocking). Callers must fall back to DB when it returns an error.
func (c *Cache) InvalidatePrefix(ctx context.Context, prefix string) error {
	var keys []string
	iter := c.client.Scan(ctx, 0, prefix+"*", 100).Iterator()
	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
	}
	if err := iter.Err(); err != nil {
		return err
	}
	if len(keys) > 0 {
		return c.client.Del(ctx, keys...).Err()
	}
	return nil
}

// Incr increments a fixed-window counter (PRD H.3): the key's TTL is set
// only when the window starts (count == 1), so every new window resets it.
// Returns the new count.
func (c *Cache) Incr(ctx context.Context, key string, ttl time.Duration) (int64, error) {
	n, err := c.client.Incr(ctx, key).Result()
	if err != nil {
		return 0, err
	}
	if n == 1 {
		if err := c.client.Expire(ctx, key, ttl).Err(); err != nil {
			return 0, err
		}
	}
	return n, nil
}

func (c *Cache) Ping(ctx context.Context) error {
	return c.client.Ping(ctx).Err()
}

func (c *Cache) Close() error {
	return c.client.Close()
}
