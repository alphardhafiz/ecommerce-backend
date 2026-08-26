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
	"ecommerce/server/internal/model"
	"ecommerce/server/internal/repository"
	"ecommerce/server/internal/service"
)

func newAddressHandler(t *testing.T) (*Address, *pgxpool.Pool) {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set, skipping address handler test")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return NewAddress(service.NewAddressService(repository.NewAddressRepo(pool))), pool
}

func addressRequest(t *testing.T, h *Address, method, path, id string, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if id != "" {
		req.SetPathValue("id", id)
	}
	rec := httptest.NewRecorder()
	auth := middleware.RequireAuth(jwtpkg.New("test-secret", jwtpkg.DefaultTTL))
	var hf func(http.ResponseWriter, *http.Request)
	switch method {
	case http.MethodGet:
		hf = h.List
	case http.MethodPost:
		hf = h.Create
	case http.MethodPut:
		hf = h.Update
	case http.MethodDelete:
		hf = h.Delete
	default:
		hf = h.SetDefault
	}
	auth(http.HandlerFunc(hf)).ServeHTTP(rec, req)
	return rec
}

func validAddressBody() string {
	return `{"label":"Rumah","recipient_name":"Budi","phone":"08123456789",
		"full_address":"Jl. Merdeka No. 1","city":"Jakarta","province":"DKI Jakarta",
		"postal_code":"10110","is_default":true}`
}

func modelAddress(label string) *model.Address {
	return &model.Address{
		Label:         &label,
		RecipientName: "Budi",
		Phone:         "08123456789",
		FullAddress:   "Jl. Merdeka No. 1",
		City:          "Jakarta",
		Province:      "DKI Jakarta",
		PostalCode:    "10110",
	}
}

func seedAddress(t *testing.T, pool *pgxpool.Pool, userID string, label string) string {
	t.Helper()
	repo := repository.NewAddressRepo(pool)
	a, err := repo.Create(context.Background(), userID, modelAddress(label))
	if err != nil {
		t.Fatal(err)
	}
	return a.ID
}

func TestAddressCRUD(t *testing.T) {
	h, pool := newAddressHandler(t)
	email := "addr-crud@example.com"
	seedUser(t, pool, email, "abc12345", "active")
	defer pool.Exec(context.Background(), `DELETE FROM users WHERE email = $1`, email)

	var userID string
	pool.QueryRow(context.Background(), `SELECT id FROM users WHERE email = $1`, email).Scan(&userID)
	token := userToken(t, userID, "user")

	// create
	rec := addressRequest(t, h, http.MethodPost, "/addresses", "", token, validAddressBody())
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201, body: %s", rec.Code, rec.Body.String())
	}
	var created struct {
		Data struct {
			ID        string `json:"id"`
			IsDefault bool   `json:"is_default"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if !created.Data.IsDefault {
		t.Error("first created address must be default (is_default=true in body)")
	}

	// list
	rec = addressRequest(t, h, http.MethodGet, "/addresses", "", token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200", rec.Code)
	}
	var listed struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Data) != 1 {
		t.Fatalf("list length = %d, want 1", len(listed.Data))
	}

	// update
	rec = addressRequest(t, h, http.MethodPut, "/addresses/"+created.Data.ID, created.Data.ID, token,
		`{"label":"Kantor","recipient_name":"Budi","phone":"08129876543",
		"full_address":"Jl. Sudirman No. 2","city":"Jakarta","province":"DKI Jakarta",
		"postal_code":"10220","is_default":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("update status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
	var phone string
	pool.QueryRow(context.Background(), `SELECT phone FROM addresses WHERE id = $1`, created.Data.ID).Scan(&phone)
	if phone != "08129876543" {
		t.Errorf("phone = %s, want 08129876543 after update", phone)
	}

	// delete
	rec = addressRequest(t, h, http.MethodDelete, "/addresses/"+created.Data.ID, created.Data.ID, token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status = %d, want 200", rec.Code)
	}
	var n int
	pool.QueryRow(context.Background(), `SELECT count(*) FROM addresses WHERE id = $1`, created.Data.ID).Scan(&n)
	if n != 0 {
		t.Error("address must be deleted")
	}
}

func TestAddressSetDefaultSingleDefault(t *testing.T) {
	h, pool := newAddressHandler(t)
	email := "addr-default@example.com"
	seedUser(t, pool, email, "abc12345", "active")
	defer pool.Exec(context.Background(), `DELETE FROM users WHERE email = $1`, email)

	var userID string
	pool.QueryRow(context.Background(), `SELECT id FROM users WHERE email = $1`, email).Scan(&userID)
	token := userToken(t, userID, "user")

	id1 := seedAddress(t, pool, userID, "Rumah")
	id2 := seedAddress(t, pool, userID, "Kantor")

	// set default to id2
	rec := addressRequest(t, h, http.MethodPatch, "/addresses/"+id2+"/default", id2, token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("set default status = %d, want 200", rec.Code)
	}

	var defaults int
	pool.QueryRow(context.Background(),
		`SELECT count(*) FROM addresses WHERE user_id = $1 AND is_default = true`, userID).Scan(&defaults)
	if defaults != 1 {
		t.Errorf("default count = %d, want exactly 1", defaults)
	}
	var defaultID string
	pool.QueryRow(context.Background(),
		`SELECT id FROM addresses WHERE user_id = $1 AND is_default = true`, userID).Scan(&defaultID)
	if defaultID != id2 {
		t.Errorf("default id = %s, want %s", defaultID, id2)
	}
	var id1Default bool
	pool.QueryRow(context.Background(), `SELECT is_default FROM addresses WHERE id = $1`, id1).Scan(&id1Default)
	if id1Default {
		t.Error("id1 must no longer be default after switching to id2")
	}
}

func TestAddressValidation(t *testing.T) {
	h, pool := newAddressHandler(t)
	email := "addr-valid@example.com"
	seedUser(t, pool, email, "abc12345", "active")
	defer pool.Exec(context.Background(), `DELETE FROM users WHERE email = $1`, email)

	var userID string
	pool.QueryRow(context.Background(), `SELECT id FROM users WHERE email = $1`, email).Scan(&userID)
	token := userToken(t, userID, "user")

	cases := []struct {
		name string
		body string
	}{
		{"empty", `{}`},
		{"bad postal", `{"recipient_name":"Budi","phone":"0812","full_address":"Jl. A","city":"Jakarta","province":"DKI","postal_code":"abc"}`},
		{"postal too short", `{"recipient_name":"Budi","phone":"0812","full_address":"Jl. A","city":"Jakarta","province":"DKI","postal_code":"123"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := addressRequest(t, h, http.MethodPost, "/addresses", "", token, tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400, body: %s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), "VALIDATION_ERROR") {
				t.Errorf("body = %s, want VALIDATION_ERROR", rec.Body.String())
			}
		})
	}
}

func TestAddressOwnershipForbidden(t *testing.T) {
	h, pool := newAddressHandler(t)
	ownerEmail := "addr-owner@example.com"
	otherEmail := "addr-other@example.com"
	seedUser(t, pool, ownerEmail, "abc12345", "active")
	seedUser(t, pool, otherEmail, "abc12345", "active")
	defer pool.Exec(context.Background(), `DELETE FROM users WHERE email = ANY($1)`, []string{ownerEmail, otherEmail})

	var ownerID, otherID string
	pool.QueryRow(context.Background(), `SELECT id FROM users WHERE email = $1`, ownerEmail).Scan(&ownerID)
	pool.QueryRow(context.Background(), `SELECT id FROM users WHERE email = $1`, otherEmail).Scan(&otherID)

	id := seedAddress(t, pool, ownerID, "Rumah")
	otherToken := userToken(t, otherID, "user")

	for _, tc := range []struct {
		name string
		body string
	}{
		{"update", validAddressBody()},
		{"delete", ""},
		{"set default", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var rec *httptest.ResponseRecorder
			switch tc.name {
			case "update":
				rec = addressRequest(t, h, http.MethodPut, "/addresses/"+id, id, otherToken, tc.body)
			case "delete":
				rec = addressRequest(t, h, http.MethodDelete, "/addresses/"+id, id, otherToken, tc.body)
			default:
				rec = addressRequest(t, h, http.MethodPatch, "/addresses/"+id+"/default", id, otherToken, tc.body)
			}
			if rec.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403", rec.Code)
			}
		})
	}
}

func TestAddressMutationsUnauthorized(t *testing.T) {
	h, _ := newAddressHandler(t)
	if rec := addressRequest(t, h, http.MethodGet, "/addresses", "", "", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("list status = %d, want 401", rec.Code)
	}
	if rec := addressRequest(t, h, http.MethodPost, "/addresses", "", "", validAddressBody()); rec.Code != http.StatusUnauthorized {
		t.Fatalf("create status = %d, want 401", rec.Code)
	}
}
