package service

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"ecommerce/server/internal/cache"
	"ecommerce/server/internal/model"
	"ecommerce/server/internal/repository"
)

func testCategoryServiceWithCache(t *testing.T) (*CategoryService, *cache.Cache, *pgxpool.Pool) {
	t.Helper()
	pool := newTestPool(t)
	repo := repository.NewCategoryRepo(pool)

	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		redisURL = "redis://localhost:16379"
	}
	c, err := cache.New(redisURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := c.Ping(ctx); err != nil {
		t.Skipf("redis not reachable, skipping: %v", err)
	}

	return NewCategoryService(repo).WithCache(c), c, pool
}

// TestCategoryCacheHitMiss: first call misses and fills the cache, the
// second returns from Redis (same category set).
func TestCategoryCacheHitMiss(t *testing.T) {
	svc, c, pool := testCategoryServiceWithCache(t)
	ctx := context.Background()
	defer c.Delete(ctx, categoriesCacheKey)

	name := "Cache Cat " + time.Now().Format("150405.000000")
	cat, err := svc.Create(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	// slug stays unique under soft delete -> hard-delete in cleanup
	defer pool.Exec(ctx, `DELETE FROM categories WHERE id = $1`, cat.ID)

	first, err := svc.ListActive(ctx)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.ListActive(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != len(first) || len(second) == 0 {
		t.Fatalf("ListActive = %d categories (first %d), want same non-empty", len(second), len(first))
	}
	if _, err := c.Get(ctx, categoriesCacheKey); err != nil {
		t.Errorf("category cache entry missing after first ListActive: %v", err)
	}
}

// TestCategoryCacheInvalidatedOnCRUD: create/update/delete wipe the single
// categories:active key.
func TestCategoryCacheInvalidatedOnCRUD(t *testing.T) {
	svc, c, pool := testCategoryServiceWithCache(t)
	ctx := context.Background()

	name := "Cache CRUD " + time.Now().Format("150405.000000")
	cat, err := svc.Create(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `DELETE FROM categories WHERE id = $1`, cat.ID)

	if _, err := svc.ListActive(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Get(ctx, categoriesCacheKey); err != nil {
		t.Fatalf("cache entry missing: %v", err)
	}

	// update wipes the cache
	if _, err := svc.Update(ctx, cat.ID, name+" 2"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Get(ctx, categoriesCacheKey); err == nil {
		t.Error("category cache must be invalidated after update")
	}
}

// TestCategoryCacheRedisDown: unreachable Redis must not break listing
// (fail-open, PRD H).
func TestCategoryCacheRedisDown(t *testing.T) {
	pool := newTestPool(t)
	svc := NewCategoryService(repository.NewCategoryRepo(pool)).WithCache(deadCache{})
	ctx := context.Background()

	cat := &model.Category{}
	if err := pool.QueryRow(ctx,
		`INSERT INTO categories (name, slug) VALUES ('Down Cat', 'down-cat') RETURNING id`).Scan(&cat.ID); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `DELETE FROM categories WHERE id = $1`, cat.ID)

	cats, err := svc.ListActive(ctx)
	if err != nil {
		t.Fatalf("ListActive with Redis down returned error: %v", err)
	}
	found := false
	for _, c := range cats {
		if c.ID == cat.ID {
			found = true
		}
	}
	if !found {
		t.Error("ListActive with Redis down must include the seeded category")
	}
}
