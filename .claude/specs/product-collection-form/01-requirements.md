# Requirements — Product Order Documentation Collection Form

## 1. Context & Goals
We must digitize supporting documents (contract / invoice / delivery slip) for
the orders already listed in `21-25订单.csv` (29,846 rows spanning
Y2021–Y2025). A small team of operators will work order-by-order from their
phones, photographing the paper documents and uploading them against the
corresponding 单据编号 (order number). The tool must be mobile-first, require
no login for the collection flow, and give each operator a clear picture of
remaining work.

### Primary Goals
- G1. Make it as fast as possible for an operator with a phone to find an order
  by 单据编号 and attach photos of three document types to it.
- G2. Guarantee every uploaded photo is deterministically named and grouped
  under its 单据编号, so a later back-office pass never has to guess which
  picture belongs where.
- G3. Automatically combine contract + invoice photos into a single, ordered
  PDF per 单据编号 and keep it plus the original files available to admins.
- G4. Give each operator live visibility into "orders completed vs remaining"
  for the year they are responsible for.
- G5. Let an administrator download any single order's bundle or bulk-export an
  entire year.

### Non-Goals
- No approval/workflow engine: uploaded is the final state.
- No OCR or content extraction from images.
- No authentication for the collection pages (open links on an internal network).
- No mobile-native app — web only, but mobile-first.

## 2. Stakeholders
| Role | Responsibility |
|------|---------------|
| Collection operator | Opens `/y{year}`, searches by 单据编号, uploads documents. |
| Administrator | Opens `/admin`, reviews/downloads/overrides per-year collections. |
| System owner | Deploys the Gin binary on a LAN server, seeds CSV, backs up `./data/`. |

## 3. Scope & Work Distribution
There are five collection pages, one per year (2021–2025). Each link is shared
with a different responsible person. The system **does not** track who is
responsible — the year path alone determines what each link sees:

- `/y2021`, `/y2022`, `/y2023`, `/y2024`, `/y2025` — each shows **only** the
  orders whose `年` column matches that year.
- `/admin` — year-agnostic back office, password protected.

## 4. Functional Requirements

### 4.1 Dataset Ingestion
- **FR-INGEST-1**: On server start, if the `orders` table is empty (or a
  `--reimport` CLI flag is passed), import `21-25订单.csv` into SQLite:
  one row per CSV row; 单据编号 trailing `.` must be stripped; whitespace
  trimmed; `年` stored without the `Y` prefix as `INTEGER year`.
- **FR-INGEST-2**: Expose a derived view "unique orders per year" =
  `COUNT(DISTINCT 单据编号)` per `year`. The five collection pages and the
  admin total both rely on this.
- **FR-INGEST-3**: CSV path and DB path are read from config (default
  `./21-25订单.csv` and `./data/app.db`). Re-importing the same CSV must be
  idempotent (same order keys → upsert, never duplicate rows).

### 4.2 `/y{year}` Collection Page (mobile-first)
- **FR-SEARCH-1**: Sticky search bar at the top. User types any substring (≥2
  characters) of a 单据编号; page lists up to 20 matching orders sorted by
  date ascending. Search is case-insensitive and scoped to **this year only**.
- **FR-SEARCH-2**: Each search-result row displays 单据编号, 购货单位
  (abbreviated if long), and a per-order upload status badge:
  - `✓` with per-type counts (e.g. `合同 2 / 发票 1 / 发货单 0`) if any
    uploads exist.
  - "未上传" if no uploads exist.
- **FR-SEARCH-3**: Tapping a result opens the **Order Detail panel** below the
  search bar. The panel replaces (does not stack) any previously open detail.
- **FR-PROGRESS-1**: Above the search bar, a progress block always shows:
  `本年共 N 个订单号 · 已上传 M · 进度 M/N (P %)`. An order counts as
  "uploaded" if at least one photo of **any** type has been accepted by the
  server. The block re-fetches after every successful submit.

### 4.3 Order Detail Panel
- **FR-DETAIL-1**: Panel header: 单据编号 and a "进度" pill (same semantics as
  4.2 per-order badge).
- **FR-DETAIL-2**: Below the header, a read-only info table for that order.
  Because a 单据编号 may span multiple product lines, show **every matching
  row** as a table with these columns (label them in Chinese):
  单据编号 / 客户 / 产品名称 / 数量 / 价税合计 / 发票号.
  (`客户` is the UI label for `购货单位`.)
- **FR-DETAIL-3**: Below the table, three upload cards appear in this order:
  1. 合同图片拍照上传
  2. 发票拍照上传
  3. 发货单拍照上传
  Each card renders existing server-side photos first (with type + index
  badge), then new staged photos, then an "加号" tile that opens the camera /
  photo library picker using `<input type="file" accept="image/*" capture>`.
- **FR-DETAIL-4**: Within one upload card, staged (not yet submitted) photos
  show a delete icon. Deleting a staged photo is local-only until submit.
  Server-side photos (already submitted) are read-only here and can only be
  changed via `/admin`.
- **FR-DETAIL-5**: A sticky "提交" button at the bottom of the detail panel is
  enabled only when there is at least one staged photo in any card. If all
  three cards are empty of staged photos, the button is disabled with a hint
  "请先添加至少一张图片".

### 4.4 Image Processing (browser-side)
- **FR-IMG-1**: Every selected file goes through a pre-upload pipeline:
  1. If the MIME type is `image/heic`/`image/heif` (iPhone default) convert to
     JPEG. Chosen library must run offline in the browser.
  2. Resize so the longest side is ≤ 2000 px (only downscale; never upscale).
  3. Re-encode to JPEG at quality `0.85`.
  4. Reject files whose original size exceeds 10 MB **after** a first quick
     client-side check (we still decode for HEIC, but reject anything bigger
     than 20 MB raw to avoid OOM on old phones).
- **FR-IMG-2**: A staged photo shows a 96×96 thumbnail and the processed byte
  size, so the user can see that compression happened.
- **FR-IMG-3**: Per-type upload caps (pre-submit staging): ≤ 9 photos per card.
  Hitting the cap disables the "加号" tile with a tooltip.

### 4.5 Submit Flow
- **FR-SUBMIT-1**: On tapping 提交, the frontend posts the staged photos for
  that single 单据编号 in **one** `multipart/form-data` request:
  `POST /api/y{year}/orders/{单号}/uploads`. Fields:
  - `contract[]` (files, 合同)
  - `invoice[]` (files, 发票)
  - `delivery[]` (files, 发货单)
- **FR-SUBMIT-2**: The backend is **append-only**:
  - Existing photos for that 单据编号 and type are kept. The next sequence
    number continues from the current max + 1 for that (单号, 类型).
  - Saved filename pattern:
    `{单号}-{客户名清洗}-{类型}-{序号}.jpg`
    where 类型 ∈ {`合同`, `发票`, `发货单`} and 序号 starts at 1, zero-padded
    to 2 digits (`01`). 客户名 sanitization rule: replace any character in
    ``/ \\ : * ? " < > | ` `` (space) with `_`; collapse consecutive `_`;
    trim trailing `_`.
  - If the 单据编号 has multiple 购货单位 rows (rare but possible), use the
    first row's 购货单位 after sanitization.
- **FR-SUBMIT-3**: After saving raw files, the backend **re-generates** a merged
  PDF named `{单号}-{客户名清洗}-合同与发票.pdf`:
  - Order: all 合同 pages first (by sequence), then all 发票 pages (by
    sequence). 发货单 is **not** included.
  - Each image becomes one PDF page scaled to fit A4 portrait, white background.
  - The previous merged PDF (if any) is overwritten atomically (write to temp,
    rename).
- **FR-SUBMIT-4**: On success, respond `200` with the new photo counts per
  type and the absolute "uploaded order count" for this year. The frontend
  must:
  - Clear all staged photos in the three cards.
  - Re-fetch the order's server-side photos so they now appear as read-only.
  - Re-fetch year-level progress for the header block.
  - Show a transient green toast "提交成功"; stay on the same page (no route
    change).
- **FR-SUBMIT-5**: On 4xx/5xx, keep staged photos intact and show a red toast
  with the server error message; do not clear.

### 4.6 `/admin` Back Office
- **FR-ADMIN-AUTH**: First visit to any `/admin/*` endpoint without a valid
  `admin_session` cookie redirects to `/admin/login`. The login form takes a
  single password field; password is read from config
  (`config.yaml: admin_password: "..."`). On success set an HttpOnly,
  SameSite=Lax cookie valid for 12 h.
- **FR-ADMIN-BROWSE**: `/admin` defaults to year 2021; a year selector switches
  the table. Table columns: 单据编号, 客户, 已上传(是/否), 合同/发票/发货单
  counts, 最后上传时间, 操作.
- **FR-ADMIN-DETAIL**: Row click opens a side panel with all uploaded photos
  (thumbnails per type), a "下载合并 PDF" button (serves the merged PDF from
  disk), and a "下载所有原图 zip" button.
- **FR-ADMIN-BULK-EXPORT**: A "导出本年 zip" button generates
  `{year}-完整资料.zip` containing per-单号 subfolders with the merged PDF and
  the 发货单 images. The zip is streamed (not held fully in memory).
- **FR-ADMIN-OVERWRITE**: In the detail panel, an admin can:
  - Delete an individual photo → renumber remaining photos of that type
    contiguously; regenerate the merged PDF.
  - "重置此单号" → delete everything on disk and in DB for that 单据编号.
  Both actions require a confirm dialog.

### 4.7 Duplicate & Edge Cases
- **FR-EDGE-DUP**: If a 单据编号 exists multiple times in CSV (shared by
  multiple products), uploads attach to the *single* 单据编号 once, and the
  detail table shows every line.
- **FR-EDGE-NAME**: If 购货单位 is empty for every row of a 单据编号 (should
  not happen, but defensively), substitute literal `未知客户`.
- **FR-EDGE-CSVREFRESH**: If CSV is re-imported later and a 单据编号 disappears
  from CSV but still has uploads on disk, keep the uploads and mark the
  order as "CSV 已移除" in admin.

## 5. Non-Functional Requirements

### 5.1 Performance
- NFR-PERF-1: Search endpoint returns in <150 ms for any substring query on a
  3-million-row-free dataset (the full 29k rows fit in memory; a single
  indexed `LIKE` over 单据编号 is enough).
- NFR-PERF-2: Submit endpoint handles 9×3 photos of 2 MB each (≈54 MB) in
  <10 s on a typical LAN; server streams multipart parts to disk and never
  loads the whole body into memory.
- NFR-PERF-3: PDF merge for a typical order (≤20 pages) completes in <2 s.

### 5.2 Reliability & Data Safety
- NFR-REL-1: All file writes go through a temp file + rename to avoid partial
  files. The merged PDF write is atomic.
- NFR-REL-2: SQLite is opened with `journal_mode=WAL` and `synchronous=NORMAL`.
- NFR-REL-3: A nightly cron (documented, not implemented) copies
  `./data/` off-box — the architecture and filesystem layout must make a
  simple `rsync` sufficient (i.e. no scattered state).

### 5.3 Security
- NFR-SEC-1: Collection endpoints are open but accept only `image/*`
  mime-types. Other uploads are rejected with 415.
- NFR-SEC-2: Filename and path inputs are never interpolated raw; the backend
  computes filenames from the 单据编号 + sanitized 客户 + type + sequence.
  Paths are confined to `./data/uploads/` via a `filepath.Clean` check.
- NFR-SEC-3: Admin password is compared with constant-time comparison.
- NFR-SEC-4: No CORS for any origin — the frontend is served by the same Gin
  process (`/` static + `/api/...`) to avoid cross-origin surface.

### 5.4 Usability (Mobile)
- NFR-UX-1: Every tap target is ≥ 44×44 px.
- NFR-UX-2: The search bar and the "提交" button remain on screen on phones
  with a 667 px viewport height without scrolling the other away.
- NFR-UX-3: On iOS Safari and WeChat's built-in browser, opening the camera
  from each upload card must use the device camera (capture attribute).
- NFR-UX-4: The page is usable at 100 % zoom on a 375 px-wide viewport.
- NFR-UX-5: Error and success toasts auto-dismiss in 2.5 s; success also
  vibrates 50 ms on supported devices.

### 5.5 Compatibility
- NFR-COMPAT-1: Latest 2 versions of iOS Safari, Android Chrome, and WeChat
  webview. No IE / no legacy Edge.

## 6. Deployment & Operations
- DEPL-1: Single Go binary on `0.0.0.0:8080`. Serves SPA static files from
  embedded FS (`//go:embed dist`). API under `/api/...`.
- DEPL-2: Data layout:
  ```
  ./data/
    app.db                            # SQLite
    uploads/
      2021/
        RX2101-22926/
          RX2101-22926-哈尔滨金诺食品有限公司-合同-01.jpg
          ...
          RX2101-22926-哈尔滨金诺食品有限公司-合同与发票.pdf
      2022/ ...
    exports/
      2021-完整资料.zip                # generated on demand
  ```
- DEPL-3: Config file `config.yaml`:
  ```yaml
  listen: "0.0.0.0:8080"
  csv_path: "./21-25订单.csv"
  data_dir: "./data"
  admin_password: "CHANGE-ME"
  image:
    max_per_card: 9
    pdf_order: ["合同", "发票"]
  ```
- DEPL-4: `./server import-csv` (subcommand) re-imports the CSV explicitly.

## 7. Acceptance Criteria (spot checks)
1. Open `/y2021` on a phone. Type `22926`. The row for 哈尔滨金诺食品 appears
   within 150 ms showing "未上传". The year header shows e.g. `本年共 5 234
   个订单号 · 已上传 0 · 0 %`.
2. Tap the row, shoot 2 合同 photos and 1 发票 photo, tap 提交. A green
   toast appears, the three cards clear, the order row now shows
   `合同 2 / 发票 1 / 发货单 0`, and the year header shows `已上传 1`.
3. Repeat submit on the same 单号 with 1 additional 合同 photo. The filename
   on disk is `RX2101-22926-哈尔滨金诺食品有限公司-合同-03.jpg` (previous two
   kept). The merged PDF now has 3 合同 pages + 1 发票 page, in that order.
4. On `/admin` after entering the configured password, select year 2021, find
   the same 单号, download the merged PDF — it opens and contains 4 pages in
   the expected order. Click "重置此单号" and confirm — the entry disappears
   from uploads on disk and DB.
5. On `/admin`, click "导出本年 zip" for 2021 — a `.zip` downloads containing
   one subfolder per 单号 with `合同与发票.pdf` and 发货单 photos.
6. Take a HEIC photo from iPhone's camera roll on `/y2022`, upload — the
   staged thumbnail appears as JPEG, and the final file on disk has `.jpg`
   extension.
7. Attempt to upload a 12 MB image — UI rejects with "单张图片需小于 10 MB".
8. Attempt `curl -F file=@foo.pdf https://.../api/y2021/orders/.../uploads`
   with a non-image — responds 415.

## 8. Out of Scope (explicitly)
- Multi-language UI.
- Email / SMS / Slack notifications.
- Offline mode / service worker.
- OCR.
- Analytics dashboard beyond the per-year progress and an optional admin
  submission timeline (deferred).
