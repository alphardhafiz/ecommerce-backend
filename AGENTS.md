# AGENTS.md — Server (Go API)

## Sebelum mulai
1. Baca dokumen ini sampai selesai.
2. Sumber kebenaran: [`../docs/AGENTS.md`](../docs/AGENTS.md), [`../docs/PRD.md`](../docs/PRD.md). Jangan duplikasi.
3. Baca [`PLANNING.md`](PLANNING.md) — daftar task berjalan.

## Alur kerja
1. Ambil task pertama yang belum `[x]` di fase berjalan.
2. Kerjakan **1 task/sesi**. Tidak boleh lompat fase / gabung task.
3. Selesai (test+lint lolos) → centang `[x]`.
4. Task baru → buat GitHub issue dulu (lihat §Issue), baru kerjakan.

## Start/stop (sering salah, WAJIB patuh)
- Buat issue ≠ kerjakan issue. Diminta "buat issue" saja → stop setelah issue dibuat.
- Mulai coding hanya setelah perintah eksplisit ("eksekusi"/"kerjakan"/"lanjut").
- Tiap tahap selesai → stop, lapor singkat. Jangan lanjut ke tahap berikutnya (commit/centang/close issue/task baru) tanpa perintah user.
- Ragu maksud user → tanya, jangan asumsi "sekaligus kerjakan".

## Aturan backend
1. Struktur: `handler → service → repository` (PRD §M.2). Jangan tambah layer baru. `payment/`, `mail/` = integrasi eksternal; `jobs/` = scheduled job.
2. Skema DB hanya lewat migrasi `golang-migrate`. Tidak edit tabel langsung.
3. Uang: `NUMERIC` (Postgres), `decimal`/integer (Go). Dilarang `float` (PRD §D).
4. Checkout: harga/stock/qty selalu diambil ulang dari DB dengan `SELECT ... FOR UPDATE` dalam 1 transaction (PRD §C.8, §G). Request body cuma ID+qty, tanpa nilai finansial.
5. Webhook Midtrans: wajib verifikasi signature (SHA-512), status check server-to-server, idempotent (PRD §F.2, §R.11).
6. Status order hanya berubah lewat alur PRD §F. Perubahan status order+payment selalu 1 DB transaction.
7. Response API ikuti PRD §L — sukses `{success, data, meta}`, error `{success:false, message, code, errors}`.
8. Redis opsional (cache-aside), bukan titik gagal — app tetap jalan saat Redis down (PRD §H).
9. Endpoint baru → catat PRD §E. Tabel baru → catat PRD §D.
10. Testing: kode non-trivial (branch/loop/money path) wajib ≥1 test jalan. Domain kritis (checkout/payment/auth) ikut PRD §P, termasuk 1 concurrency test checkout stock=1.
11. Bingung → cek edge case PRD §S dulu. PRD tidak menjawab → tanya user. Jangan mengarang solusi sendiri.

## Command
| Aksi | Command |
|---|---|
| Infra lokal | `docker compose up -d` |
| Migration up | `migrate -path migrations -database "$DATABASE_URL" up` |
| Jalankan | `go run ./cmd/api` |
| Test | `go test ./...` |
| Lint | `golangci-lint run` |

## Issue
Diminta buatkan GitHub issue:
1. Planning high-level saja, tanpa detail teknis rinci (kolom SQL/nama variabel/langkah rinci).
2. Format: konteks singkat, tujuan, pointer PRD, acceptance criteria.

## Commit
1. Jangan commit/push/PR sebelum user review & approve eksplisit. Tampilkan `git diff`/ringkasan dulu.
2. Checkpoint wajib tiap task selesai:
   - `git status` + `git diff --stat` → ringkas ke user.
   - Tanya "Commit + push + close issue?" → **STOP**.
   - Tidak lanjut `git commit`/`push`/`gh issue close` sebelum user setuju eksplisit.
3. Commit message: English, conventional commits (`feat:`, `fix:`, `chore:`, `docs:`).
4. Task selesai → centang `[x]` di PLANNING.md sebelum/bersama commit.