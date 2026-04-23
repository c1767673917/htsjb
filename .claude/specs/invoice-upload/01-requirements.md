# Requirements — Invoice Upload Feature (油脂发票)

## 1. Overview

Add an invoice data management module that mirrors the existing order collection workflow. Users import invoice records from `油脂发票.csv`, search by combined invoice number (合并发票号码), view invoice details (invoice number, customer name, invoice date), and upload invoice photos/PDFs.

## 2. Functional Requirements

### FR-INV-IMPORT: Invoice CSV Import

- **FR-INV-IMPORT-1**: On first boot (or when the `invoices` table is empty), import `油脂发票.csv` into new database tables (`invoices` and `invoice_lines`).
- **FR-INV-IMPORT-2**: Support re-import via CLI command (`import-invoice-csv --reimport`).
- **FR-INV-IMPORT-3**: CSV columns mapped:
  - 合并发票号码 → `invoice_no` (primary identifier, used for search)
  - 购买方名称 → `customer` (customer/buyer name)
  - 开票日期 → `invoice_date` (invoice date)
  - 销方名称 → `seller` (seller name)
  - 金额 → `amount`
  - 税额 → `tax_amount`
  - 价税合计 → `total_with_tax`
  - 年 → `year` (parsed from "Y2021" format)
  - Other columns stored in `invoice_lines` for detail view.
- **FR-INV-IMPORT-4**: Multiple CSV rows with the same 合并发票号码 are treated as line items of one invoice. The `invoices` table stores one row per unique `invoice_no` (no year grouping at the table level; year is stored as a column for reference but not used in routing or search).
- **FR-INV-IMPORT-5**: Add `invoice_csv_path` to `config.yaml` for the invoice CSV file path, defaulting to `./油脂发票.csv`.

### FR-INV-SEARCH: Invoice Fuzzy Search

- **FR-INV-SEARCH-1**: New API endpoint `GET /api/invoices/search?q=xxx&limit=20` performs fuzzy search on `invoice_no LIKE '%' || ? || '%'`. The `invoice_no` column stores 合并发票号码 values.
- **FR-INV-SEARCH-2**: Minimum 2 characters required for search (consistent with existing order search).
- **FR-INV-SEARCH-3**: Returns: `invoiceNo` (displayed to user as "发票号码"), `customer` (购买方名称), `invoiceDate` (开票日期), upload status.
- **FR-INV-SEARCH-4**: Results limited to 50 maximum, default 20.
- **FR-INV-SEARCH-5**: No year grouping — search spans all years. The endpoint does not take a year parameter.

### FR-INV-DETAIL: Invoice Detail View

- **FR-INV-DETAIL-1**: New API endpoint `GET /api/invoices/:invoiceNo` returns invoice details.
- **FR-INV-DETAIL-2**: Response includes:
  - Invoice number (合并发票号码)
  - Customer name (购买方名称)
  - Invoice date (开票日期)
  - Seller name (销方名称)
  - Line items (product name, amount, tax, total)
  - Uploaded photos list
- **FR-INV-DETAIL-3**: Line items are all rows from `invoice_lines` with matching invoice_no.

### FR-INV-UPLOAD: Invoice Photo/PDF Upload

- **FR-INV-UPLOAD-1**: New API endpoint `POST /api/invoices/:invoiceNo/uploads` accepts multipart form with invoice files.
- **FR-INV-UPLOAD-2**: Upload field name: `invoice_photo` or `invoice_photo[]`.
- **FR-INV-UPLOAD-3**: Accepted formats: JPEG, PNG, WebP images + PDF files.
- **FR-INV-UPLOAD-4**: Support clipboard paste (Ctrl+V / Cmd+V) for images — reuse existing frontend image pipeline.
- **FR-INV-UPLOAD-5**: **PDF files stored as-is** without image conversion. Images are re-encoded to JPEG as in existing flow.
- **FR-INV-UPLOAD-6**: Files stored at `data/uploads/invoices/{invoiceNo}/`.
- **FR-INV-UPLOAD-7**: Upload metadata stored in new `invoice_uploads` table.
- **FR-INV-UPLOAD-8**: Include `operator` field for tracking who uploaded.
- **FR-INV-UPLOAD-9**: Per-invoice upload cap: 50 files (reuse `per_kind_max` config).

### FR-INV-DELETE: Invoice Upload Deletion

- **FR-INV-DELETE-1**: `DELETE /api/invoices/:invoiceNo/uploads/:id` deletes a single uploaded file.
- **FR-INV-DELETE-2**: Any user can delete any upload (consistent with existing behavior).

### FR-INV-PAGE: Invoice Collection Page (Frontend)

- **FR-INV-PAGE-1**: New route `/invoices` — invoice collection page. No year grouping.
- **FR-INV-PAGE-2**: Search bar: input a few digits of "发票号码" (internally 合并发票号码), fuzzy search, select from results. User-facing label is "发票号码", not "合并发票号码".
- **FR-INV-PAGE-3**: After selection, display:
  - 发票号码 (合并发票号码 value)
  - 购买方名称 (customer name)
  - 开票日期 (invoice date)
  - Line item details (product, amount, tax)
  - Already uploaded files (photos + PDFs)
- **FR-INV-PAGE-4**: Upload area: pick or paste images, take photo, upload PDF files.
- **FR-INV-PAGE-5**: Submit button: upload all staged files in one multipart POST.
- **FR-INV-PAGE-6**: After successful upload, refresh detail to show new files.
- **FR-INV-PAGE-7**: Support paste image from clipboard (Ctrl+V / Cmd+V).
- **FR-INV-PAGE-8**: PDF files displayed with a file/PDF icon and filename, not as image previews.

### FR-INV-ADMIN: Invoice Admin Module

- **FR-INV-ADMIN-1**: New tab/section within existing `/admin` page for invoice management.
- **FR-INV-ADMIN-2**: List all invoices with pagination, search by invoice number, upload status filter.
- **FR-INV-ADMIN-3**: View invoice detail and uploaded files (photos + PDFs).
- **FR-INV-ADMIN-4**: Delete individual uploads.
- **FR-INV-ADMIN-5**: Reset invoice (delete all uploads for an invoice).
- **FR-INV-ADMIN-6**: Export invoice list as CSV.
- **FR-INV-ADMIN-7**: Reuse existing admin session auth and CSRF mechanism.
- **FR-INV-ADMIN-8**: No year grouping — admin sees all invoices across all years in one list.

## 3. Non-Functional Requirements

- **NFR-COMPAT-1**: Reuse existing upload infrastructure (storage service, image pipeline, concurrency limits).
- **NFR-COMPAT-2**: New tables must be added via SQLite migration (increment `user_version`).
- **NFR-COMPAT-3**: New CSV import must not interfere with existing order CSV import.
- **NFR-PERF-1**: Search response time < 200ms for typical queries.
- **NFR-PERF-2**: Invoice CSV import (46K rows) should complete within 30 seconds.
- **NFR-UX-1**: Mobile-first responsive design, consistent with existing collection page.
- **NFR-UX-2**: PDF uploads displayed with a PDF icon/thumbnail rather than image preview.

## 4. Out of Scope

- Invoice OCR or automatic data extraction from photos
- Invoice validation against tax authority
- Multi-tenant / per-user access control (beyond admin auth)
- Invoice-to-order linking/matching

## 5. Acceptance Criteria

1. `油脂发票.csv` can be imported into the database on first boot.
2. Users can search invoices by partial 合并发票号码 and see results with customer name + date.
3. Users can select an invoice and upload photos/PDFs.
4. Uploaded files are persisted on disk and in the database.
5. Admin panel shows invoice list with upload status and supports management operations.
6. Existing order collection functionality is unaffected.
