# Repo Scan — Invoice Upload Feature

## Stack

- **Backend**: Go 1.21+, Gin HTTP framework, SQLite (modernc.org/sqlite), sqlx ORM
- **Frontend**: Vue 3.4 + TypeScript, Pinia stores, Vue Router, Vite build
- **Embedded SPA**: Frontend dist is embedded into the Go binary via `go:embed`
- **Storage**: Local filesystem for uploaded files, SQLite for metadata

## Project Structure

```
├── cmd/server/main.go          — Entry point, delegates to app.Run()
├── backend/
│   ├── app/run.go              — Bootstrap: config, DB, services, HTTP server
│   ├── internal/
│   │   ├── config/             — YAML config loader
│   │   ├── db/db.go            — SQLite open + migrations (user_version=3)
│   │   ├── ingest/ingest.go    — CSV importer for order data (21-25订单.csv)
│   │   ├── orders/service.go   — Order CRUD, search, admin list, progress
│   │   ├── uploads/service.go  — Multipart upload handler, JPEG materialization
│   │   ├── uploads/delete.go   — Upload deletion logic
│   │   ├── admin/service.go    — Admin routes, session auth, CSRF, exports
│   │   ├── storage/storage.go  — Filesystem paths, locking, path validation
│   │   ├── pdfmerge/           — Merge uploaded images into PDF
│   │   ├── httpapi/router.go   — Route registration, SPA fallback, timeouts
│   │   ├── httpapi/limits/     — Concurrency semaphore manager
│   │   ├── httpapi/middleware.go — Security headers, request logging
│   │   ├── apierror/           — Typed API error handling
│   │   └── metrics/            — Prometheus metrics
│   └── tests/integration/      — Integration tests
├── frontend/
│   ├── src/
│   │   ├── App.vue             — Root component
│   │   ├── main.ts             — Vue app bootstrap
│   │   ├── router.ts           — 7 routes: / → /y{year}, /admin/login, /admin
│   │   ├── lib/api.ts          — Typed fetch wrapper, collectionApi + adminApi
│   │   ├── lib/imagePipeline.ts— Client-side HEIC→JPEG, resize pipeline
│   │   ├── lib/filename.ts     — Filename utilities
│   │   ├── stores/collection.ts— Collection page state: search, detail, upload
│   │   ├── stores/admin.ts     — Admin panel state: orders, detail, CRUD
│   │   ├── stores/ui.ts        — Toast notifications
│   │   ├── views/CollectionView.vue — Per-year collection page
│   │   ├── views/AdminView.vue — Admin back-office
│   │   ├── views/AdminLoginView.vue — Admin login
│   │   └── components/         — SearchBar, OrderDetailPanel, UploadCard, etc.
│   └── dist/                   — Built frontend assets (embedded)
├── config.yaml                 — Runtime config
├── 21-25订单.csv               — Source order data (imported on first boot)
└── 油脂发票.csv                — Source invoice data (46K+ rows, new feature)
```

## Database Schema (SQLite, user_version=3)

- **orders**: (year, order_no) PK, customer, customer_clean, csv_present, check_status
- **order_lines**: id PK, year, order_no, order_date, customer, product, quantity, amount, etc.
- **uploads**: id PK, year, order_no, kind CHECK('合同','发票','发货单'), seq, filename, byte_size, sha256, operator

## Existing Patterns

1. **CSV Import**: `ingest.Importer.ImportCSV()` reads CSV → inserts into `order_lines` + upserts `orders`. Runs on first boot if tables empty.
2. **Search**: Orders searched by `order_no LIKE '%' || ? || '%'`, minimum 2 chars, returns up to 50 results with upload counts.
3. **Upload Flow**: Multipart POST → stream to temp → decode image → re-encode JPEG → store in `data/uploads/{year}/{orderNo}/` → insert DB record.
4. **Admin**: Session-based auth (HMAC-signed cookie), CSRF via `X-Admin-Csrf` header. Admin routes under `/api/admin/`.
5. **Frontend**: Vue 3 SPA with Pinia stores. `collectionApi` for public endpoints, `adminApi` for admin. Search debounced at 250ms.

## Invoice CSV Structure (油脂发票.csv)

~46,206 rows. Key columns:
- 合并发票号码 (col 5) — primary search key
- 购买方名称 (col 9) — customer name
- 开票日期 (col 10) — invoice date
- 销方名称 (col 7) — seller name
- 金额 (col 18), 税额 (col 20), 价税合计 (col 21)
- 年 (col 29) — year prefix like "Y2021"

Multiple rows can share the same 合并发票号码 (one invoice can have multiple line items).

## Config (config.yaml)

- `csv_path`: path to order CSV
- `data_dir`: root for uploads and DB
- `admin_password`: admin login
- `limits`: per_kind_max=50, file size limits, concurrency caps
- `image.accepted_mime`: jpeg, png, webp
