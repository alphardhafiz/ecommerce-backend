# PLANNING.md — Server (Go API)

Pecahan task per fase mengikuti PRD §U (Development Roadmap). Sumber kebenaran: [`../docs/PRD.md`](../docs/PRD.md) + [`../docs/AGENTS.md`](../docs/AGENTS.md).

**Cara pakai:**
- Kerjakan berurutan per task. Jangan mulai fase berikutnya sebelum DoD fase sekarang terpenuhi.
- Satu saat = satu task. Centang `[x]` hanya setelah task selesai (test + lint jalan).
- Task belum dipecah detail → pecah dulu sebelum coding (docs/AGENTS.md aturan 3).
- Endpoint baru wajib dicatat di PRD §E; tabel baru di PRD §D.

## Fase 1 — Project setup + database

- [x] Init repo Go (`go mod init`), `.gitignore`, `.env.example`
- [x] `docker-compose.yml` lokal: PostgreSQL 16 + Redis 7
- [x] Struktur folder per PRD §M.2 (`cmd/`, `internal/{config,handler,service,repository,middleware,model,cache,payment,mail,jobs}`, `pkg/`, `migrations/`)
- [x] Config loader (`internal/config`) — baca env, validasi wajib
- [x] Structured logger + request ID middleware (`pkg/logger`, `internal/middleware`)
- [x] Migrasi: `users`, `refresh_tokens`, `password_reset_tokens`
- [x] Migrasi: `categories`, `products`, `product_images` (+ `pg_trgm` untuk search)
- [x] Migrasi: `wishlists`, `carts`, `cart_items`, `addresses`
- [x] Migrasi: `orders`, `order_items`, `payments`, `payment_notifications`
- [x] Koneksi pgx (pool) + Redis client wrapper (`internal/cache`)
- [x] `GET /health` + `GET /health/ready` (ping DB & Redis)
- [x] Test: migration up bersih, health check 200

**DoD (PRD §U.1):** semua tabel ter-migrate, app connect DB & Redis, health check jalan.

## Fase 2 — Authentication + RBAC

- [x] Repository `users` (create, find by email, update)
- [x] JWT helper (HS256, claims `user_id`/`role`/`jti`, exp 15m) + validator
- [x] `POST /auth/register` (bcrypt cost 12, validasi email/password)
- [x] `POST /auth/login` (access token di body, refresh token httpOnly cookie)
- [x] `POST /auth/refresh` (rotation + reuse detection → revoke semua sesi + CSRF double-submit)
- [x] `POST /auth/logout` (revoke refresh token, hapus cookie)
- [x] Integrasi Resend (`internal/mail`) + `POST /auth/forgot-password` (response generic, kirim email async)
- [x] `POST /auth/reset-password` (invalidate token + semua refresh token user)
- [x] Middleware `RequireAuth` + `RequireRole("admin")`
- [x] `GET /users/me`, `PATCH /users/me` (tanpa field `role` dari body)
- [x] `GET /admin/users`, `PATCH /admin/users/:id/status`
- [x] Test: auth_service unit (termasuk refresh reuse detection, reset revoke semua sesi)

**DoD (PRD §U.2):** semua endpoint `/auth/*` berfungsi, middleware role-check teruji.

## Fase 3 — Product + category

- [x] Repository categories + products (soft delete, partial index)
- [x] `GET /categories`, `POST/PUT/DELETE /admin/categories`
- [x] `POST /admin/products` + `PUT/DELETE /admin/products/:id` (soft delete)
- [x] `PATCH /admin/products/:id/status`, `PATCH /admin/products/:id/stock`
- [x] `GET /products` (pagination, search `ILIKE`, filter category/harga/stock, sort)
- [x] `GET /products/:id` (detail + images + kategori)
- [x] Upload image ke object storage (`internal/` client) + `POST/DELETE /admin/products/:id/images[/:imageId]` (validasi MIME/ukuran, rename UUID)
- [x] Test: listing filter/sort, soft delete tidak muncul di publik

**DoD (PRD §U.3):** admin kelola produk lengkap dengan gambar; user bisa browse & search.

## Fase 4 — Wishlist + cart

- [x] `GET/POST/DELETE /wishlist[/:productId]` (unique constraint → 409, exclude soft-deleted)
- [x] Cart lazy creation (1 user = 1 cart), repo cart + cart_items
- [x] `GET /cart` (subtotal on-the-fly dari harga DB, flag `is_available`)
- [x] `POST /cart/items` (merge quantity), `PATCH /cart/items/:id`, `DELETE /cart/items/:id`, `DELETE /cart`
- [x] Test: duplikat wishlist 409, item inactive `is_available:false` tidak masuk total
- [x] `GET /cart` sertakan `primary_image` per item (subquery `product_images`, pola sama wishlist)

**DoD (PRD §U.4):** wishlist & cart end-to-end, termasuk handling produk i/nactive/dihapus.

## Fase 5 — Address + order (tanpa payment)

- [x] CRUD `/addresses` + `PATCH /addresses/:id/default` (satu default, transaction)
- [x] `POST /orders/checkout`: DB transaction, `SELECT ... FOR UPDATE` (urutan kunci konsisten), validasi ulang harga/stock/is_active, snapshot address & order_items, kurangi stock, hapus cart items, payment stub
- [x] `GET /orders` (pagination, milik sendiri), `GET /orders/:id` (ownership check → 403)
- [x] `POST /orders/:id/cancel` (hanya PENDING, stock kembali dalam transaction)
- [x] `GET /admin/orders`, `PATCH /admin/orders/:id/status` (state transition PRD §C.9, final state terkunci)
- [x] Test: **concurrency checkout stock=1** (2 goroutine, hanya 1 sukses)

**DoD (PRD §U.5):** checkout membuat order PENDING dengan stock berkurang benar, concurrency test lulus.

## Fase 6 — Midtrans integration

- [x] Midtrans Snap client (`internal/payment`): create transaction, status check
- [ ] Integrasi ke checkout: insert `payments`, rollback semua jika Midtrans gagal
- [ ] `POST /payments/webhook`: verifikasi signature SHA-512 + server-to-server status check
- [ ] Idempotency: cek status sebelum update (row lock), log `payment_notifications`, duplicate → tetap 200
- [ ] Handle `success`/`expire`/`cancel` (stock kembali di transaction yang sama, `orders` + `payments` satu transaction)
- [ ] Scheduled job expire order PENDING > 60 menit (`internal/jobs`)
- [ ] `GET /orders/:id/payment` (ownership check)
- [ ] Test: webhook payload valid/invalid signature + duplicate delivery

**DoD (PRD §U.6):** full flow sandbox berhasil: checkout → bayar → webhook → order PAID.

## Fase 7 — Admin dashboard

- [ ] `GET /admin/dashboard` (total users/products/orders per status/revenue/low stock + filter `period`)
- [ ] Test: angka sesuai data seed

**DoD (PRD §U.7):** dashboard menampilkan angka akurat sesuai DB.

## Fase 8 — Redis + optimization

- [ ] Product list cache (`products:list:{hash}`, TTL 5m, invalidasi prefix via SCAN+DEL)
- [ ] Product detail cache (`product:detail:{id}`, TTL 10m, invalidasi targeted)
- [ ] Category list cache (`categories:active`, TTL 30m)
- [ ] Rate limit Redis: login/register strict (fail-closed), endpoint umum (fail-open)
- [ ] Test: fallback saat Redis down

**DoD (PRD §U.8):** cache hit terverifikasi, rate limit teruji, fallback Redis down bekerja.

## Fase 9 — Testing

- [ ] Unit test service domain kritis (checkout, payment, auth)
- [ ] Integration test repository (test DB terpisah/testcontainers)
- [ ] API test `httptest` (status code, response shape, middleware auth)
- [ ] Webhook test lengkap (idempotency, state transition)

**DoD (PRD §U.9):** coverage domain kritis ≥ 70%, E2E flow minimal lulus di CI.

## Fase 10 — Deployment

- [ ] Dockerfile + docker-compose production + Nginx + HTTPS (Let's Encrypt)
- [ ] Backup `pg_dump` cron (retensi 7 hari)
- [ ] Health check Docker + uptime monitor eksternal

**DoD (PRD §U.10):** aplikasi diakses via domain publik, checkout end-to-end di production sandbox.
