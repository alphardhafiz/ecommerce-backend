package service

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"ecommerce/server/internal/cache"
	"ecommerce/server/internal/model"
	"ecommerce/server/internal/repository"
)

func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set, skipping service test")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func testProductServiceWithCache(t *testing.T) (*ProductService, *cache.Cache) {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set, skipping cache test")
	}
	pool := newTestPool(t)
	repo := repository.NewProductRepo(pool)

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

	return NewProductService(repo).WithCache(c), c
}

// deadCache fails instantly, simulating Redis being down (fail-open, PRD H).
type deadCache struct{}

func (deadCache) Get(ctx context.Context, key string) ([]byte, error) {
	return nil, errors.New("redis down")
}
func (deadCache) Set(ctx context.Context, key string, val []byte, ttl time.Duration) error {
	return errors.New("redis down")
}
func (deadCache) InvalidatePrefix(ctx context.Context, prefix string) error {
	return errors.New("redis down")
}

// TestProductListCacheHitMiss: first call misses and fills the cache, the
// second returns from Redis (same product id, no DB round-trip needed).
func TestProductListCacheHitMiss(t *testing.T) {
	svc, c := testProductServiceWithCache(t)
	ctx := context.Background()

	prod := seedServiceProduct(t, svc, "Cache Hit Test", 45000, 7)
	defer svc.products.SoftDelete(ctx, prod.ID)

	f := ProductListFilter{Search: "Cache Hit Test", Limit: 12}
	defer c.Delete(ctx, productListKey(f))

	products, total, err := svc.List(ctx, f)
	if err != nil || total != 1 || len(products) != 1 {
		t.Fatalf("first List = %d products, total %d, err %v", len(products), total, err)
	}

	// second call must be served from cache (same results)
	products2, total2, err := svc.List(ctx, f)
	if err != nil || total2 != 1 || len(products2) != 1 {
		t.Fatalf("second List = %d products, total %d, err %v", len(products2), total2, err)
	}
	if products2[0].ID != prod.ID {
		t.Errorf("cached product id = %s, want %s", products2[0].ID, prod.ID)
	}
}

func TestProductListCacheInvalidatedOnMutation(t *testing.T) {
	svc, c := testProductServiceWithCache(t)
	ctx := context.Background()

	prod := seedServiceProduct(t, svc, "Cache Inval Test", 45000, 7)
	defer svc.products.SoftDelete(ctx, prod.ID)

	f := ProductListFilter{Search: "Cache Inval Test", Limit: 12}
	if _, _, err := svc.List(ctx, f); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Get(ctx, productListKey(f)); err != nil {
		t.Fatalf("cache entry missing after first List: %v", err)
	}

	// update stock -> list cache wiped
	if _, err := svc.UpdateStock(ctx, prod.ID, 3); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Get(ctx, productListKey(f)); err == nil {
		t.Error("list cache must be invalidated after stock update")
	}
}

// TestProductListCacheRedisDown: an unreachable Redis must not break the
// listing (fail-open, PRD H).
func TestProductListCacheRedisDown(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set, skipping cache test")
	}
	pool := newTestPool(t)
	svc := NewProductService(repository.NewProductRepo(pool)).WithCache(deadCache{})
	ctx := context.Background()
	prod := seedServiceProduct(t, svc, "Cache Down Test", 45000, 7)
	defer svc.products.SoftDelete(ctx, prod.ID)

	products, total, err := svc.List(ctx, ProductListFilter{Search: "Cache Down Test", Limit: 12})
	if err != nil {
		t.Fatalf("List with Redis down returned error: %v", err)
	}
	if total != 1 || len(products) != 1 {
		t.Errorf("List with Redis down = %d products, total %d, want 1/1", len(products), total)
	}
}

// seedServiceProduct creates an active product via the service.
func seedServiceProduct(t *testing.T, svc *ProductService, name string, price, stock int64) *model.Product {
	t.Helper()
	p, err := svc.Create(context.Background(), ProductInput{Name: name, Price: price, Stock: stock})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateStatus(context.Background(), p.ID, true); err != nil {
		t.Fatal(err)
	}
	return p
}
