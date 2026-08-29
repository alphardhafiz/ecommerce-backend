package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	jwtpkg "ecommerce/server/internal/jwt"
	"ecommerce/server/internal/middleware"
	"ecommerce/server/internal/repository"
	"ecommerce/server/internal/service"
)

func newOrderHandler(t *testing.T) (*Order, *pgxpool.Pool) {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set, skipping order handler test")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return NewOrder(service.NewOrderService(repository.NewOrderRepo(pool))), pool
}

// cleanupOrderUser removes orders (FK orders.user_id has no cascade) before
// the user row.
func cleanupOrderUser(t *testing.T, pool *pgxpool.Pool, email string) {
	t.Helper()
	ctx := context.Background()
	pool.Exec(ctx, `DELETE FROM orders WHERE user_id IN (SELECT id FROM users WHERE email = $1)`, email)
	pool.Exec(ctx, `DELETE FROM users WHERE email = $1`, email)
}

func checkoutRequest(t *testing.T, h *Order, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/orders/checkout", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	middleware.RequireAuth(jwtpkg.New("test-secret", jwtpkg.DefaultTTL))(http.HandlerFunc(h.Checkout)).ServeHTTP(rec, req)
	return rec
}

// seedCheckoutUser creates a user with a cart, one product line, and an
// address; returns userID, cartItemID, addressID, productID.
func seedCheckoutUser(t *testing.T, pool *pgxpool.Pool, email string, price int64, stock int64, qty int) (string, string, string, string) {
	t.Helper()
	seedUser(t, pool, email, "abc12345", "active")
	var userID string
	if err := pool.QueryRow(context.Background(), `SELECT id FROM users WHERE email = $1`, email).Scan(&userID); err != nil {
		t.Fatal(err)
	}

	prod := seedProduct(t, pool, "Produk Checkout", price, stock, nil)
	prodRepo := repository.NewProductRepo(pool)
	if _, err := prodRepo.SetActive(context.Background(), prod.ID, true); err != nil {
		t.Fatal(err)
	}

	addrRepo := repository.NewAddressRepo(pool)
	label := "Rumah"
	addr, err := addrRepo.Create(context.Background(), userID, modelAddress(label))
	if err != nil {
		t.Fatal(err)
	}

	cartRepo := repository.NewCartRepo(pool)
	cart, err := cartRepo.GetOrCreate(context.Background(), userID)
	if err != nil {
		t.Fatal(err)
	}
	if err := cartRepo.AddItem(context.Background(), cart.ID, prod.ID, qty); err != nil {
		t.Fatal(err)
	}
	var itemID string
	if err := pool.QueryRow(context.Background(),
		`SELECT id FROM cart_items WHERE cart_id = $1 AND product_id = $2`, cart.ID, prod.ID).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	return userID, itemID, addr.ID, prod.ID
}

func TestOrderCheckoutSuccess(t *testing.T) {
	h, pool := newOrderHandler(t)
	email := "checkout-ok@example.com"
	userID, itemID, addrID, prodID := seedCheckoutUser(t, pool, email, 50000, 10, 2)
	defer cleanupOrderUser(t, pool, email)

	rec := checkoutRequest(t, h, userToken(t, userID, "user"),
		`{"cart_item_ids":["`+itemID+`"],"address_id":"`+addrID+`"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201, body: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Data struct {
			OrderID     string `json:"order_id"`
			TotalAmount int64  `json:"total_amount"`
			Status      string `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Data.TotalAmount != 100000 {
		t.Errorf("total = %d, want 100000 (50000*2)", body.Data.TotalAmount)
	}
	if body.Data.Status != "PENDING" {
		t.Errorf("status = %s, want PENDING", body.Data.Status)
	}

	// stock decremented
	var stock int
	pool.QueryRow(context.Background(), `SELECT stock FROM products WHERE id = $1`, prodID).Scan(&stock)
	if stock != 8 {
		t.Errorf("stock = %d, want 8", stock)
	}

	// order snapshot
	var recipient, phone, addr string
	pool.QueryRow(context.Background(),
		`SELECT recipient_name, phone, shipping_address FROM orders WHERE id = $1`, body.Data.OrderID).
		Scan(&recipient, &phone, &addr)
	if recipient != "Budi" || phone != "08123456789" || !strings.Contains(addr, "Jl. Merdeka No. 1") {
		t.Errorf("snapshot = %s/%s/%s, want address snapshot", recipient, phone, addr)
	}

	// order_item snapshot + cart line removed
	var name string
	var price int64
	pool.QueryRow(context.Background(),
		`SELECT product_name, price FROM order_items WHERE order_id = $1`, body.Data.OrderID).Scan(&name, &price)
	if name != "Produk Checkout" || price != 50000 {
		t.Errorf("order_item snapshot = %s/%d", name, price)
	}
	var n int
	pool.QueryRow(context.Background(), `SELECT count(*) FROM cart_items WHERE id = $1`, itemID).Scan(&n)
	if n != 0 {
		t.Error("checked-out cart item must be deleted")
	}
}

func TestOrderCheckoutValidation(t *testing.T) {
	h, pool := newOrderHandler(t)
	email := "checkout-valid@example.com"
	userID, itemID, addrID, _ := seedCheckoutUser(t, pool, email, 50000, 10, 1)
	defer cleanupOrderUser(t, pool, email)
	token := userToken(t, userID, "user")

	cases := []struct {
		name string
		body string
		want string
	}{
		{"empty body", `{}`, "VALIDATION_ERROR"},
		{"empty cart ids", `{"cart_item_ids":[],"address_id":"` + addrID + `"}`, "VALIDATION_ERROR"},
		{"missing address", `{"cart_item_ids":["` + itemID + `"]}`, "VALIDATION_ERROR"},
		{"unknown item", `{"cart_item_ids":["00000000-0000-0000-0000-000000000000"],"address_id":"` + addrID + `"}`, "CART_EMPTY"},
		{"foreign item", `{"cart_item_ids":["` + itemID + `"],"address_id":"00000000-0000-0000-0000-000000000000"}`, "INVALID_ADDRESS"},
		{"invalid json", `{`, "INVALID_REQUEST"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := checkoutRequest(t, h, token, tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400, body: %s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tc.want) {
				t.Errorf("body = %s, want %s", rec.Body.String(), tc.want)
			}
		})
	}
}

func TestOrderCheckoutOutOfStock(t *testing.T) {
	h, pool := newOrderHandler(t)
	email := "checkout-oos@example.com"
	userID, itemID, addrID, prodID := seedCheckoutUser(t, pool, email, 50000, 1, 2)
	defer cleanupOrderUser(t, pool, email)

	rec := checkoutRequest(t, h, userToken(t, userID, "user"),
		`{"cart_item_ids":["`+itemID+`"],"address_id":"`+addrID+`"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409, body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "PRODUCT_OUT_OF_STOCK") {
		t.Errorf("body = %s, want PRODUCT_OUT_OF_STOCK", rec.Body.String())
	}

	var stock int
	pool.QueryRow(context.Background(), `SELECT stock FROM products WHERE id = $1`, prodID).Scan(&stock)
	if stock != 1 {
		t.Errorf("stock = %d, want unchanged 1", stock)
	}
}

func TestOrderCheckoutInactiveProduct(t *testing.T) {
	h, pool := newOrderHandler(t)
	email := "checkout-inactive@example.com"
	userID, itemID, addrID, prodID := seedCheckoutUser(t, pool, email, 50000, 10, 1)
	defer cleanupOrderUser(t, pool, email)

	prodRepo := repository.NewProductRepo(pool)
	if _, err := prodRepo.SetActive(context.Background(), prodID, false); err != nil {
		t.Fatal(err)
	}

	rec := checkoutRequest(t, h, userToken(t, userID, "user"),
		`{"cart_item_ids":["`+itemID+`"],"address_id":"`+addrID+`"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409, body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "PRODUCT_INACTIVE") {
		t.Errorf("body = %s, want PRODUCT_INACTIVE", rec.Body.String())
	}
}

func TestOrderGetUnauthorized(t *testing.T) {
	h, _ := newOrderHandler(t)
	rec := ordersRequest(t, h, http.MethodGet, "/orders", "", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	rec = ordersRequest(t, h, http.MethodGet, "/orders/x", "x", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("detail status = %d, want 401", rec.Code)
	}
}

func ordersRequest(t *testing.T, h *Order, method, path, id, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if id != "" {
		req.SetPathValue("id", id)
	}
	rec := httptest.NewRecorder()
	auth := middleware.RequireAuth(jwtpkg.New("test-secret", jwtpkg.DefaultTTL))
	if id != "" {
		auth(http.HandlerFunc(h.Get)).ServeHTTP(rec, req)
	} else {
		auth(http.HandlerFunc(h.List)).ServeHTTP(rec, req)
	}
	return rec
}

func TestOrderListAndDetail(t *testing.T) {
	h, pool := newOrderHandler(t)
	email := "orders-list@example.com"
	userID, itemID, addrID, _ := seedCheckoutUser(t, pool, email, 50000, 10, 2)
	defer cleanupOrderUser(t, pool, email)
	token := userToken(t, userID, "user")

	// checkout twice to have 2 orders
	rec := checkoutRequest(t, h, token, `{"cart_item_ids":["`+itemID+`"],"address_id":"`+addrID+`"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("checkout 1 status = %d", rec.Code)
	}
	var first struct {
		Data struct {
			OrderID string `json:"order_id"`
		} `json:"data"`
	}
	json.Unmarshal(rec.Body.Bytes(), &first)

	// refill cart and checkout again
	catRepo := repository.NewCartRepo(pool)
	cart, err := catRepo.GetOrCreate(context.Background(), userID)
	if err != nil {
		t.Fatal(err)
	}
	prod := seedProduct(t, pool, "Produk Kedua", 30000, 5, nil)
	defer pool.Exec(context.Background(), `DELETE FROM products WHERE id = $1`, prod.ID)
	prodRepo := repository.NewProductRepo(pool)
	if _, err := prodRepo.SetActive(context.Background(), prod.ID, true); err != nil {
		t.Fatal(err)
	}
	catRepo.AddItem(context.Background(), cart.ID, prod.ID, 1)
	var itemID2 string
	pool.QueryRow(context.Background(),
		`SELECT id FROM cart_items WHERE cart_id = $1 AND product_id = $2`, cart.ID, prod.ID).Scan(&itemID2)

	rec = checkoutRequest(t, h, token, `{"cart_item_ids":["`+itemID2+`"],"address_id":"`+addrID+`"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("checkout 2 status = %d, body: %s", rec.Code, rec.Body.String())
	}
	var second struct {
		Data struct {
			OrderID string `json:"order_id"`
		} `json:"data"`
	}
	json.Unmarshal(rec.Body.Bytes(), &second)

	// list: newest first, 2 orders, items included
	rec = ordersRequest(t, h, http.MethodGet, "/orders", "", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d", rec.Code)
	}
	var listBody struct {
		Data []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
			Total  int64  `json:"total_amount"`
			Items  []struct {
				ProductName string `json:"product_name"`
			} `json:"items"`
		} `json:"data"`
		Meta struct {
			Total int `json:"total"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listBody); err != nil {
		t.Fatal(err)
	}
	if listBody.Meta.Total != 2 || len(listBody.Data) != 2 {
		t.Fatalf("meta.total = %d, len = %d, want 2/2", listBody.Meta.Total, len(listBody.Data))
	}
	if listBody.Data[0].ID != second.Data.OrderID {
		t.Errorf("newest order = %s, want %s", listBody.Data[0].ID, second.Data.OrderID)
	}
	if listBody.Data[0].Total != 30000 || len(listBody.Data[0].Items) != 1 {
		t.Errorf("order 1 total/items = %d/%d, want 30000/1", listBody.Data[0].Total, len(listBody.Data[0].Items))
	}
	if listBody.Data[1].ID != first.Data.OrderID {
		t.Errorf("older order = %s, want %s", listBody.Data[1].ID, first.Data.OrderID)
	}
	if listBody.Data[1].Total != 100000 || len(listBody.Data[1].Items) != 1 {
		t.Errorf("order 2 total/items = %d/%d, want 100000/1", listBody.Data[1].Total, len(listBody.Data[1].Items))
	}

	// detail
	rec = ordersRequest(t, h, http.MethodGet, "/orders/"+second.Data.OrderID, second.Data.OrderID, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("detail status = %d", rec.Code)
	}
	var detail struct {
		Data struct {
			ID      string `json:"id"`
			Status  string `json:"status"`
			Items   []any  `json:"items"`
			Address string `json:"shipping_address"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.Data.Status != "PENDING" || len(detail.Data.Items) != 1 {
		t.Errorf("detail = %+v, want PENDING + 1 item", detail.Data)
	}
}

func TestOrderDetailOwnershipForbidden(t *testing.T) {
	h, pool := newOrderHandler(t)
	ownerEmail := "orders-owner@example.com"
	otherEmail := "orders-other@example.com"
	ownerID, itemID, addrID, _ := seedCheckoutUser(t, pool, ownerEmail, 50000, 10, 1)
	seedUser(t, pool, otherEmail, "abc12345", "active")
	defer cleanupOrderUser(t, pool, ownerEmail)
	defer cleanupOrderUser(t, pool, otherEmail)

	rec := checkoutRequest(t, h, userToken(t, ownerID, "user"),
		`{"cart_item_ids":["`+itemID+`"],"address_id":"`+addrID+`"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("checkout status = %d", rec.Code)
	}
	var body struct {
		Data struct {
			OrderID string `json:"order_id"`
		} `json:"data"`
	}
	json.Unmarshal(rec.Body.Bytes(), &body)

	var otherID string
	pool.QueryRow(context.Background(), `SELECT id FROM users WHERE email = $1`, otherEmail).Scan(&otherID)

	// other user cannot view owner's order -> 403
	rec = ordersRequest(t, h, http.MethodGet, "/orders/"+body.Data.OrderID, body.Data.OrderID, userToken(t, otherID, "user"))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}

	// nonexistent order -> 404
	rec = ordersRequest(t, h, http.MethodGet, "/orders/00000000-0000-0000-0000-000000000000", "00000000-0000-0000-0000-000000000000", userToken(t, otherID, "user"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}
