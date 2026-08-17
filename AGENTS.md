# AGENTS.md — Server (Go API)

## Sebelum mulai bekerja

1. Baca dokumen ini sampai selesai.
2. Sumber kebenaran ada di [`../docs/AGENTS.md`](../docs/AGENTS.md) dan [`../docs/PRD.md`](../docs/PRD.md). Jangan duplikasi aturan yang sudah ada di sana.
3. Baca [`PLANNING.md`](PLANNING.md) — daftar task berjalan.

## Alur kerja (ikuti urutan ini)

1. Ambil task pertama yang belum dicentang (`[ ]`) pada fase berjalan di PLANNING.md.
2. Kerjakan SATU task itu saja. Dilarang mengerjakan banyak task sekaligus atau melompat fase.
3. Setelah selesai (test + lint hijau), centang `[x]` di PLANNING.md.
4. Untuk task baru: buat GitHub issue dulu (lihat aturan issue di bawah) sebelum mengerjakan.

## Aturan mulai & berhenti kerja (PENTING, sering salah)

1. **Membuat issue ≠ mengerjakan issue.** Kalau user hanya minta "buat github issue", berhenti SETELAH issue dibuat. JANGAN lanjut eksekusi.
2. **Mulai menulis kode HANYA setelah user memerintah eksplisit**, contoh: "eksekusi", "kerjakan", "lanjut".
3. **Setiap tahap selesai → berhenti dan lapor singkat.** Jangan melanjutkan ke tahap berikutnya (commit, centang PLANNING, close issue, task berikutnya) tanpa perintah user.
4. **Sebelum mulai bekerja, baca ulang pesan terakhir user.** Kalau ragu apa yang diminta, tanya dulu — jangan asumsikan "sekaligus kerjakan".

## Aturan khusus backend

1. **Struktur kode**: ikuti PRD §M.2 — `handler → service → repository`. Jangan tambah layer baru. `payment/` dan `mail/` hanya untuk integrasi eksternal, `jobs/` untuk scheduled job.
2. **Skema DB**: hanya diubah lewat file migrasi `golang-migrate`. Tidak boleh edit tabel langsung.
3. **Uang**: `NUMERIC` di PostgreSQL, `decimal`/integer di Go. DILARANG pakai `float` (PRD §D).
4. **Checkout**: harga, stock, quantity SELALU diambil ulang dari DB dengan `SELECT ... FOR UPDATE` dalam satu transaction (PRD §C.8, §G). Request body hanya berisi referensi (ID + quantity), tidak pernah nilai finansial.
5. **Webhook Midtrans**: wajib verifikasi signature (SHA-512), status check server-to-server, dan idempotent (PRD §F.2, §R.11).
6. **Status order**: hanya boleh berubah lewat alur PRD §F. Perubahan status order dan payment selalu dalam satu DB transaction.
7. **Response API**: ikuti format PRD §L — sukses `{success, data, meta}`, error `{success:false, message, code, errors}`.
8. **Redis**: opsional (cache-aside). Jangan jadikan titik gagal — aplikasi harus tetap jalan saat Redis down (PRD §H).
9. **Dokumentasi perubahan**: endpoint baru → catat di PRD §E; tabel baru → catat di PRD §D.
10. **Testing**: setiap kode non-trivial (ada branch/loop/money path) wajib punya minimal 1 test yang bisa dijalankan. Domain kritis (checkout, payment, auth) ikut PRD §P, termasuk 1 concurrency test checkout stock=1.
11. **Saat bingung**: baca edge case di PRD §S sebelum mengerjakan fitur. Kalau PRD tidak menjawab, tanya ke user. JANGAN mengarang solusi sendiri.

## Perintah standar

| Aksi | Command |
|---|---|
| Infra lokal | `docker compose up -d` |
| Migration up | `migrate -path migrations -database "$DATABASE_URL" up` |
| Jalankan | `go run ./cmd/api` |
| Test | `go test ./...` |
| Lint | `golangci-lint run` |

## Aturan GitHub issue

Saat user minta dibuatkan GitHub issue untuk sebuah task:

1. Buat planning HIGH LEVEL saja. Jangan tulis detail low-level (kolom SQL, nama variabel, langkah teknis rinci).
2. Format issue: konteks singkat, tujuan, pointer ke bagian PRD sebagai sumber kebenaran, dan acceptance criteria.
3. Alasan: yang mengerjakan issue adalah junior programmer atau AI model murah. Mereka harus belajar dari PRD, bukan menerima jawaban jadi.

## Aturan commit

1. **JANGAN commit, push, atau buat PR sebelum user review** dan mendapat persetujuan eksplisit. Tampilkan `git diff`/ringkasan perubahan dulu.
2. **Checkpoint wajib di akhir setiap task** (selalu lakukan, tanpa kecuali):
   1. Jalankan `git status` + `git diff --stat`, tampilkan ringkasan ke user.
   2. Tanya: "Commit + push + close issue?" — lalu **BERHENTI**.
   3. TIDAK boleh lanjut ke perintah `git commit`/`git push`/`gh issue close` sampai user menjawab setuju secara eksplisit.
3. Pesan commit dalam bahasa Inggris, deskriptif, mengikuti conventional commits (contoh: `feat:`, `fix:`, `chore:`, `docs:`).
4. Setiap task selesai → tandai `[x]` di PLANNING.md sebelum atau bersama commit.
