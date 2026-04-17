# Architecture — Product Order Documentation Collection Form

Companion to `01-requirements.md`. Single binary, LAN deployment.
v2 — revised after Codex adversarial review.

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
│  ├── GET  /, /y2021..5, /admin, /admin/login      │    │
│  │   (SPA history fallback; server never 302s,   │    │
│  │    page-level redirect is done by the SPA      │    │
│  │    guard after a 401 from /api/admin/*)        │    │
│  ├── /api/y/:year/*       (collection endpoints)  │    │
│  ├── /api/admin/*         (protected endpoints)   │    │
│  └── /files/y/:year/:orderNo/:filename            │    │
│                                                   ▼    │
│  Services: ingest, order query, upload orchestrator,   │
│            pdf merge, zip export, admin                │
│                                                        │
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
| Routing | Vue Router 4 | 5 `/y{year}` routes + `/admin/*`; admin route has a `beforeEnter` guard that calls `GET /api/admin/ping` and on 401 redirects to `/admin/login` (satisfies FR-ADMIN-AUTH page-level behavior) |
| State | Pinia | 3 stores: `collection`, `admin`, `ui` |
| HTTP | native `fetch` with a tiny `api.ts` wrapper | No axios |
| HEIC | `heic2any` (lazy-imported only when MIME is heic) | keeps main bundle light |
| Image resize | Canvas 2D + `createImageBitmap` → `canvas.toBlob('image/jpeg', 0.85)` | No third-party |
| Toast | Tiny in-house component | 2.5 s auto-dismiss; `navigator.vibrate(50)` on success |
| A11y & layout | `rem`-based sizing, min 44 × 44 px targets, safe-area inset padding, `position: sticky` for search & submit on a `100dvh` layout | satisfies NFR-UX-1/2/3/4 |
| PWA | **No** (explicit non-goal) | |
| Styles | Plain CSS + CSS variables + a `mobile.css` breakpoint layer | No Tailwind |

### Backend
| Concern | Choice | Notes |
|--------|--------|-------|
| Runtime | Go 1.22+ | |
| Web | `github.com/gin-gonic/gin` | |
| DB | `modernc.org/sqlite` (pure Go) | WAL mode, `_busy_timeout=5000` |
| SQL helper | `github.com/jmoiron/sqlx` | struct-scan convenience |
| CSV ingest | `encoding/csv` stdlib | Streaming |
| PDF | `github.com/jung-kurt/gofpdf/v2` | JPEG pages, A4 portrait |
| Zip export | `archive/zip` stdlib (streaming) | |
| File ops | `os`, `io`, `path/filepath`, `mime/multipart` (streaming reader) | `filepath.Clean` path-traversal guard |
| Config | `gopkg.in/yaml.v3` | single file `config.yaml` |
| Logging | `log/slog` stdlib | |
| Tests | `testing` + `net/http/httptest` | |

### Build & Deploy
- Frontend: `pnpm build` → `frontend/dist/` (copied to `backend/dist/` and
  embedded via `//go:embed all:dist`).
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
| `frontend/src/App.vue` + `router.ts` | frontend | Shell + 7 routes (`/y{year}` ×5 + `/admin/login` + `/admin`); admin guard |
| `frontend/src/views/CollectionView.vue` | frontend | `/y{year}` mobile collection page (progress block, search, detail) |
| `frontend/src/views/AdminLoginView.vue` | frontend | Password prompt for admin |
| `frontend/src/views/AdminView.vue` | frontend | Year switch, list, side panel, bulk export button |
| `frontend/src/components/SearchBar.vue` | frontend | Sticky input + result list |
| `frontend/src/components/ProgressBlock.vue` | frontend | Year counters + progress bar |
| `frontend/src/components/OrderDetailPanel.vue` | frontend | Read-only meta table + three upload cards + submit button |
| `frontend/src/components/UploadCard.vue` | frontend | Per-type upload card (staged + server-side views, picker, delete) |
| `frontend/src/components/Toast.vue` | frontend | Transient success/error messages |
| `frontend/src/stores/collection.ts` (Pinia) | frontend | Staged photos, current order, year progress cache |
| `frontend/src/stores/admin.ts` (Pinia) | frontend | Admin list, detail, auth cookie state |
| `frontend/src/stores/ui.ts` (Pinia) | frontend | Toast queue, global loading state |
| `frontend/src/lib/imagePipeline.ts` | frontend | HEIC detect + decode + resize + JPEG encode |
| `frontend/src/lib/api.ts` | frontend | Thin fetch wrapper; admin 401 handler |
| `frontend/src/lib/filename.ts` | frontend | Client-side preview of sanitized filename |
| `backend/cmd/server/main.go` | backend | Binary entrypoint; config load, DB open, embed serve, route wire-up, CLI subcommands |
| `backend/internal/config` | backend | YAML loader + defaults + env overrides + CLI flags (`import-csv`, `--reimport`) |
| `backend/internal/ingest` | backend | CSV → SQLite importer; idempotent upserts; soft-delete bookkeeping on re-import |
| `backend/internal/db` | backend | Schema migration + sqlx helpers |
| `backend/internal/orders` | backend | Query APIs (search, progress, detail) |
| `backend/internal/uploads` | backend | Multipart streaming handler, filename resolver, per-type seq calc, atomic submit orchestrator |
| `backend/internal/pdfmerge` | backend | JPEG → PDF pages, ordered 合同→发票, atomic write |
| `backend/internal/admin` | backend | Admin session middleware, list/detail/delete/reset/zip endpoints |
| `backend/internal/storage` | backend | Filesystem helpers with `filepath.Clean` guard; per-order mutex map |
| `backend/internal/httpapi` | backend | Route registration, error → JSON mapping, embed static handler, SPA history fallback |
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

## 5. Database Schema

```sql
-- Canonical per-line-item data from CSV (for the detail table display).
-- Idempotency uses a full-row content hash to prevent false de-dup of
-- legitimately repeated lines (same product, same invoice, different row).
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
    source_hash    TEXT    NOT NULL UNIQUE,     -- sha256 of normalised raw CSV row
    source_line    INTEGER NOT NULL             -- 1-based CSV line (stable per-row id)
);
CREATE INDEX idx_order_lines_year_order ON order_lines(year, order_no);
CREATE INDEX idx_order_lines_order_like ON order_lines(order_no);

-- One row per (year, order_no). `csv_present` supports FR-EDGE-CSVREFRESH:
-- on every re-import we first set csv_present=0 for all orders, then set it
-- back to 1 for every order we re-encounter. Orders that keep csv_present=0
-- but have rows in `uploads` are surfaced in admin as "CSV 已移除".
CREATE TABLE orders (
    year            INTEGER NOT NULL,
    order_no        TEXT    NOT NULL,
    customer        TEXT    NOT NULL,   -- original 购货单位 (first row)
    customer_clean  TEXT    NOT NULL,   -- sanitized, used in filenames
    csv_present     INTEGER NOT NULL DEFAULT 1,  -- 1 if still in most recent CSV
    first_seen_at   TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (year, order_no)
);
CREATE INDEX idx_orders_csv_present ON orders(csv_present);

-- Each uploaded photo. Merged PDF is NOT in this table — it is derivable
-- and is located via a deterministic filename probe under the order's dir.
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
`{year}` is in the path; server rejects non-[2021..2025] with 404
`{"error":{"code":"YEAR_NOT_FOUND","message":"..."}}`.

### Canonical error codes (HTTP status → code)
| Status | `code` | Cause |
|-------|--------|-------|
| 400 | `BAD_REQUEST`              | malformed query / path / form |
| 400 | `ORDER_HAS_NO_STAGED_FILES`| submit with zero files |
| 401 | `UNAUTHENTICATED`          | missing/invalid admin session |
| 404 | `YEAR_NOT_FOUND`           | year outside 2021..2025 |
| 404 | `ORDER_NOT_FOUND`          | order_no absent from `orders` (or filtered by year) |
| 404 | `FILE_NOT_FOUND`           | `/files/...` with unknown filename |
| 409 | `UPLOAD_CAP_EXCEEDED`      | >9 files of one kind in one submit |
| 413 | `REQUEST_TOO_LARGE`        | total body > 60 MB (server cap) |
| 415 | `UNSUPPORTED_MEDIA_TYPE`   | any file's sniffed type is not `image/jpeg`/`image/png`/`image/webp` |
| 423 | `ORDER_LOCKED`             | another admin/upload op holds the per-order lock > 30 s |
Note: `UPLOAD_CAP_EXCEEDED` is always **409** and `ORDER_LOCKED` is always
**423**. They are never interchanged.
| 429 | `RATE_LIMITED`             | admin login > 5 attempts / 5 min / IP, or global year-export concurrency cap |
| 500 | `INTERNAL`                 | catch-all |
| 503 | `SERVER_BUSY`              | global upload/PDF/bundle admission-control semaphore full for > 5 s |

### 6.1 Collection (public, per-year scoped)

| Method | Path | Purpose |
|-------|------|---------|
| `GET`  | `/api/y/:year/progress` | `{ total, uploaded, percent }` for that year |
| `GET`  | `/api/y/:year/search?q=...&limit=20` | ≤20 matching orders for this year |
| `GET`  | `/api/y/:year/orders/:orderNo` | Full detail for order (line items + current uploads) |
| `POST` | `/api/y/:year/orders/:orderNo/uploads` | Multipart **streaming** upload |

**Search contract (spells out the fuzzy semantics):**
- Query param `q`: server rejects `len(q) < 2` with `BAD_REQUEST`.
- Matching: `WHERE year = ? AND order_no LIKE '%' || ? || '%' COLLATE NOCASE`
  against `DISTINCT order_no`; case-insensitive substring.
- Ordering: join once with the earliest `order_date` per `order_no`; sort
  ascending by `order_date`, ties by `order_no`.
- `limit` default 20, hard-capped at 50.
- Each response item is **derived**, per row:
  ```json
  {
    "orderNo": "RX2101-22926",
    "customer": "哈尔滨金诺食品有限公司",
    "uploaded": true,              // at least one row in `uploads` for this order
    "counts": { "合同": 2, "发票": 1, "发货单": 0 },
    "csvPresent": true             // false when FR-EDGE-CSVREFRESH triggered
  }
  ```

**Detail contract:**
```json
{
  "orderNo": "RX2101-22926",
  "year": 2021,
  "customer": "哈尔滨金诺食品有限公司",
  "csvPresent": true,
  "lines": [
    {
      "orderNo": "RX2101-22926",
      "customer": "哈尔滨金诺食品有限公司",
      "date": "2021/1/4",
      "product": "满特起酥油（FM）",
      "quantity": 1000,
      "amount": 114545.45,
      "totalWithTax": 126000.0,
      "taxRate": 10,
      "invoiceNo": "2021/50122444"
    }
    // one entry per row in order_lines for this (year, orderNo)
  ],
  "uploads": {
    "合同":   [ { "id": 12, "seq": 1, "filename": "...", "url": "/files/y/2021/RX2101-22926/...", "size": 384219 } ],
    "发票":   [],
    "发货单": []
  }
}
```
Every `line` carries the six columns the UI shows. Extra columns (`amount`,
`taxRate`) are included so admin can reuse the same endpoint.

**Submit contract (`POST /api/y/:year/orders/:orderNo/uploads`):**
- Content-Type: `multipart/form-data`.
- Field names (exactly, arrays): `contract[]`, `invoice[]`, `delivery[]`.
  The server accepts both the HTML-historical `contract` (without `[]`) and
  `contract[]` — normalized server-side. Same for invoice / delivery.
- Total body size ≤ 60 MB (enforced via `http.MaxBytesReader`) → 413.
- Each file's sniffed content type must start with `image/` and be one of
  `image/jpeg`, `image/png`, `image/webp` → 415 otherwise.
- Server does **not** call `ParseMultipartForm` eagerly. It uses
  `req.MultipartReader()` and writes each `*Part` to a `temp` file under
  `./data/uploads/.incoming/{uuid}/` via `io.Copy` so bytes never buffer in
  memory (NFR-PERF-2).
- Per-kind stage cap: after iterating parts, if any bucket has > 9 files →
  **409 `UPLOAD_CAP_EXCEEDED`** after rollback.
- Response on success (HTTP 200):
  ```json
  {
    "counts": { "合同": 3, "发票": 1, "发货单": 0 },
    "progress": { "total": 5234, "uploaded": 1, "percent": 0.0191 },
    "mergedPdfStale": false
  }
  ```
  If the DB portion succeeded but the PDF merge step failed, the server still
  returns **HTTP 200** with `"mergedPdfStale": true`. Rationale (matches
  FR-SUBMIT-4/-5): the user's uploads are safely persisted; the frontend
  must clear staged photos as for any success, but also surface a subtle
  banner ("合并 PDF 暂未生成，稍后管理员可重建") and refetch server-side
  photos. This prevents the duplicate-append hazard that would otherwise
  occur if we returned 500 while the rows were already committed.
  The admin endpoint `POST /api/admin/:year/orders/:orderNo/rebuild-pdf`
  completes the PDF when possible.

### 6.2 File serving
| Method | Path | Purpose |
|-------|------|---------|
| `GET`  | `/files/y/:year/:orderNo/:filename` | Serve a stored file from `./data/uploads/...` |

Validation rules (prevent the earlier self-contradiction about merged PDF):
- `:year` ∈ 2021..2025 else 404.
- `:orderNo` must exist in `orders` table else 404.
- `:filename` must be either
  (a) exactly the generated merged PDF name for that order
      (`{orderNo}-{customer_clean}-合同与发票.pdf`), or
  (b) match a row in `uploads` (same `filename` column) for that
      `(year, order_no)`.
  Anything else → 404.
- Built path `filepath.Clean(filepath.Join(dataDir, "uploads", yyyy, orderNo,
  filename))` must still have `filepath.Join(dataDir, "uploads")` prefix.
- `Cache-Control: private, no-store`.

### 6.3 Admin (password-gated)

| Method | Path | Purpose |
|-------|------|---------|
| `GET`  | `/api/admin/ping`    | 200 `{"ok":true,"csrfToken":"<hex>"}` if session valid; 401 otherwise. The returned CSRF token is HMAC-derived from the session token and must be echoed back in `X-Admin-Csrf` on every state-changing admin request. SPA router guard calls this before rendering `/admin`. |
| `POST` | `/api/admin/login`   | `{password}` → sets `admin_session` cookie, 12 h TTL, HttpOnly, SameSite=Lax |
| `POST` | `/api/admin/logout`  | Clears cookie |
| `GET`  | `/api/admin/years`   | `[{year, total, uploaded, csvRemoved}, ...]` |
| `GET`  | `/api/admin/:year/orders?page=1&size=50&onlyUploaded=true&onlyCsvRemoved=true` | Paginated list; see shape below |
| `GET`  | `/api/admin/:year/orders/:orderNo` | Same shape as collection detail |
| `GET`  | `/api/admin/:year/orders/:orderNo/merged.pdf` | Downloads merged PDF (serves from disk) |
| `GET`  | `/api/admin/:year/orders/:orderNo/bundle.zip` | **All original photos for this order** (every 合同 / 发票 / 发货单 JPEG) plus the merged PDF — matches FR-ADMIN-DETAIL "下载所有原图 zip" |
| `DELETE` | `/api/admin/:year/orders/:orderNo/uploads/:id` | Delete one photo, renumber, regen PDF (see §9.3) |
| `DELETE` | `/api/admin/:year/orders/:orderNo` | Reset: purge all photos + DB rows for this order (see §9.4) |
| `POST` | `/api/admin/:year/orders/:orderNo/rebuild-pdf` | Idempotent merged-PDF rebuild. 200 `{ "ok": true, "pages": <int> }` when rebuilt; 404 `ORDER_NOT_FOUND` if the order has no uploads. Requires session cookie and `X-Admin-Csrf` header. Used after a submit/delete returns `mergedPdfStale=true`, and available for routine remediation. |
| `GET`  | `/api/admin/:year/export.zip` | Streamed year bundle: **for each order, emits `{orderNo}/合同与发票.pdf` and any `{orderNo}/*发货单*.jpg` — never raw contract/invoice originals** (FR-ADMIN-BULK-EXPORT) |

**Admin list response shape:**
```json
{
  "page": 1,
  "size": 50,
  "total": 5234,
  "items": [
    {
      "orderNo": "RX2101-22926",
      "customer": "哈尔滨金诺食品有限公司",
      "uploaded": true,               // any upload row exists
      "counts": { "合同": 2, "发票": 1, "发货单": 0 },
      "lastUploadAt": "2026-04-16T09:12:45Z",   // nullable when uploaded=false
      "csvRemoved": false              // true when orders.csv_present = 0 but uploads exist
    }
  ]
}
```

Sessions: server-side in-memory (`sync.Map[token]expiry`) so restart wipes
them. Admin re-logs in. Cookie `admin_session=<32-byte random hex>`,
HttpOnly, SameSite=Lax, `Secure` if request is TLS.

## 7. Frontend Page Designs (mobile)

Unchanged from v1; details per §7 of the prior draft. UX commitments that
land at the architectural level:
- Layout uses `height: 100dvh`, safe-area inset padding.
- Sticky header (progress + search) and sticky submit button share the
  viewport; search card min-height 96 px, submit button bar 64 px, which
  fits a 667 px mobile viewport with detail scroll area ≥ 400 px.
- `UploadCard` `<input type="file" accept="image/*" capture>` triggers camera
  on iOS/Android.
- On successful submit, `ui` store triggers `navigator.vibrate?.(50)` and a
  2.5 s green toast.

## 8. Image Pipeline (frontend, `lib/imagePipeline.ts`)

```
File (from <input>)
   │
   ▼
size > 20 MB  → reject "single image exceeds 20 MB pre-compression limit"
   │
   ▼
mime ∈ {image/heic, image/heif} → heic2any({toType:'image/jpeg', quality:0.9})
   │
   ▼
createImageBitmap(blob)
   │
   ▼
OffscreenCanvas (canvas fallback) resize so max(w,h) ≤ 2000
   │
   ▼
convertToBlob({type:'image/jpeg', quality:0.85})
   │
   ▼
size > 10 MB → reject
```

Pinia staged photo shape: `{blob, previewUrl, origSize, outSize, kind}`.
Frontend enforces ≤ 9 per kind **before** submit; server re-enforces as the
source of truth.

## 9. Backend Upload & Admin Flows

### 9.1 Per-order locking (shared by upload, delete, reset, and zip export)
`internal/storage.OrderLock` exposes
`func Acquire(year int, orderNo string) (release func())`.
Implementation: `sync.Map[string, *sync.Mutex]`; `TryLock` with a 30 s
deadline; on timeout return 423 `ORDER_LOCKED`. Admin per-year export
acquires locks lazily per order as it walks the directory (never global).

### 9.2 Submit handler (one request = one atomic outcome)

```
1. Match route → resolve (year, orderNo); 404 if unknown.
2. Acquire per-order lock. Defer release.
3. mkdir -p  ./data/uploads/.incoming/{txid}/
4. mr := req.MultipartReader()
   for each part:
       - if field ∉ {contract,contract[],invoice,invoice[],delivery,delivery[]} → skip
       - read first 512 B into sniff buf, fail fast if not image/jpeg|png|webp
       - stream part body via io.Copy into .incoming/{txid}/{kind}-{n}.bin
         (using http.MaxBytesReader on req.Body before this loop)
   if ALL THREE buckets are empty → return 400 ORDER_HAS_NO_STAGED_FILES
     (a submit with files in only 1 or 2 kinds is valid per FR-DETAIL-5)
   if any bucket has > 9 files → return 409 UPLOAD_CAP_EXCEEDED
5. Snapshot the current merged PDF (if present) by renaming it to
   `.../合同与发票.pdf.bak-{txid}`. If none exists, note that.
6. BEGIN TRANSACTION
   for each (kind, fileList):   // kinds with 0 files are skipped
       maxSeq := SELECT COALESCE(MAX(seq),0) FROM uploads WHERE year=? AND order_no=? AND kind=?
       for i, src in fileList:
           seq := maxSeq + i + 1
           dstName := fmt.Sprintf("%s-%s-%s-%02d.jpg", orderNo, customerClean, kind, seq)
           dstPath := .../uploads/{year}/{orderNo}/{dstName}
           (if the part wasn't JPEG → re-encode to JPEG under .incoming)
           rename .incoming/.../{src} → dstPath.tmp
           fsync
           rename dstPath.tmp → dstPath
           INSERT INTO uploads(year, order_no, kind, seq, filename, byte_size, sha256) ...
   COMMIT DB first (source of truth now reflects new rows).
7. Regenerate merged PDF via pdfmerge.Build(合同rows → 发票rows) into
   `.../合同与发票.pdf.new-{txid}`. On success, rename atomically to final
   name. On success, unlink `.bak-{txid}`.
8. Compute new counts and year-level progress; respond 200.

Failure handling (ordered windows):
- Step 4 error (I/O / media type / caps):
    `.incoming/{txid}/` deferred rm -rf → respond with mapped code.
- Step 6 error (any file rename / DB error before COMMIT):
    ROLLBACK DB. Unlink any `dstPath` we successfully created in this call
    (we recorded them in an in-memory list) — existing server-side photos
    remain untouched because we only ever wrote to NEW seq numbers. Restore
    the PDF by renaming `.bak-{txid}` back to the canonical name. Respond 500.
- Step 7 error (PDF build or rename):
    DB is already committed, so new photo rows stay. Restore the PDF by
    renaming `.bak-{txid}` back to the canonical name so downloads remain
    consistent with the previous build; log the mismatch. Respond **HTTP 200**
    with `mergedPdfStale=true` per the submit contract — the frontend still
    clears staged photos, preventing duplicate re-submission on retry.
    The admin endpoint `POST /api/admin/:year/orders/:orderNo/rebuild-pdf`
    finishes the rebuild when invoked.
`.incoming/{txid}/` is always rm -rf'd on handler exit (defer). Any leftover
`.bak-{txid}` or `.new-{txid}` is cleaned at startup by a janitor that scans
`./data/uploads/*` for orphaned `.bak-*` / `.new-*` files.
```

This replaces the earlier per-kind-transaction design. One submit → one
transaction → one PDF rebuild → either everything lands or nothing does.

### 9.3 Admin delete single photo (`DELETE /.../uploads/:id`)

```
1. Acquire per-order lock.
2. SELECT kind, seq, filename for the id; 404 if missing.
3. Snapshot merged PDF → `.bak-{txid}` as in §9.2 step 5.
4. Move the deleted photo to `.../.trash/{year}-{orderNo}-{id}-{txid}.jpg`
   (atomic rename; filesystem state is now "deleted" but recoverable).
5. Build the renumbering plan:
   - SELECT remaining rows of (year, order_no, kind) ORDER BY seq.
   - For each remaining row at 0-based index i, desired seq = i+1,
     desired filename = {orderNo}-{customer_clean}-{kind}-{i+1:02}.jpg.
   - First pass: rename every on-disk file that needs renaming to a
     temporary name `.{old}.rename-{txid}` to avoid collisions.
   - Second pass: rename each `.rename-{txid}` file to its final name.
   Both passes emit inverse operations so step 7's rollback can undo them.
6. BEGIN TRANSACTION
     DELETE FROM uploads WHERE id = ?
     UPDATE uploads SET seq = ?, filename = ? WHERE id = ?   (per row)
   COMMIT.
7. Regenerate merged PDF; atomic rename; unlink `.bak-{txid}`.
8. Asynchronously unlink the photo from `.trash/`.
Failure handling:
- Step 4 fails → respond 500, no state changed.
- Step 5 fails → reverse any renames done so far using the captured inverse
  map; restore the .trash file to its original name; respond 500.
- Step 6 fails → same rollback as step 5 plus move the .trash file back.
- Step 7 fails → DB is already the truth; restore PDF from `.bak-{txid}`;
  return 500 with `merged_pdf_stale=true`; `rebuild-pdf` can recover.
```

### 9.4 Admin reset (`DELETE /.../orders/:orderNo`)
Under the per-order lock, with crash-safe ordering:
```
1. Rename ./data/uploads/{year}/{orderNo}/
         → ./data/uploads/.trash/{year}-{orderNo}-{txid}/   (atomic)
   If the dir does not exist, skip (DB may still have stale rows).
2. BEGIN TRANSACTION
       DELETE FROM uploads WHERE year=? AND order_no=?
   COMMIT
3. Asynchronously rm -rf ./data/uploads/.trash/{year}-{orderNo}-{txid}/.
   A startup janitor also purges any leftover .trash/ entries older than 1 h.
Failure handling:
- Step 1 fails → respond 500, DB untouched.
- Step 2 fails → rename the trashed dir back to the original path to restore
    disk state before responding 500.
- Step 3 failure is non-fatal (disk reclaim happens lazily).
```

### 9.5 Admin zip export
Acquires the per-order lock for the duration of that order's entries being
written. Writes only:
- `{orderNo}/{customer_clean}-合同与发票.pdf` (if present)
- `{orderNo}/*发货单-*.jpg`
No contract/invoice originals are included. Matches FR-ADMIN-BULK-EXPORT.

## 10. CSV Ingest (`internal/ingest`)

- Triggered on startup when `order_lines` is empty, or by `./server import-csv --reimport`.
- Algorithm:
  ```
  BEGIN
  UPDATE orders SET csv_present = 0   -- mark all as "not in this CSV yet"
  for each CSV row:
      normalize: strip BOM, trim spaces, strip trailing '.' from 单据编号,
                  drop 'Y' prefix from 年
      source_hash = sha256(normalized-row-tsv)
      INSERT OR IGNORE INTO order_lines(..., source_hash, source_line)
      INSERT INTO orders(year, order_no, customer, customer_clean, csv_present)
          VALUES (?, ?, ?, ?, 1)
          ON CONFLICT(year, order_no) DO UPDATE SET csv_present = 1
  COMMIT
  ```
- Orders whose `csv_present = 0` but that have rows in `uploads` surface as
  "CSV 已移除" in admin (`csvRemoved` filter). They keep their uploads.
- `source_hash` is the idempotency key — re-running the import against the
  same CSV is a no-op; a slightly different CSV row adds a new line (never
  silently merges).

Filename sanitization (`customer_clean`), used both in ingest and in
`lib/filename.ts`:
```go
// Illegal on Windows/macOS/Linux filename chars and whitespace.
var illegal = regexp.MustCompile(`[\\/:*?"<>|\s]+`)
customerClean := strings.Trim(illegal.ReplaceAllString(name, "_"), "_")
if customerClean == "" { customerClean = "未知客户" }
```

## 11. PDF Merge (`internal/pdfmerge`)

- A4 portrait, 10 mm margins, white background.
- For each JPEG page:
  - Decode once for pixel dimensions.
  - `scale = min((pageW - 2*margin)/imgW, (pageH - 2*margin)/imgH)`
  - Center horizontally and vertically.
  - Embed via `pdf.RegisterImageOptionsReader`.
- Output buffered; atomic temp-file write; final `os.Rename`.

Large-image guardrail: if pixel dimension > 8000, re-encode to 2000-px JPEG
in memory first.

## 12. Zip Export (`internal/admin`)

Per-year export `GET /api/admin/:year/export.zip`:
- Sets `Content-Disposition: attachment; filename="{year}-完整资料.zip"`.
- Uses `archive/zip.NewWriter(responseBody)` with no intermediate buffer.
- Walks `./data/uploads/{year}/` sorted by orderNo.
- For each order, acquires the per-order lock, then emits:
  - `{orderNo}/{customer_clean}-合同与发票.pdf` (if present)
  - Every 发货单 JPEG matching `*-发货单-*.jpg`
- Never emits raw 合同/发票 originals (spec-compliant).
- Per-order `bundle.zip` has **different contents** from the year export:
  it includes the merged PDF **and every original photo** of every kind
  (合同 / 发票 / 发货单), matching FR-ADMIN-DETAIL "下载所有原图 zip".
  It uses a different producer (`admin.BuildOrderBundle`) that emits all
  files under `./data/uploads/{year}/{orderNo}/`, excluding only the hidden
  `.bak-*` / `.new-*` / `.rename-*` temp files.

## 13. Configuration

```yaml
listen: "0.0.0.0:8080"
csv_path: "./21-25订单.csv"
data_dir: "./data"
admin_password: "CHANGE-ME"
session_ttl_hours: 12
limits:
  per_kind_max: 9                  # FR-IMG-3
  submit_body_max_mb: 60           # NFR-PERF-2 cap
  single_file_max_mb: 10           # FR-IMG-1
  single_file_decode_cap_mb: 20    # pre-HEIC-decode safety
  max_pixels: 50000000             # decompression-bomb guard (~50 MP)
concurrency:
  max_uploads: 4                   # ARCH-06 admission control
  max_pdf_rebuilds: 4
  max_year_exports: 1
  max_bundle_exports: 4
  max_image_decodes: 4
  acquire_timeout_seconds: 5
db:
  max_open_conns: 8                # ARCH-05
  max_idle_conns: 4
  conn_max_lifetime_minutes: 30
image:
  pdf_order: ["合同", "发票"]       # server uses this to build merged PDF
  accepted_mime: ["image/jpeg", "image/png", "image/webp"]
```

Environment variables override config keys (e.g. `APP_ADMIN_PASSWORD`,
`APP_LISTEN`).

## 14. Security Notes

### 14.1 Accepted risk — open LAN deployment
This service is intended for a **trusted internal LAN** with anonymous
operators. The following threats are **explicitly accepted** by the owner
and out of scope for the implementation:

| ID | Risk | Accepted-on | Rationale |
|----|------|-------------|-----------|
| ARCH-01 | `/api/y/:year/*` endpoints are unauthenticated; anyone on the LAN who knows the URL can read order data | 2026-04-16 | Collection operators must have a frictionless URL to share; the dataset is internal, not PII-grade |
| ARCH-02 | Unauthenticated `POST /api/y/:year/orders/:orderNo/uploads` lets any LAN peer append forged photos | 2026-04-16 | Operator flow must not require a login step; append-only model + admin review provides the audit loop |
| ARCH-03 | Admin login and session cookie travel over plain HTTP by default | 2026-04-16 | Only deployed inside the operator's LAN; TLS is the LAN operator's choice and not enforced by the application |

These three findings were flagged **Critical-Architectural** in Codex review
R1 (2026-04-16); the user reviewed and accepted them. No code should try to
work around them; e.g. do not add opt-in TLS, do not add per-year tokens.

### 14.2 Still-enforced application-level safeguards
- Admin password compared with `crypto/subtle.ConstantTimeCompare`.
- `/files/...` handler path assembly:
  ```go
  clean := filepath.Clean(filepath.Join(cfg.DataDir, "uploads", yyyy, orderNo, filename))
  if !strings.HasPrefix(clean, filepath.Clean(filepath.Join(cfg.DataDir, "uploads"))+string(os.PathSeparator)) {
      return 404
  }
  ```
- `Content-Type` on upload parts is sniffed with `http.DetectContentType`;
  the multipart header's claimed type is never trusted.
- Admin session token: 32 bytes from `crypto/rand`, hex-encoded.
- Admin `POST/DELETE` endpoints require **both** a valid session **and** a
  header `X-Admin-Csrf: <token>` returned from `/api/admin/ping`. The token
  is bound to the session token via HMAC; this closes the same-origin-only
  CSRF exposure.
- Rate limit on `/api/admin/login`: max 5 / 5 min / IP via
  `golang.org/x/time/rate` per-IP buckets (in-memory).
- No CORS headers set; frontend served from same origin.

## 14.3 HTTP server timeout SLA (ARCH-04)
`http.Server` is split by endpoint class via a per-route handler that wraps a
scoped `http.TimeoutHandler`. Base server values:

| Setting | Value | Rationale |
|---------|-------|-----------|
| `ReadHeaderTimeout` | `10 s`   | keeps slowloris handshake bounded |
| `ReadTimeout`       | **off** at the server level (0) | set per-route (see below) — the server-level value would cap the 60 MB upload stream |
| `WriteTimeout`      | **off** at the server level (0) | set per-route for streaming endpoints |
| `IdleTimeout`       | `60 s`   | closes keep-alive connections with no in-flight request |
| `MaxHeaderBytes`    | `1 MB`   | guard against header bombs |

Per-route deadlines (enforced via a `timeoutMiddleware(d time.Duration)`):
| Route pattern | Deadline | Notes |
|---------------|----------|-------|
| `GET  /api/**` (non-streaming JSON)        | `10 s`  | search / progress / detail |
| `POST /api/y/:year/orders/:orderNo/uploads`| `120 s` | covers 60 MB multipart on LAN + PDF rebuild |
| `GET  /files/**`                           | `120 s` | single file download |
| `GET  /api/admin/:year/export.zip`         | `15 min` | whole-year ZIP stream |
| `GET  /api/admin/.../bundle.zip`           | `120 s` | single-order ZIP |
| `POST /api/admin/:year/orders/:orderNo/rebuild-pdf` | `60 s` | |

Each timeout wraps a `context.WithTimeout` so the underlying DB / file ops
cancel cleanly; the per-order mutex is always released before the goroutine
returns thanks to `defer release()` inside the handler.

## 14.4 SQLite connection-pool & PRAGMA contract (ARCH-05)
Historically PRAGMA statements were issued once on an ad-hoc connection;
`database/sql` would then dial more connections without replaying them. Fix:

- Open DB through a custom `sql.Register`ed driver wrapper (or
  `sqlx.Open` + `Connector` in modernc's driver) whose `OpenConnector.Connect`
  issues the following on **every** connection before returning it:
  ```
  PRAGMA journal_mode=WAL;
  PRAGMA synchronous=NORMAL;
  PRAGMA foreign_keys=ON;
  PRAGMA busy_timeout=5000;
  PRAGMA temp_store=MEMORY;
  ```
- Pool sizing:
  - `SetMaxOpenConns(8)`   — serialise writes behind SQLite's single-writer
    contract; eight connections is enough for mostly-read workloads while
    bounding memory.
  - `SetMaxIdleConns(4)`
  - `SetConnMaxLifetime(30 min)` — recycle to drop stale fds.
- Writes still serialize inside SQLite; our per-order mutex prevents races
  before we even hit SQLite, so `busy_timeout=5000` is a belt-and-braces.

## 14.5 Global concurrency & admission control (ARCH-06)
Per-order locks only protect a single order's state. Server-wide resource
caps live in `internal/httpapi/limits`:

| Gate | Cap | Behavior on saturation |
|------|-----|------------------------|
| Concurrent uploads (`POST /api/y/.../uploads`) | `4` | 503 `SERVER_BUSY` |
| Concurrent PDF rebuilds (including the one triggered by upload) | `4` | handler queues up to the upload limit; extras → 503 |
| Concurrent year-export streams (`/api/admin/:year/export.zip`) | `1` | 429 `RATE_LIMITED` |
| Concurrent per-order bundle exports | `4` | 503 `SERVER_BUSY` |
| Concurrent image decode (inside upload/rebuild) | `4` | internal semaphore, no HTTP error; just serialises |

Implementation: weighted semaphore (`golang.org/x/sync/semaphore` or a small
channel-based one) acquired with a 5 s timeout; on timeout the handler
returns the indicated HTTP status.

Canonical error codes updated:
| Status | `code` | Cause |
|-------|--------|-------|
| 503 | `SERVER_BUSY`      | global upload/PDF/bundle semaphore full |
| 429 | `RATE_LIMITED`     | includes year-export cap (in addition to the existing admin login rate limit) |

## 14.6 Image byte/pixel enforcement (was C-02)
Config values `single_file_max_mb` and `single_file_decode_cap_mb` are now
**actually enforced** in `internal/uploads` and `internal/pdfmerge`:

- Each streamed part is bounded by a `http.MaxBytesReader`-style
  `io.LimitReader` sized to `single_file_max_mb` (default 10 MB). Exceeding
  the cap → 413 `REQUEST_TOO_LARGE`.
- Before any `image.Decode` call (for PNG/WebP path and for the optional
  backend JPEG fallback), the decoded file size is checked against
  `single_file_decode_cap_mb` (default 20 MB). Too big → 415
  `UNSUPPORTED_MEDIA_TYPE` with message "image exceeds server decode cap".
- Additionally, after reading dimensions via `image.DecodeConfig`, any
  image with `width * height > 50_000_000` pixels (approx 50 MP) is
  rejected to block decompression bombs.

## 14.7 PDF merge memory bound (was C-01)
`internal/pdfmerge.Build` streams the PDF output directly to disk via
`pdf.Output(w io.Writer)` against the `.new-{txid}` file handle; no full-PDF
`bytes.Buffer` is held. Each JPEG is read once via `pdf.RegisterImageOptionsReader`
and released before the next page. For images > 8000 px on any side, the
re-encode step uses `golang.org/x/image/draw.BiLinear` into a stack-allocated
`*image.RGBA` sized to the target (max 2000 px side); the original decoded
image is freed before the next page. Peak memory per rebuild is bounded to
one working image (max 2000 px * 2000 px * 4 bytes ≈ 16 MB) plus a few MB of
PDF writer state, regardless of page count.

## 14.8 CSV re-import semantics (was C-03)
CSV re-import is now a **full refresh** of `order_lines`:

```
BEGIN IMMEDIATE
DELETE FROM order_lines      -- drop all previous lines in one go
-- walk the CSV, INSERT every normalized row
UPDATE orders SET csv_present = 0
INSERT INTO orders(...) ON CONFLICT DO UPDATE SET csv_present = 1
COMMIT
```

`order_lines.source_hash` stays as a UNIQUE index strictly for crash
recovery inside one import (ignores an exact duplicate line in the CSV
itself, which would be an upstream bug). `orders` rows are kept — uploads
must outlive re-imports — but `csv_present=0` surfaces orders whose CSV
entry disappeared.

This replaces the earlier "INSERT OR IGNORE on source_hash" behavior, which
allowed stale `order_lines` rows to accumulate.

## 15. Observability

- `log/slog` JSON logger to stdout.
- Per-request log line: method, path, status, duration, bytes-in, bytes-out.
- Submit handler logs: year, orderNo, per-kind counts received, new totals,
  txid, duration of each phase (receive / rename / pdf-merge / commit).
- Admin-destructive actions (reset, delete) log full before/after state.

## 16. Testing Strategy

- **Backend unit** (`backend/internal/**/*_test.go`):
  - `ingest`: CSV → DB idempotency (same CSV twice = no new rows); `csv_present` flipping; source_hash behavior on legitimately duplicate rows.
  - `uploads`: filename/seq calculation; reject >9 per kind (409); reject non-image MIME (415); reject body >60 MB (413); only-all-three-empty rejection (400); pre-COMMIT failure fully rolls back files + DB; post-COMMIT PDF failure returns **200 with `mergedPdfStale=true`** and restores the prior PDF from `.bak`; concurrent submits to the same order serialize via the per-order lock.
  - `pdfmerge`: N JPEG bytes → N pages in correct order; atomic write; large-image guardrail.
  - `storage`: path-traversal guard rejects `..`, absolute paths, unicode-normalized traversal; orphan janitor cleans `.bak-*` / `.new-*` / `.rename-*` / `.trash/`.
  - `admin`:
      - delete + renumber preserves seq contiguity and restores state on failure.
      - reset renames dir to `.trash/` then deletes DB rows atomically.
      - **year export** (`/api/admin/:year/export.zip`) contains only merged PDF + 发货单 images; no 合同/发票 originals.
      - **per-order bundle** (`/api/admin/:year/orders/:orderNo/bundle.zip`) contains every original photo of every kind plus the merged PDF.
      - `rebuild-pdf` is idempotent and recovers from the `mergedPdfStale` state.
      - CSRF: state-changing admin requests without a valid `X-Admin-Csrf` header are rejected.
- **Backend integration** (`backend/tests/integration/*`):
  - Full `/api/y/2021/orders/xxx/uploads` round trip with `httptest`.
  - Admin login (rate-limited), ping returns CSRF token, CSRF enforcement on DELETE/POST admin routes, zip export.
  - Year-export zip content assertions (merged PDF + 发货单 only).
  - Per-order bundle.zip assertions (all originals + merged PDF).
  - Post-COMMIT PDF failure simulation verifies 200 with `mergedPdfStale=true`, prior PDF preserved, `rebuild-pdf` recovers.
- **Frontend unit** (`frontend/tests/unit/*` via Vitest + JSDOM):
  - `imagePipeline`: HEIC branch (mocked), size-cap, canvas resize math.
  - `lib/filename`: sanitizer matches backend regex.
  - Pinia stores: stage → submit success clears; submit error keeps staged.
- **E2E** out of scope for v1 (manual acceptance per requirements §7).

## 17. Delivery Sequence

1. `backend/internal/db` + migrations + skeleton Gin server.
2. `backend/internal/ingest` + CLI; verify on 21-25订单.csv.
3. `backend/internal/orders` search + progress + detail endpoints.
4. Frontend bootstrap: Vite + router + `/y2021` consumes step-3 endpoints.
5. `backend/internal/storage.OrderLock` + `backend/internal/uploads` atomic submit handler.
6. `backend/internal/pdfmerge` invoked from the submit handler.
7. Frontend `UploadCard` + `imagePipeline` + submit flow end-to-end.
8. `backend/internal/admin` endpoints + session middleware + CSRF.
9. Frontend `/admin/*` views.
10. Zip export (single-order + per-year streaming).
11. Tests and hardening.

## 18. Risks & Mitigations

| Risk | Mitigation |
|------|-----------|
| 购货单位 with rare characters breaks filename | Sanitizer + fallback `未知客户`; unit-tested; same regex on FE and BE |
| Admin forgets password | Password in `config.yaml`; edit + restart |
| User taps 提交 twice on bad Wi-Fi | Button disables + shows spinner; server is append-only so a duplicate retry only adds extra sequences |
| HEIC decode fails on old devices | imagePipeline returns typed error; UI prompts re-shoot |
| Partial write / crash mid-submit | Per-order lock + single DB tx + temp-then-rename files; handler `defer` cleans `.incoming/{txid}` |
| Zip export + concurrent upload | Per-order lock serializes; admin export grabs locks lazily, one order at a time |
| Disk fills | Admin reset + ops documentation to watch `./data/uploads` |
| Port 8080 busy | `listen` in config |
| CSV drift removes an order that already has photos | `orders.csv_present=0` flag + admin filter `onlyCsvRemoved=true` |
