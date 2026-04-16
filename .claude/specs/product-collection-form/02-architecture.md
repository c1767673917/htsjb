# Architecture — Product Order Documentation Collection Form

Companion to `01-requirements.md`. Single binary, LAN deployment.

## 1. High-Level Topology

```
┌────────────────────────────────────────────────────────┐
│                   Operator's phone                     │
│  Mobile Safari / Chrome / WeChat webview               │
│  (HEIC→JPEG, resize, compress, preview, submit)        │
└────────────────┬───────────────────────────────────────┘
                 │  HTTP (LAN, 0.0.0.0:8080)
                 │  - static SPA assets
                 │  - /api/*
                 ▼
┌────────────────────────────────────────────────────────┐
│         Single Go binary (Gin)  ← embed:dist           │
│  Router ──────────────────────────────────────────┐    │
│  ├── GET  /*              (serve Vue SPA)         │    │
│  ├── /api/y{year}/*       (collection endpoints)  │    │
│  └── /api/admin/*         (protected endpoints)   │    │
│                                                   │    │
│  Services: ingest, order query, upload, pdf       │    │
│            merge, zip export, admin               │    │
│                                                   ▼    │
│  Storage:                                              │
│  ├── ./data/app.db (SQLite, WAL)                       │
│  └── ./data/uploads/{year}/{order}/{files + merged.pdf}│
└────────────────────────────────────────────────────────┘
```

## 2. Tech Stack

### Frontend
| Concern | Choice | Notes |
|--------|--------|-------|
| Framework | Vue 3.4+ `<script setup>` | Composition API everywhere |
| Build | Vite 5 + TypeScript | Output to `frontend/dist`, embedded by Go |
| Routing | Vue Router 4 | 5 `/y{year}` routes + `/admin/*` |
| State | Pinia | 2 stores: `collection`, `admin` |
| HTTP | native `fetch` with a tiny `api.ts` wrapper | No axios |
| HEIC | `heic2any` (lazy-imported only when MIME is heic) | keeps main bundle light |
| Image resize | Canvas 2D + `createImageBitmap` → `canvas.toBlob('image/jpeg', 0.85)` | No third-party |
| Toast | Tiny in-house component | 2.5 s auto-dismiss |
| PWA | **No** (explicit non-goal) | |
| Styles | Plain CSS + CSS variables + a `mobile.css` breakpoint layer | No Tailwind |

### Backend
| Concern | Choice | Notes |
|--------|--------|-------|
| Runtime | Go 1.22+ | |
| Web | `github.com/gin-gonic/gin` | |
| DB | `modernc.org/sqlite` (pure Go) | WAL mode |
| SQL helper | `github.com/jmoiron/sqlx` | struct-scan convenience |
| CSV ingest | `encoding/csv` stdlib | Streaming |
| PDF | `github.com/jung-kurt/gofpdf/v2` | JPEG pages, A4 portrait |
| Zip export | `archive/zip` stdlib (streaming) | |
| File ops | `os`, `io`, `path/filepath` | `filepath.Clean` path-traversal guard |
| Config | `gopkg.in/yaml.v3` | single file `config.yaml` |
| Logging | `log/slog` stdlib | |
| Tests | `testing` + `net/http/httptest` | |

### Build & Deploy
- Frontend: `pnpm build` → `frontend/dist/` (committed to the Go embed via
  `//go:embed all:dist`).
- Backend: `go build -trimpath -o server ./cmd/server`.
- Runtime dependencies: none (pure Go). `./server` + `./config.yaml` +
  `./21-25订单.csv` on first run.

## 3. Component Classification

Every shipping component is tagged `frontend`, `backend`, or `fullstack`.
Dispatching is mechanical: `requirements-frontend` implements `frontend`
components and the frontend portion of `fullstack`; Codex implements `backend`
components and the backend portion of `fullstack`.

| Component | Type | Scope |
|-----------|------|-------|
| `frontend/src/App.vue` + `router.ts` | frontend | Shell + 7 routes (`/y{year}` ×5 + `/admin/login` + `/admin`) |
| `frontend/src/views/CollectionView.vue` | frontend | The `/y{year}` mobile collection page (progress block, search, detail) |
| `frontend/src/views/AdminLoginView.vue` | frontend | Password prompt for admin |
| `frontend/src/views/AdminView.vue` | frontend | Year switch, list, side panel, bulk export button |
| `frontend/src/components/SearchBar.vue` | frontend | Sticky input + result list |
| `frontend/src/components/ProgressBlock.vue` | frontend | Year counters + progress bar |
| `frontend/src/components/OrderDetailPanel.vue` | frontend | Read-only meta table + three upload cards + submit button |
| `frontend/src/components/UploadCard.vue` | frontend | Per-type upload card (staged + server-side views, picker, delete) |
| `frontend/src/components/Toast.vue` | frontend | Transient success/error messages |
| `frontend/src/stores/collection.ts` (Pinia) | frontend | Staged photos, current order, year progress cache |
| `frontend/src/stores/admin.ts` (Pinia) | frontend | Admin list, detail, auth cookie state |
| `frontend/src/lib/imagePipeline.ts` | frontend | HEIC detect + decode + resize + JPEG encode |
| `frontend/src/lib/api.ts` | frontend | Thin fetch wrapper; shared error handling |
| `frontend/src/lib/filename.ts` | frontend | Client-side preview of sanitized filename (optional helper) |
| `backend/cmd/server/main.go` | backend | Binary entrypoint; config load, DB open, embed serve, route wire-up |
| `backend/internal/config` | backend | YAML loader + defaults + CLI flags (`import-csv`, `--reimport`) |
| `backend/internal/ingest` | backend | CSV → SQLite importer; idempotent upserts |
| `backend/internal/db` | backend | Schema migration + sqlx helpers |
| `backend/internal/orders` | backend | Query APIs (search, progress, detail) |
| `backend/internal/uploads` | backend | Multipart handler, filename resolver, per-type seq calc, file write, DB insert |
| `backend/internal/pdfmerge` | backend | JPEG → PDF pages, ordered 合同→发票, atomic write |
| `backend/internal/admin` | backend | Admin auth middleware, list/detail/delete/reset/zip endpoints |
| `backend/internal/storage` | backend | Filesystem helpers with `filepath.Clean` guard |
| `backend/internal/httpapi` | backend | Route registration, error → JSON mapping, embed static handler |
| `backend/tests/integration` | backend | httptest-based end-to-end tests |
| `frontend/tests/unit` | frontend | Vitest tests for imagePipeline, stores, filename sanitizer |
| Filesystem layout under `./data/...` | backend | Owned by backend (but shape is contract for both sides) |

No `fullstack` components — the boundary is a REST API, so every component
lands cleanly on one side.

## 4. Filesystem Layout

```
repo-root/
├── 21-25订单.csv
├── config.yaml
├── frontend/
│   ├── src/...
│   ├── tests/unit/...
│   ├── package.json
│   ├── tsconfig.json
│   └── vite.config.ts
├── backend/
│   ├── cmd/server/main.go
│   ├── internal/{config,db,ingest,orders,uploads,pdfmerge,admin,storage,httpapi}/
│   ├── tests/integration/...
│   ├── dist/                       # frontend build output, //go:embed all:dist
│   ├── go.mod
│   └── go.sum
└── data/                           # git-ignored
    ├── app.db
    └── uploads/
        └── {year}/
            └── {单号}/
                ├── {单号}-{客户清洗}-合同-01.jpg
                ├── {单号}-{客户清洗}-合同-02.jpg
                ├── {单号}-{客户清洗}-发票-01.jpg
                ├── {单号}-{客户清洗}-发货单-01.jpg
                └── {单号}-{客户清洗}-合同与发票.pdf
```

During `go build`, Vite's `frontend/dist/` is copied to `backend/dist/` by a
`make` target (or equivalent script) before `go build`, and the server embeds
it.

## 5. Database Schema

```sql
-- Canonical per-line-item data from CSV (for the detail table display)
CREATE TABLE order_lines (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    year           INTEGER NOT NULL,            -- 2021..2025 (stripped Y)
    order_no       TEXT    NOT NULL,            -- 单据编号 (trailing '.' stripped)
    order_date     TEXT    NOT NULL,            -- CSV 日期 as YYYY/M/D
    customer       TEXT    NOT NULL,            -- 购货单位
    product        TEXT    NOT NULL,            -- 产品名称
    quantity       REAL    NOT NULL,            -- 数量
    amount         REAL    NOT NULL,            -- 金额
    total_with_tax REAL    NOT NULL,            -- 价税合计
    tax_rate       REAL    NOT NULL,            -- 税率(%)
    invoice_no     TEXT    NOT NULL,            -- 发票号
    UNIQUE(year, order_no, product, invoice_no) -- idempotent re-import
);
CREATE INDEX idx_order_lines_year_order ON order_lines(year, order_no);
CREATE INDEX idx_order_lines_order_like ON order_lines(order_no);

-- One row per (year, order_no) with the chosen sanitized customer name.
-- Populated during ingest from the first line of each order.
CREATE TABLE orders (
    year            INTEGER NOT NULL,
    order_no        TEXT    NOT NULL,
    customer        TEXT    NOT NULL,   -- original 购货单位 (first row)
    customer_clean  TEXT    NOT NULL,   -- sanitized, used in filenames
    PRIMARY KEY (year, order_no)
);

-- Each uploaded photo. 'merged.pdf' itself is NOT stored here — it is
-- derivable from the rows and regenerated on every submit.
CREATE TABLE uploads (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    year        INTEGER NOT NULL,
    order_no    TEXT    NOT NULL,
    kind        TEXT    NOT NULL CHECK (kind IN ('合同','发票','发货单')),
    seq         INTEGER NOT NULL,
    filename    TEXT    NOT NULL,          -- the on-disk name
    byte_size   INTEGER NOT NULL,
    sha256      TEXT    NOT NULL,
    uploaded_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (year, order_no, kind, seq),
    FOREIGN KEY (year, order_no) REFERENCES orders(year, order_no)
);
CREATE INDEX idx_uploads_year_order ON uploads(year, order_no);
CREATE INDEX idx_uploads_year_kind ON uploads(year, kind);
```

Migrations are applied on startup with a simple `PRAGMA user_version`-gated
SQL sequence inside `internal/db`.

## 6. API Contract

All endpoints return JSON (`application/json; charset=utf-8`) except file
downloads. Error shape is `{"error": {"code": "STR", "message": "..."}}`.
`{year}` is in the path (2021..2025); server rejects other values with 404.

### 6.1 Collection (public, per-year scoped)

| Method | Path | Purpose |
|-------|------|---------|
| `GET`  | `/api/y/:year/progress` | `{ total, uploaded, percent }` for that year |
| `GET`  | `/api/y/:year/search?q=...&limit=20` | Up to 20 matching orders |
| `GET`  | `/api/y/:year/orders/:orderNo` | Full detail: line items + current uploads |
| `POST` | `/api/y/:year/orders/:orderNo/uploads` | Multipart: fields `contract[]`, `invoice[]`, `delivery[]` |

Search response item:
```json
{
  "orderNo": "RX2101-22926",
  "customer": "哈尔滨金诺食品有限公司",
  "uploaded": true,
  "counts": { "合同": 2, "发票": 1, "发货单": 0 }
}
```

Detail response:
```json
{
  "orderNo": "RX2101-22926",
  "year": 2021,
  "customer": "哈尔滨金诺食品有限公司",
  "lines": [
    { "date": "2021/1/4", "product": "满特起酥油（FM）", "quantity": 1000,
      "totalWithTax": 126000.0, "invoiceNo": "2021/50122444" }
  ],
  "uploads": {
    "合同":   [ { "id": 12, "filename": "...", "url": "/files/2021/RX2101-22926/..." } ],
    "发票":   [],
    "发货单": []
  }
}
```

Submit response:
```json
{
  "counts": { "合同": 3, "发票": 1, "发货单": 0 },
  "progress": { "total": 5234, "uploaded": 1, "percent": 0.0191 }
}
```

### 6.2 File serving
| Method | Path | Purpose |
|-------|------|---------|
| `GET`  | `/files/:year/:orderNo/:filename` | Serves an image or merged PDF from `./data/uploads/...`; validates year, orderNo, filename exist in DB before streaming; sets `Cache-Control: private, no-store`. |

### 6.3 Admin (password-gated)

Middleware: require `admin_session` cookie. If absent or invalid, respond 401.

| Method | Path | Purpose |
|-------|------|---------|
| `POST` | `/api/admin/login`   | `{password}` → sets `admin_session` cookie, 12 h TTL |
| `POST` | `/api/admin/logout`  | Clears cookie |
| `GET`  | `/api/admin/years`   | `[{year:2021,total,uploaded}, ...]` |
| `GET`  | `/api/admin/:year/orders?page=1&size=50&onlyUploaded=true` | Paginated list |
| `GET`  | `/api/admin/:year/orders/:orderNo` | Same as collection detail |
| `GET`  | `/api/admin/:year/orders/:orderNo/merged.pdf` | Downloads merged PDF |
| `GET`  | `/api/admin/:year/orders/:orderNo/bundle.zip` | zip of all originals + merged PDF |
| `DELETE` | `/api/admin/:year/orders/:orderNo/uploads/:id` | Remove single photo, renumber, regen PDF |
| `DELETE` | `/api/admin/:year/orders/:orderNo` | Reset: purge all photos + DB rows for this order |
| `GET`  | `/api/admin/:year/export.zip` | Streamed year bundle |

Sessions are stored server-side in-memory (simple map keyed by random token).
No DB table is required because there is only one admin user and restart wipes
the session — the admin re-logs in. This keeps the surface tiny.

## 7. Frontend Page Designs (mobile)

### `/y{year}` (single page, no nav bar)

```
┌─────────────────────────────┐
│  2021 年单据收集             │
│  本年共 5234 · 已传 128      │
│  [▓░░░░░░░░░░] 2.4 %        │
├─────────────────────────────┤
│  🔍 [ RX2101        ]       │  sticky
├─────────────────────────────┤
│  RX2101-22926 ✓ 2/1/0       │
│  哈尔滨金诺食品有限公司       │
│  --------------------------  │
│  RX2101-22927 未上传         │
│  海通（郫县）                │
│  ...                         │
├─────────────────────────────┤
│  [Order Detail Panel — opens│
│   when a row is tapped]     │
└─────────────────────────────┘
```

Detail panel layout (scrolls under the sticky header):

```
单号 RX2101-22926            进度 ✓
─────────────────────────────
客户  哈尔滨金诺食品有限公司
┌─ 明细表 ────────────────────┐
│ 产品 · 数量 · 价税合计 · 发票号 │
│ 满特起酥油 · 1000 · 126000 · 2021/501.. │
└────────────────────────────┘

【合同图片拍照上传】
 [已传1][已传2]  [+]          ← already on server render as gray
【发票拍照上传】
 [+]
【发货单拍照上传】
 [+]

         [ 提交 ]             ← sticky bottom
```

### `/admin`

- Year tab bar (2021 | 2022 | 2023 | 2024 | 2025).
- Table: 单据编号 | 客户 | 已上传 | 合/发/送 | 最后上传 | [查看] [重置].
- Side panel on row click — thumbnails + download buttons.
- Top right: `[导出 {year}.zip]`.

## 8. Image Pipeline (frontend, `lib/imagePipeline.ts`)

```
File (from <input>)
   │
   ▼
if size > 20 MB  → reject (client-side "too big")
   │
   ▼
if mime == image/heic|image/heif
   → await heic2any({blob, toType: 'image/jpeg', quality: 0.9})
   │
   ▼
decode via createImageBitmap
   │
   ▼
OffscreenCanvas (fallback to canvas) resize so max(w,h) <= 2000
   │
   ▼
canvas.convertToBlob({type: 'image/jpeg', quality: 0.85})
   │
   ▼
if result size > 10 MB → reject (extremely rare after resize)
```

The returned `Blob` is what gets appended to the FormData. The staged photo in
Pinia retains `{blob, previewUrl: URL.createObjectURL(blob), origSize, outSize}`.

## 9. Backend Upload Handler Flow (`internal/uploads`)

1. `POST /api/y/:year/orders/:orderNo/uploads` enters Gin handler.
2. Parse multipart with `c.Request.ParseMultipartForm(64<<20)` (64 MB total
   memory limit; bigger spills to temp files handled by the stdlib).
3. Validate: year in [2021,2025]; orderNo exists in `orders` table; each file's
   sniffed content type starts with `image/`.
4. For each of the three field arrays (`contract`, `invoice`, `delivery`):
   - Begin a transaction for DB inserts.
   - Query `MAX(seq)` for `(year, order_no, kind)`; start at max+1.
   - For each file: compute target filename
     `{orderNo}-{customer_clean}-{kind}-{seq:02}.jpg`; write via temp file +
     `os.Rename` to the order's directory; insert `uploads` row with sha256.
5. Commit DB transaction.
6. Regenerate merged PDF: read all 合同 rows (ordered by seq), then all 发票
   rows, feed JPEG bytes through `pdfmerge.Build(...)` → write to temp → rename
   to `{orderNo}-{customer_clean}-合同与发票.pdf`.
7. Compute new per-type counts and year progress; respond.

Concurrency: one `sync.Mutex` keyed by `year/orderNo` (using a `sync.Map` of
mutexes) so two concurrent submits to the same order serialize cleanly. Cross-
order traffic is unaffected.

## 10. CSV Ingest (`internal/ingest`)

- Called on startup when `order_lines` is empty, or when `./server
  import-csv --reimport` is run.
- Read row-by-row:
  - Strip the leading `Y` from `年` (CSV contains `Y2021`).
  - Strip trailing `.` and whitespace from `单据编号`.
  - `INSERT OR IGNORE` into `order_lines` (using the 4-column UNIQUE key above).
  - `INSERT OR IGNORE` into `orders (year, order_no, customer, customer_clean)`
    using the first-seen customer for that order.
- Report `{inserted, skipped}` in the log. Idempotent.

Filename sanitization (`customer_clean`):
```go
var illegal = regexp.MustCompile(`[\/\\:*?"<>|\s]+`)
customerClean := strings.Trim(illegal.ReplaceAllString(name, "_"), "_")
if customerClean == "" { customerClean = "未知客户" }
```

## 11. PDF Merge (`internal/pdfmerge`)

- A4 portrait, 10 mm margins, white background.
- For each JPEG page:
  - Decode once to get pixel dimensions.
  - Compute `scale = min((pageW - 2*margin)/imgW, (pageH - 2*margin)/imgH)`.
  - Center horizontally and vertically.
  - Embed as a registered JPEG (`pdf.RegisterImageOptionsReader`).
- Output buffered; atomic temp-file write; final `os.Rename`.

Large-image guardrail: before embedding, if an image's pixel dimension exceeds
`8000 px`, re-encode to a 2000-px JPEG in memory (defensive — frontend should
already have compressed).

## 12. Zip Export (`internal/admin`)

`GET /api/admin/:year/export.zip` streams:
- Walks `./data/uploads/{year}/` in lexical order.
- For each order dir, emits `orderNo/{filename}` entries (originals + merged
  PDF) into `archive/zip.Writer` bound to the response body.
- Sets `Content-Disposition: attachment; filename="{year}-完整资料.zip"`.
- Never holds all bytes in memory; streamed directly.

Per-order `bundle.zip` uses the same logic, scoped to one dir.

## 13. Configuration

`config.yaml` (same path as the binary by default, overridable with `-config`):

```yaml
listen: "0.0.0.0:8080"
csv_path: "./21-25订单.csv"
data_dir: "./data"
admin_password: "CHANGE-ME"
session_ttl_hours: 12
image:
  max_per_card: 9
  pdf_order: ["合同", "发票"]
```

Environment variables override config keys (e.g. `APP_ADMIN_PASSWORD`).

## 14. Security Notes

- Admin password compared with `crypto/subtle.ConstantTimeCompare`.
- `/files/...` handler builds path via `filepath.Join(cfg.DataDir,
  "uploads", year, orderNo, filename)` then `filepath.Clean` and asserts the
  result still has the `data_dir` prefix. Any mismatch → 404.
- `Content-Type` on upload is sniffed by `http.DetectContentType`; extensions
  in Content-Disposition are ignored.
- Admin session token is a 32-byte `crypto/rand`-generated string.
- No CORS headers set; frontend served from same origin.
- Rate limit on `/api/admin/login`: max 5 attempts / 5 min per IP (simple
  in-memory counter).

## 15. Observability

- `log/slog` JSON logger to stdout.
- Per-request log line: method, path, status, duration, size.
- Submit handler logs: year, orderNo, per-type counts received, new totals.

## 16. Testing Strategy

- **Backend unit** (`backend/internal/**/*_test.go`):
  - `ingest`: CSV → DB idempotency.
  - `uploads`: filename/seq calculation, concurrent submits to same order.
  - `pdfmerge`: given N JPEG bytes, output PDF has N pages in correct order.
  - `storage`: path-traversal guard rejects `..` and absolute paths.
- **Backend integration** (`backend/tests/integration/*`):
  - Full `/api/y/2021/orders/xxx/uploads` round trip with `httptest` server.
  - Admin login/logout + zip export.
- **Frontend unit** (`frontend/tests/unit/*` via Vitest + JSDOM):
  - `imagePipeline`: size-cap, HEIC branch (mocked), canvas resize math.
  - `lib/filename`: sanitizer matches backend.
  - Pinia stores: stage-then-submit sequence, error keeps staged.
- **E2E** is out of scope for v1 (manual spot checks per 01§7).

## 17. Delivery Sequence

1. `backend/internal/db` + migrations + skeleton Gin server with 404 only.
2. `backend/internal/ingest` + `./server import-csv` CLI. Verify DB populated.
3. `backend/internal/orders` search + progress + detail endpoints. Verify with curl.
4. Frontend bootstrap: Vite + router + `/y2021` page hitting step-3 endpoints.
5. `backend/internal/uploads` + `/api/y/:year/orders/:orderNo/uploads`.
6. `backend/internal/pdfmerge` + invoke it from upload handler.
7. Frontend `UploadCard` + `imagePipeline` + submit flow; verify full round-trip.
8. `backend/internal/admin` endpoints + session middleware.
9. Frontend `/admin/*` views.
10. `internal/admin` zip export (both per-order and per-year).
11. Tests and hardening.

## 18. Risks & Mitigations

| Risk | Mitigation |
|------|-----------|
| 购货单位 with rare characters breaks filename | Sanitizer + fallback `未知客户`; unit-tested |
| Admin forgets password → lockout | Password in `config.yaml`; owner can edit and restart |
| User taps 提交 twice on bad Wi-Fi | Button becomes a spinner state; 2nd tap ignored; submit is append-only so a retry after success is harmless beyond duplicated photos — documented UX caveat |
| HEIC conversion fails on very old devices | imagePipeline returns a typed error; UI shows "此图片无法处理，请用相机重拍" |
| Disk fills | Admin can delete reset orders; documented ops note to monitor `./data/` |
| Port 8080 busy | `listen` in config file; fallback documented |
