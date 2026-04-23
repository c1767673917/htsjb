# Architecture — Invoice Upload Feature

## 1. Database Schema Changes

Increment `currentUserVersion` from 3 to 4. New migration block in `db.go`:

### Table: `invoices`

```sql
CREATE TABLE IF NOT EXISTS invoices (
    invoice_no TEXT PRIMARY KEY,
    customer TEXT NOT NULL,
    customer_clean TEXT NOT NULL,
    seller TEXT NOT NULL,
    invoice_date TEXT NOT NULL,
    csv_present INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX IF NOT EXISTS idx_invoices_no_like ON invoices(invoice_no);
```

Primary key is `invoice_no` (合并发票号码). No year partitioning — all years in one table.

### Table: `invoice_lines`

```sql
CREATE TABLE IF NOT EXISTS invoice_lines (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    invoice_no TEXT NOT NULL,
    year INTEGER NOT NULL,
    invoice_date TEXT NOT NULL,
    seller TEXT NOT NULL,
    customer TEXT NOT NULL,
    product TEXT NOT NULL,
    quantity REAL NOT NULL,
    amount REAL NOT NULL,
    tax_amount REAL NOT NULL,
    total_with_tax REAL NOT NULL,
    tax_rate TEXT NOT NULL,
    source_hash TEXT NOT NULL UNIQUE,
    source_line INTEGER NOT NULL,
    FOREIGN KEY (invoice_no) REFERENCES invoices(invoice_no)
);
CREATE INDEX IF NOT EXISTS idx_invoice_lines_no ON invoice_lines(invoice_no);
```

### Table: `invoice_uploads`

```sql
CREATE TABLE IF NOT EXISTS invoice_uploads (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    invoice_no TEXT NOT NULL,
    seq INTEGER NOT NULL,
    filename TEXT NOT NULL,
    original_name TEXT NOT NULL DEFAULT '',
    content_type TEXT NOT NULL DEFAULT 'image/jpeg',
    byte_size INTEGER NOT NULL,
    sha256 TEXT NOT NULL,
    operator TEXT NOT NULL DEFAULT '',
    uploaded_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (invoice_no, seq),
    FOREIGN KEY (invoice_no) REFERENCES invoices(invoice_no)
);
CREATE INDEX IF NOT EXISTS idx_invoice_uploads_no ON invoice_uploads(invoice_no);
```

Key differences from `uploads` table:
- No `year` or `kind` columns (invoices are not year-grouped or kind-categorized)
- `content_type` column to distinguish images from PDFs
- `original_name` to preserve the original filename for PDFs

## 2. Backend Services

### 2.1 Invoice Ingest (`backend/internal/invoiceingest/`)

New package `invoiceingest` following the same pattern as `ingest`:

- `Importer` struct with `NeedsImport()`, `ImportCSV()`, `ValidateCSV()` methods
- CSV column mapping (0-indexed from the CSV):
  - col 4: 合并发票号码 → `invoice_no`
  - col 8: 购买方名称 → `customer`
  - col 9: 开票日期 → `invoice_date`
  - col 6: 销方名称 → `seller`
  - col 12: 货物或应税劳务名称 → `product`
  - col 15: 数量 → `quantity`
  - col 17: 金额 → `amount`
  - col 19: 税额 → `tax_amount`
  - col 20: 价税合计 → `total_with_tax`
  - col 18: 税率 → `tax_rate` (stored as text, e.g. "9%")
  - col 28: 年 → `year` (parsed from "Y2021")
- Dedup: `source_hash` = SHA256 of key fields, `INSERT OR IGNORE`
- Upsert `invoices` table: one row per unique `invoice_no`, picking first customer/seller/date encountered

### 2.2 Invoice Service (`backend/internal/invoices/`)

New package `invoices` mirroring `orders`:

```go
type Service struct {
    db      *sqlx.DB
    storage *storage.Service
}

// Core methods:
func (s *Service) Search(ctx, q string, limit int) ([]SearchItem, error)
func (s *Service) Detail(ctx, invoiceNo string) (Detail, error)
func (s *Service) AdminList(ctx, page, size int, query string, onlyUploaded bool) (AdminList, error)
func (s *Service) AdminExportAll(ctx, query string, onlyUploaded bool) ([]AdminListItem, error)
```

Types:
```go
type SearchItem struct {
    InvoiceNo   string `json:"invoiceNo"`
    Customer    string `json:"customer"`
    InvoiceDate string `json:"invoiceDate"`
    Uploaded    bool   `json:"uploaded"`
    UploadCount int    `json:"uploadCount"`
}

type Detail struct {
    InvoiceNo string       `json:"invoiceNo"`
    Customer  string       `json:"customer"`
    Seller    string       `json:"seller"`
    InvoiceDate string     `json:"invoiceDate"`
    Lines     []Line       `json:"lines"`
    Uploads   []UploadFile `json:"uploads"`
}
```

### 2.3 Invoice Upload Service (`backend/internal/invoiceuploads/`)

New package `invoiceuploads` handling file upload for invoices:

- Reuse `storage.Service` for filesystem paths and locking
- **Image files**: same pipeline as existing (decode → re-encode JPEG)
- **PDF files**: stream directly to disk, no image conversion. MIME type `application/pdf` is accepted alongside images.
- Storage path: `data/uploads/invoices/{invoiceNo}/{filename}`
- Filename pattern: `{invoiceNo}-{seq:02d}.{ext}` where ext is `jpg` for images, `pdf` for PDFs
- Lock key: `"inv:" + invoiceNo` (reuse storage.Service Acquire with a wrapper)

### 2.4 Invoice Admin Routes

Add to existing `admin.Service.RegisterRoutes()`:

```
GET    /admin/invoices                — list invoices with pagination
GET    /admin/invoices/:invoiceNo     — invoice detail
DELETE /admin/invoices/:invoiceNo/uploads/:id — delete upload
DELETE /admin/invoices/:invoiceNo     — reset invoice (delete all uploads)
GET    /admin/invoices/export.csv     — export invoice list as CSV
```

All admin invoice routes require the existing session + CSRF middleware.

## 3. API Endpoints

### Public Endpoints (no auth)

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/invoices/search?q=&limit=` | Fuzzy search invoices by invoice_no |
| GET | `/api/invoices/:invoiceNo` | Invoice detail + uploads |
| POST | `/api/invoices/:invoiceNo/uploads` | Upload files (multipart) |
| DELETE | `/api/invoices/:invoiceNo/uploads/:id` | Delete a single upload |
| GET | `/files/invoices/:invoiceNo/:filename` | Serve uploaded file |

### Admin Endpoints (session + CSRF)

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/admin/invoices?page=&size=&q=&onlyUploaded=` | Paginated invoice list |
| GET | `/api/admin/invoices/:invoiceNo` | Invoice detail |
| DELETE | `/api/admin/invoices/:invoiceNo/uploads/:id` | Delete upload (CSRF) |
| DELETE | `/api/admin/invoices/:invoiceNo` | Reset invoice (CSRF) |
| GET | `/api/admin/invoices/export.csv` | CSV export |

### Request/Response Shapes

**Search Response:**
```json
{
  "items": [
    {
      "invoiceNo": "31449777",
      "customer": "周小鹏",
      "invoiceDate": "2021-12-31",
      "uploaded": true,
      "uploadCount": 2
    }
  ]
}
```

**Detail Response:**
```json
{
  "invoiceNo": "31449777",
  "customer": "周小鹏",
  "seller": "上海瑞勋国际贸易有限公司",
  "invoiceDate": "2021-12-31 12:33:37",
  "lines": [
    {
      "product": "*植物油*百圣深层煎炸起酥油",
      "quantity": 400,
      "amount": 57247.71,
      "taxAmount": 5152.29,
      "totalWithTax": 62400,
      "taxRate": "9%"
    }
  ],
  "uploads": [
    {
      "id": 1,
      "seq": 1,
      "filename": "31449777-01.jpg",
      "url": "/files/invoices/31449777/31449777-01.jpg",
      "size": 123456,
      "contentType": "image/jpeg",
      "operator": "张三"
    }
  ]
}
```

**Upload Request:** Multipart form with fields `invoice_photo[]` (images) and `invoice_pdf[]` (PDFs), plus optional `operator` text field.

**Upload Response:**
```json
{
  "uploadCount": 3
}
```

## 4. Storage Layout

```
data/
├── uploads/
│   ├── invoices/              ← NEW
│   │   ├── 31449777/
│   │   │   ├── 31449777-01.jpg
│   │   │   ├── 31449777-02.pdf
│   │   │   └── ...
│   │   └── .incoming/         ← temp staging
│   ├── 2021/                  ← existing order uploads
│   ├── 2022/
│   └── ...
└── app.db
```

The `storage.Service.EnsureLayout()` method is updated to also create `uploads/invoices/` and `uploads/invoices/.incoming/`.

## 5. Config Changes

Add to `config.yaml`:
```yaml
invoice_csv_path: "./油脂发票.csv"
```

Add to `config.Config` struct:
```go
InvoiceCSVPath string `yaml:"invoice_csv_path"`
```

## 6. Frontend Architecture

### 6.1 Router Changes

Add to `router.ts`:
```typescript
{
  path: '/invoices',
  name: 'invoices',
  component: InvoiceView,
  meta: { title: '发票录入' },
},
{
  path: '/invoices/:operator',
  name: 'invoices-operator',
  component: InvoiceView,
  props: (route) => ({ operator: String(route.params.operator ?? '') }),
  meta: { title: '发票录入' },
},
```

### 6.2 New Components

| File | Classification | Description |
|------|---------------|-------------|
| `views/InvoiceView.vue` | frontend | Invoice collection page (mirrors CollectionView) |
| `components/InvoiceDetailPanel.vue` | frontend | Invoice detail + upload area |
| `components/InvoiceUploadCard.vue` | frontend | File picker for images + PDFs with paste support |

### 6.3 New Store

`stores/invoice.ts` — mirrors `collection.ts` pattern:
- `searchQuery`, `searchResults`, `searching` — search state
- `currentDetail` — selected invoice detail
- `staged` — staged files (images + PDFs) before submit
- `operator` — current operator name
- Methods: `runSearch()`, `openInvoice()`, `stageFiles()`, `submit()`, `deleteUpload()`

### 6.4 API Client Extensions

Add to `lib/api.ts`:
```typescript
export interface InvoiceSearchItem {
  invoiceNo: string;
  customer: string;
  invoiceDate: string;
  uploaded: boolean;
  uploadCount: number;
}

export interface InvoiceLine {
  product: string;
  quantity: number;
  amount: number;
  taxAmount: number;
  totalWithTax: number;
  taxRate: string;
}

export interface InvoiceUploadFile {
  id: number;
  seq: number;
  filename: string;
  url: string;
  size: number;
  contentType: string;
  operator?: string;
}

export interface InvoiceDetail {
  invoiceNo: string;
  customer: string;
  seller: string;
  invoiceDate: string;
  lines: InvoiceLine[];
  uploads: InvoiceUploadFile[];
}

export const invoiceApi = {
  search(q: string, limit?: number, signal?: AbortSignal) { ... },
  detail(invoiceNo: string, signal?: AbortSignal) { ... },
  submit(invoiceNo: string, form: FormData) { ... },
  deleteUpload(invoiceNo: string, id: number) { ... },
};
```

### 6.5 Admin Panel Changes

In `AdminView.vue`, add a tab/toggle for "发票管理":
- Tab options: "订单管理" (existing) | "发票管理" (new)
- Invoice tab shows `InvoiceAdminPanel.vue` component
- `stores/admin.ts` extended with invoice-related state and methods

New admin API functions in `lib/api.ts`:
```typescript
adminApi.invoiceList(params) { ... }
adminApi.invoiceDetail(invoiceNo) { ... }
adminApi.deleteInvoiceUpload(invoiceNo, id, csrf) { ... }
adminApi.resetInvoice(invoiceNo, csrf) { ... }
adminApi.invoiceCsvExportUrl(filters) { ... }
```

### 6.6 PDF Upload Handling

Frontend changes to support PDF:
- `InvoiceUploadCard.vue` accepts both images and PDFs via `accept="image/*,.pdf,application/pdf"`
- Images go through existing `imagePipeline.ts` (HEIC→JPEG conversion, resize)
- PDFs bypass the image pipeline — stored as raw `File` objects in staged state
- Display: PDFs show a file icon + filename; images show thumbnail preview
- Clipboard paste only applies to images (browsers paste images, not PDFs)

## 7. Bootstrap Changes

In `app/run.go` `serve()`:
1. Create `invoiceingest.Importer` alongside existing `ingest.Importer`
2. Check `invoiceImporter.NeedsImport()` → if true, run `ImportCSV(cfg.InvoiceCSVPath)`
3. Create `invoices.Service` and `invoiceuploads.Service`
4. Pass new services to `httpapi.Router`

In `app/run.go` `importCSV()`:
- Add `import-invoice-csv` subcommand with same flags (`--reimport`, `--dry-run`, `--error-report`)

## 8. Route Registration

In `httpapi/router.go`, add new route groups:

```go
// Public invoice routes
invGroup := api.Group("/invoices")
invGroup.GET("/search", r.handleInvoiceSearch)
invGroup.GET("/:invoiceNo", r.handleInvoiceDetail)
invGroup.POST("/:invoiceNo/uploads", r.invoiceUploads.HandleSubmit)
invGroup.DELETE("/:invoiceNo/uploads/:id", r.invoiceUploads.HandleDelete)

// Invoice file serving
engine.GET("/files/invoices/:invoiceNo/:filename", r.handleInvoiceFile)

// Admin invoice routes (inside existing adminGroup)
authed.GET("/invoices", s.handleInvoiceList)
authed.GET("/invoices/export.csv", s.handleInvoiceCSVExport)
authed.GET("/invoices/:invoiceNo", s.handleInvoiceDetail)
mutating.DELETE("/invoices/:invoiceNo/uploads/:id", s.handleDeleteInvoiceUpload)
mutating.DELETE("/invoices/:invoiceNo", s.handleResetInvoice)
```

## 9. Timeout Configuration

Add to `routeTimeout()` in `router.go`:
```go
case method == http.MethodPost && strings.HasPrefix(requestPath, "/api/invoices/") && strings.HasSuffix(requestPath, "/uploads"):
    return 120 * time.Second
case strings.HasPrefix(requestPath, "/files/invoices/"):
    return 120 * time.Second
```

## Component Classification

| Component | Type | Description |
|-----------|------|-------------|
| `db.go` migration v4 | backend | Add invoices, invoice_lines, invoice_uploads tables |
| `backend/internal/invoiceingest/` | backend | CSV importer for 油脂发票.csv |
| `backend/internal/invoices/service.go` | backend | Invoice search, detail, admin list |
| `backend/internal/invoiceuploads/service.go` | backend | Invoice file upload + delete |
| `config.go` changes | backend | Add `invoice_csv_path` field |
| `app/run.go` changes | backend | Bootstrap invoice services, add CLI subcommand |
| `httpapi/router.go` changes | backend | Register invoice API routes |
| `admin/service.go` changes | backend | Add invoice admin route handlers |
| `storage/storage.go` changes | backend | Add invoice storage paths, update EnsureLayout |
| `frontend/src/router.ts` | frontend | Add /invoices routes |
| `frontend/src/lib/api.ts` | frontend | Add invoiceApi + admin invoice API functions |
| `frontend/src/stores/invoice.ts` | frontend | Invoice collection store |
| `frontend/src/stores/admin.ts` | frontend | Extend with invoice admin state |
| `frontend/src/views/InvoiceView.vue` | frontend | Invoice collection page |
| `frontend/src/components/InvoiceDetailPanel.vue` | frontend | Invoice detail + upload panel |
| `frontend/src/components/InvoiceUploadCard.vue` | frontend | File picker (images + PDF) |
| `frontend/src/views/AdminView.vue` | frontend | Add invoice tab |
