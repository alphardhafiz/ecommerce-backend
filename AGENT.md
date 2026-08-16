# AGENT.md — Server (Go API)

Baca ini sebelum menulis kode apa pun. Sumber kebenaran: [`../docs/AGENTS.md`](../docs/AGENTS.md) dan [`../docs/PRD.md`](../docs/PRD.md). Aturan umum, stack, gotchas, dan alur kerja fase ada di sana — jangan diduplikasi di sini.

**Progress:** [`PLANNING.md`](PLANNING.md) adalah task list berjalan. Sebelum mulai kerja, cek PLANNING.md — ambil task pertama yang belum dicentang pada fase berjalan, kerjakan SATU task itu saja, lalu centang `[x]` setelah selesai (test/lint jalan). Jangan lompat fase, jangan kerjakan banyak task sekaligus.

## Aturan khusus backend

1. Struktur mengikuti PRD §M.2: `handler → service → repository`, tanpa layer tambahan. `payment/` dan `mail/` untuk integrasi eksternal, `jobs/` untuk scheduled job.
2. Skema DB hanya berubah lewat `golang-migrate`. Uang = `NUMERIC` di DB, `decimal`/integer di Go — jangan pernah `float` (PRD §D, AGENTS.md).
3. Checkout: harga/stock/quantity diambil ulang dari DB dengan row lock `FOR UPDATE` dalam satu transaction (PRD §C.8, §G). Request body hanya berisi referensi (ID + quantity), tidak pernah nilai finansial.
4. Webhook Midtrans wajib verifikasi signature (SHA-512) + server-to-server status check + idempotent (PRD §F.2, §R.11). Status order hanya berubah lewat alur PRD §F.
5. Perubahan status order dan payment selalu dalam satu DB transaction (PRD §F).
6. Response API mengikuti format PRD §L: `{success, data, meta}` / `{success:false, message, code, errors}`.
7. Redis optional (cache-aside), tidak boleh jadi titik gagal (PRD §H).
8. Endpoint baru → catat di PRD §E; tabel baru → catat di PRD §D.
9. Kode non-trivial wajib punya minimal 1 test. Domain kritis (checkout, payment, auth) ikut PRD §P, termasuk 1 concurrency test checkout stock=1.

## Perintah standar

| Aksi | Command |
|---|---|
| Infra lokal | `docker compose up -d` |
| Migration up | `migrate -path migrations -database "$DATABASE_URL" up` |
| Jalankan | `go run ./cmd/api` |
| Test | `go test ./...` |
| Lint | `golangci-lint run` |

## Aturan commit

**JANGAN commit, push, atau buat PR sebelum user review.** Tampilkan `git diff`/ringkasan perubahan, tunggu persetujuan eksplisit.
