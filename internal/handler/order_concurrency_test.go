package handler

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"

	"ecommerce/server/internal/repository"
)

// TestOrderCheckoutConcurrencyStockOne: two users checkout the same product
// with stock=1 in parallel. Exactly one must succeed (201), the other gets
// 409 PRODUCT_OUT_OF_STOCK; final stock is 0, never negative (PRD G, U.5).
func TestOrderCheckoutConcurrencyStockOne(t *testing.T) {
	h, pool := newOrderHandler(t)

	prod := seedProduct(t, pool, "Produk Race", 50000, 1, nil)
	prodRepo := repository.NewProductRepo(pool)
	if _, err := prodRepo.SetActive(context.Background(), prod.ID, true); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(context.Background(), `DELETE FROM products WHERE id = $1`, prod.ID)

	type actor struct {
		email  string
		userID string
		itemID string
		addrID string
	}
	actors := make([]actor, 2)
	for i := range actors {
		a := &actors[i]
		a.email = "race-user-" + string(rune('A'+i)) + "@example.com"
		seedUser(t, pool, a.email, "abc12345", "active")
		defer cleanupOrderUser(t, pool, a.email)
		if err := pool.QueryRow(context.Background(),
			`SELECT id FROM users WHERE email = $1`, a.email).Scan(&a.userID); err != nil {
			t.Fatal(err)
		}

		addrRepo := repository.NewAddressRepo(pool)
		label := "Rumah"
		addr, err := addrRepo.Create(context.Background(), a.userID, modelAddress(label))
		if err != nil {
			t.Fatal(err)
		}
		a.addrID = addr.ID

		cartRepo := repository.NewCartRepo(pool)
		cart, err := cartRepo.GetOrCreate(context.Background(), a.userID)
		if err != nil {
			t.Fatal(err)
		}
		if err := cartRepo.AddItem(context.Background(), cart.ID, prod.ID, 1); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(context.Background(),
			`SELECT id FROM cart_items WHERE cart_id = $1 AND product_id = $2`, cart.ID, prod.ID).Scan(&a.itemID); err != nil {
			t.Fatal(err)
		}
	}

	type result struct {
		code int
		body string
	}
	results := make(chan result, 2)
	var wg sync.WaitGroup
	for _, a := range actors {
		wg.Add(1)
		go func(a actor) {
			defer wg.Done()
			body := `{"cart_item_ids":["` + a.itemID + `"],"address_id":"` + a.addrID + `"}`
			rec := checkoutRequest(t, h, userToken(t, a.userID, "user"), body)
			results <- result{code: rec.Code, body: rec.Body.String()}
		}(a)
	}
	wg.Wait()
	close(results)

	okCount, oosCount := 0, 0
	for res := range results {
		if res.code == http.StatusCreated {
			okCount++
		} else if res.code == http.StatusConflict && strings.Contains(res.body, "PRODUCT_OUT_OF_STOCK") {
			oosCount++
		}
	}
	if okCount != 1 || oosCount != 1 {
		t.Fatalf("ok=%d oos=%d, want exactly 1 success and 1 PRODUCT_OUT_OF_STOCK", okCount, oosCount)
	}

	var stock int
	if err := pool.QueryRow(context.Background(),
		`SELECT stock FROM products WHERE id = $1`, prod.ID).Scan(&stock); err != nil {
		t.Fatal(err)
	}
	if stock != 0 {
		t.Errorf("final stock = %d, want 0 (no overselling)", stock)
	}

	var orders int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM orders o
		 JOIN users u ON u.id = o.user_id
		 WHERE u.email LIKE 'race-user-%@example.com'`).Scan(&orders); err != nil {
		t.Fatal(err)
	}
	if orders != 1 {
		t.Errorf("orders = %d, want 1", orders)
	}
}
