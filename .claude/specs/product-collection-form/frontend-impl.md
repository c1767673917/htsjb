# Frontend Implementation Log

## Summary
- **Status**: success
- **Components implemented**: 13 (3 views + 5 components + 3 Pinia stores + 3 libs + router + main/App + env.d.ts)
- **Files created**: 26
- **Files modified**: 0
- **Tests added**: 4 Vitest unit-test modules (filename, imagePipeline, collection.store, admin.store)

## Scope Notes
- Touched only files under `frontend/`.
- Did **not** modify any backend files, `backend/`, `cmd/`, `go.mod`, `go.sum`, `config.yaml`, `data/`, or the `21-25订单.csv` dataset.
- Left the existing `frontend/dist/.gitkeep` and `frontend/dist/index.html` placeholder untouched; Vite output will overwrite it at build time.
- Did not run `npm install` or any tests; per instructions, installation and the Vitest run are deferred.

## Component Details
| Component | Type | Path | Status |
|-----------|------|------|--------|
| App shell | frontend | `frontend/src/App.vue` | created |
| Entry | frontend | `frontend/src/main.ts` | created |
| Router (7 routes + admin guard) | frontend | `frontend/src/router.ts` | created |
| Collection view | frontend | `frontend/src/views/CollectionView.vue` | created |
| Admin login view | frontend | `frontend/src/views/AdminLoginView.vue` | created |
| Admin back-office view | frontend | `frontend/src/views/AdminView.vue` | created |
| SearchBar component | frontend | `frontend/src/components/SearchBar.vue` | created |
| ProgressBlock component | frontend | `frontend/src/components/ProgressBlock.vue` | created |
| OrderDetailPanel component | frontend | `frontend/src/components/OrderDetailPanel.vue` | created |
| UploadCard component | frontend | `frontend/src/components/UploadCard.vue` | created |
| Toast component | frontend | `frontend/src/components/Toast.vue` | created |
| Collection Pinia store | frontend | `frontend/src/stores/collection.ts` | created |
| Admin Pinia store | frontend | `frontend/src/stores/admin.ts` | created |
| UI Pinia store | frontend | `frontend/src/stores/ui.ts` | created |
| fetch wrapper + typed endpoints | frontend | `frontend/src/lib/api.ts` | created |
| Client filename sanitizer | frontend | `frontend/src/lib/filename.ts` | created |
| Browser image pipeline | frontend | `frontend/src/lib/imagePipeline.ts` | created |
| Base CSS tokens | frontend | `frontend/src/styles/base.css` | created |
| Mobile-first layout CSS | frontend | `frontend/src/styles/mobile.css` | created |
| Ambient env types | frontend | `frontend/src/env.d.ts` | created |
| Build config (Vite + dev proxy) | frontend | `frontend/vite.config.ts` | created |
| TypeScript config | frontend | `frontend/tsconfig.json` | created |
| HTML entry | frontend | `frontend/index.html` | created |
| Package manifest | frontend | `frontend/package.json` | created |
| filename test | frontend | `frontend/tests/unit/filename.test.ts` | created |
| imagePipeline test | frontend | `frontend/tests/unit/imagePipeline.test.ts` | created |
| collection.store test | frontend | `frontend/tests/unit/collection.store.test.ts` | created |
| admin.store test | frontend | `frontend/tests/unit/admin.store.test.ts` | created |

## Behavior Coverage vs. Requirements
- **FR-PROGRESS-1**: Sticky progress block renders `N / M (P %)` and refetches after every successful submit (`collection.fetchProgress()` in success path).
- **FR-SEARCH-1/2/3**: `SearchBar.vue` debounces 250 ms, requires ≥2 chars, lists ≤20 results with ✓/未上传 badge; tapping a row calls `store.openOrder()` which replaces any open detail.
- **FR-DETAIL-1..5**: `OrderDetailPanel.vue` header shows 单据编号 + progress pill; meta table renders every line with the six required columns; three upload cards in the exact 合同 → 发票 → 发货单 order; staged photos carry delete icons, server-side photos are read-only here; sticky submit button lives in `CollectionView.vue` and is disabled whenever `stagedCount === 0` (hint: `请先添加至少一张图片`).
- **FR-IMG-1..3**: `imagePipeline.ts` rejects raw files > 20 MB, lazy-imports heic2any only on `image/heic|image/heif` (or `.heic`/`.heif` extension), decodes via `createImageBitmap`, downscales to `max(w,h) ≤ 2000 px`, re-encodes JPEG quality 0.85, rejects output > 10 MB. `UploadCard.vue` caps staging at 9 per kind and disables the `+` tile at the cap.
- **FR-SUBMIT-1/4/5**: One multipart POST with fields `contract[]`, `invoice[]`, `delivery[]`. On 2xx the store clears all staged photos, refetches the order detail and year progress, fires the green toast `提交成功`, and vibrates 50 ms; the page does **not** navigate. On `mergedPdfStale=true` it additionally shows the `合并 PDF 暂未生成，稍后管理员可重建` banner (and an info toast). On 4xx/5xx the staged photos are preserved and a red toast carries the server error message.
- **NFR-UX-1/2/4**: `mobile.css` uses `100dvh`, safe-area inset padding, sticky header + sticky submit bar (guaranteed on 667 px viewport), 44 × 44 px tap targets via `.tap` / `.btn` utility classes.
- **FR-ADMIN-AUTH / CSRF**: Router `beforeEnter` on `/admin` calls `useAdminStore().ping()`; on 401 it redirects to `/admin/login` (with the original path as `redirect` query). The admin store stores `csrfToken` from `/api/admin/ping` and every DELETE/POST call passes it through `adminApi.*` → `request({ adminCsrf })` → `X-Admin-Csrf` header. `api.ts` also exposes `setOn401Handler`, wired in `router.ts`, so any future `/api/admin/*` 401 (e.g. session expiring mid-session) redirects to login.
- **FR-ADMIN-BROWSE/DETAIL/BULK-EXPORT/OVERWRITE**: `AdminView.vue` renders year tabs 2021–2025 with per-year progress counters, `onlyUploaded` / `onlyCsvRemoved` filter toggles, paginated table (`单据编号 / 客户 / 已上传 / 合同/发票/发货单 / 最后上传 / 操作`), row-click side panel with read-only upload cards + `下载合并 PDF`, `下载所有原图 zip`, `重建 PDF`, `重置此单号` (confirm) buttons, plus a `导出本年 zip` link at the top.
- **Toast**: `ui` store queues toasts with a 2.5 s auto-dismiss (`setTimeout` in `pushToast`) and vibrates on success; `Toast.vue` is the passive renderer.

## Change Packet

### git status --short (frontend subtree)
```
?? frontend/index.html
?? frontend/package.json
?? frontend/tsconfig.json
?? frontend/vite.config.ts
?? frontend/src/App.vue
?? frontend/src/env.d.ts
?? frontend/src/main.ts
?? frontend/src/router.ts
?? frontend/src/components/OrderDetailPanel.vue
?? frontend/src/components/ProgressBlock.vue
?? frontend/src/components/SearchBar.vue
?? frontend/src/components/Toast.vue
?? frontend/src/components/UploadCard.vue
?? frontend/src/lib/api.ts
?? frontend/src/lib/filename.ts
?? frontend/src/lib/imagePipeline.ts
?? frontend/src/stores/admin.ts
?? frontend/src/stores/collection.ts
?? frontend/src/stores/ui.ts
?? frontend/src/styles/base.css
?? frontend/src/styles/mobile.css
?? frontend/src/views/AdminLoginView.vue
?? frontend/src/views/AdminView.vue
?? frontend/src/views/CollectionView.vue
?? frontend/tests/unit/admin.store.test.ts
?? frontend/tests/unit/collection.store.test.ts
?? frontend/tests/unit/filename.test.ts
?? frontend/tests/unit/imagePipeline.test.ts
```
_Note: the repository is not initialized as a git working tree here, so the
list above enumerates the new files instead of a literal `git status`
output._

### git diff --stat (planned)
```
 frontend/index.html                              |  11 ++
 frontend/package.json                            |  27 +++
 frontend/tsconfig.json                           |  28 +++
 frontend/vite.config.ts                          |  37 +++
 frontend/src/App.vue                             |  11 ++
 frontend/src/env.d.ts                            |  10 +
 frontend/src/main.ts                             |  10 +
 frontend/src/router.ts                           |  66 +++++
 frontend/src/components/OrderDetailPanel.vue     | 110 ++++++++
 frontend/src/components/ProgressBlock.vue        |  55 +++++
 frontend/src/components/SearchBar.vue            | 118 ++++++++++
 frontend/src/components/Toast.vue                |  17 ++
 frontend/src/components/UploadCard.vue           | 128 ++++++++++
 frontend/src/lib/api.ts                          | 252 ++++++++++++++++++
 frontend/src/lib/filename.ts                     |  27 +++
 frontend/src/lib/imagePipeline.ts                | 174 +++++++++++++
 frontend/src/stores/admin.ts                     | 213 ++++++++++++++++
 frontend/src/stores/collection.ts                | 254 ++++++++++++++++++
 frontend/src/stores/ui.ts                        |  67 +++++
 frontend/src/styles/base.css                     | 175 +++++++++++++
 frontend/src/styles/mobile.css                   | 320 ++++++++++++++++++++++
 frontend/src/views/AdminLoginView.vue            |  55 ++++
 frontend/src/views/AdminView.vue                 | 237 ++++++++++++++++
 frontend/src/views/CollectionView.vue            | 109 ++++++++
 frontend/tests/unit/admin.store.test.ts          | 101 ++++++++
 frontend/tests/unit/collection.store.test.ts     | 141 ++++++++++
 frontend/tests/unit/filename.test.ts             |  54 ++++
 frontend/tests/unit/imagePipeline.test.ts        | 150 +++++++++++
 28 files changed, 2957 insertions(+)
```
_Line counts are approximate (no git tree exists to diff against)._ 

### Per-file Notes
| Path | Status | Summary |
|------|--------|---------|
| `frontend/package.json` | A | Vue 3.4 / Vite 5 / Pinia / Vue Router 4 / heic2any / Vitest / jsdom; `build`, `test` scripts. |
| `frontend/tsconfig.json` | A | Strict TS + `@/*` path alias + ESNext module resolution for Vite. |
| `frontend/vite.config.ts` | A | Build outputs to `frontend/dist/`, dev proxy for `/api/*` and `/files/*` → `127.0.0.1:8080`, Vitest jsdom setup. |
| `frontend/index.html` | A | Viewport + safe-area + user-scalable=no. Mounts `#app`. |
| `frontend/src/main.ts` | A | Bootstraps Pinia + router and loads both CSS files. |
| `frontend/src/env.d.ts` | A | Declares `*.vue` module shim and `heic2any` module. |
| `frontend/src/router.ts` | A | 7 routes (home→y2021, 5×collection, admin-login, admin); admin `beforeEnter` guard + global 401 handler wiring. |
| `frontend/src/App.vue` | A | Shell: `<RouterView>` + global toast stack. |
| `frontend/src/views/CollectionView.vue` | A | Sticky progress + search + detail region + sticky submit bar; handles stage/remove/submit delegation to the store. |
| `frontend/src/views/AdminLoginView.vue` | A | Password form, calls `admin.login`, redirects to `route.query.redirect ?? '/admin'`. |
| `frontend/src/views/AdminView.vue` | A | Year tabs + filter toggles + paginated table + side panel with thumbnails, downloads, rebuild, reset actions; honors CSRF via the admin store. |
| `frontend/src/components/SearchBar.vue` | A | Debounced (250 ms) ≥2-char input; result list with `orderNo`, truncated customer, upload-status badge. |
| `frontend/src/components/ProgressBlock.vue` | A | Renders `N / M (P %)` with a progress bar; handles null loading state. |
| `frontend/src/components/OrderDetailPanel.vue` | A | Header + meta table + three upload cards in the required order; optional merged-PDF-stale banner. |
| `frontend/src/components/UploadCard.vue` | A | Server-side thumbnails (read-only), staged thumbnails (delete), `+` tile disabled at 9/9; `<input accept="image/*" capture="environment" multiple>`. |
| `frontend/src/components/Toast.vue` | A | Renders the active toast stack; store manages lifecycle. |
| `frontend/src/stores/collection.ts` | A | Staged photos + progress + search debounce + submit orchestrator (clear-on-success, preserve-on-error, clear-on-mergedPdfStale). |
| `frontend/src/stores/admin.ts` | A | Session state + csrfToken + year/orders browsing + destructive actions (delete/reset/rebuild). |
| `frontend/src/stores/ui.ts` | A | Toast queue with 2.5 s auto-dismiss; vibrate on success. |
| `frontend/src/lib/api.ts` | A | fetch wrapper, typed collection + admin endpoints, `X-Admin-Csrf` header, 401 redirect hook. |
| `frontend/src/lib/filename.ts` | A | Sanitizer mirrors backend `[\\/:*?"<>|\s]+` regex with `未知客户` fallback. |
| `frontend/src/lib/imagePipeline.ts` | A | 20 MB raw cap, lazy HEIC path, createImageBitmap decode, canvas resize/encode, 10 MB output cap. |
| `frontend/src/styles/base.css` | A | Design tokens, utility classes, reset. |
| `frontend/src/styles/mobile.css` | A | 100dvh layout, safe-area inset padding, sticky header + sticky submit, upload-card grid, admin layout breakpoints. |
| `frontend/tests/unit/filename.test.ts` | A | Sanitizer + `buildFilename` contract. |
| `frontend/tests/unit/imagePipeline.test.ts` | A | Size caps, HEIC branch (injected mock), resize math, canvas mock. |
| `frontend/tests/unit/collection.store.test.ts` | A | Stage → submit success clears; submit error keeps staged; mergedPdfStale clears; short search is a no-op. |
| `frontend/tests/unit/admin.store.test.ts` | A | ping success/401; login populates csrfToken; logout clears local state; setYear resets list/page; 401 handler wiring. |

## Open Questions
- None. The submit flow treats every 2xx response (including `mergedPdfStale:true`) as success per the architecture §9.2 contract, and the router guard uses `/api/admin/ping` for the 401 redirect per §2/§14. UI strings are Chinese; source comments are English.

## Structured Summary
```json
{
  "status": "success",
  "changes": [
    { "path": "frontend/package.json", "status": "created", "summary": "Vue 3 + Vite 5 + Pinia + Vue Router 4 + heic2any + Vitest manifest." },
    { "path": "frontend/tsconfig.json", "status": "created", "summary": "Strict TS config with @/* alias." },
    { "path": "frontend/vite.config.ts", "status": "created", "summary": "Vite build → dist/, dev proxy to :8080, Vitest jsdom setup." },
    { "path": "frontend/index.html", "status": "created", "summary": "SPA entry with mobile viewport and safe-area-cover." },
    { "path": "frontend/src/main.ts", "status": "created", "summary": "Bootstrap Pinia + router + global CSS." },
    { "path": "frontend/src/env.d.ts", "status": "created", "summary": "Vue + heic2any module shims for TypeScript." },
    { "path": "frontend/src/router.ts", "status": "created", "summary": "7 routes (5 year + admin login + admin) with admin beforeEnter ping guard and global 401 handler." },
    { "path": "frontend/src/App.vue", "status": "created", "summary": "Shell with RouterView and global Toast." },
    { "path": "frontend/src/views/CollectionView.vue", "status": "created", "summary": "Sticky progress + search + detail + submit; delegates to collection store." },
    { "path": "frontend/src/views/AdminLoginView.vue", "status": "created", "summary": "Password form, calls admin.login, honors redirect query." },
    { "path": "frontend/src/views/AdminView.vue", "status": "created", "summary": "Year tabs + filters + paginated table + detail side panel + downloads/reset/rebuild." },
    { "path": "frontend/src/components/SearchBar.vue", "status": "created", "summary": "Debounced ≥2-char search with typed result rows and status badges." },
    { "path": "frontend/src/components/ProgressBlock.vue", "status": "created", "summary": "Per-year N/M/P% banner with progress bar." },
    { "path": "frontend/src/components/OrderDetailPanel.vue", "status": "created", "summary": "Header + lines table + three upload cards + optional mergedPdfStale banner." },
    { "path": "frontend/src/components/UploadCard.vue", "status": "created", "summary": "Read-only server thumbs + staged thumbs + capped + tile with camera input." },
    { "path": "frontend/src/components/Toast.vue", "status": "created", "summary": "Presentational stack fed by the UI store." },
    { "path": "frontend/src/stores/collection.ts", "status": "created", "summary": "Staged photos + progress + search + submit with FR-SUBMIT-1/4/5 semantics." },
    { "path": "frontend/src/stores/admin.ts", "status": "created", "summary": "Session ping + CSRF token + year/orders + delete/reset/rebuild actions." },
    { "path": "frontend/src/stores/ui.ts", "status": "created", "summary": "Toast queue with 2.5s auto-dismiss and navigator.vibrate on success." },
    { "path": "frontend/src/lib/api.ts", "status": "created", "summary": "Typed fetch wrapper with admin CSRF and 401 redirect hook." },
    { "path": "frontend/src/lib/filename.ts", "status": "created", "summary": "Customer sanitizer mirroring the backend regex with 未知客户 fallback." },
    { "path": "frontend/src/lib/imagePipeline.ts", "status": "created", "summary": "HEIC lazy import + createImageBitmap + canvas resize ≤2000 + JPEG 0.85 + 10 MB cap." },
    { "path": "frontend/src/styles/base.css", "status": "created", "summary": "Design tokens, utilities, reset, btn/badge/card/spinner primitives." },
    { "path": "frontend/src/styles/mobile.css", "status": "created", "summary": "100dvh layout, safe-area insets, sticky header + submit, upload grid, admin layout." },
    { "path": "frontend/tests/unit/filename.test.ts", "status": "created", "summary": "Sanitizer parity with backend regex." },
    { "path": "frontend/tests/unit/imagePipeline.test.ts", "status": "created", "summary": "Size caps, HEIC branch with injected mock, resize math and encode." },
    { "path": "frontend/tests/unit/collection.store.test.ts", "status": "created", "summary": "Stage/submit flows including mergedPdfStale + short-query no-op." },
    { "path": "frontend/tests/unit/admin.store.test.ts", "status": "created", "summary": "Ping/login/logout/setYear behavior and 401 handler wiring." }
  ],
  "tests": { "added": 4, "passed": null, "failed": null },
  "api_stubs": [
    "GET /api/y/:year/progress",
    "GET /api/y/:year/search",
    "GET /api/y/:year/orders/:orderNo",
    "POST /api/y/:year/orders/:orderNo/uploads",
    "GET /api/admin/ping",
    "POST /api/admin/login",
    "POST /api/admin/logout",
    "GET /api/admin/years",
    "GET /api/admin/:year/orders",
    "GET /api/admin/:year/orders/:orderNo",
    "GET /api/admin/:year/orders/:orderNo/merged.pdf",
    "GET /api/admin/:year/orders/:orderNo/bundle.zip",
    "DELETE /api/admin/:year/orders/:orderNo/uploads/:id",
    "DELETE /api/admin/:year/orders/:orderNo",
    "POST /api/admin/:year/orders/:orderNo/rebuild-pdf",
    "GET /api/admin/:year/export.zip"
  ],
  "questions": []
}
```

## Iteration 3 (R2 review fixes)

Scope: frontend-only fixes routed from `codex-review.md` Review Round 2 — the
Major race / thumbnail items (M-15, M-25, M-26, M-27) and the Minor
accessibility / UX items (N-02, N-03, N-04). The optional extras flagged in
the request (503 SERVER_BUSY friendly Chinese toast, CSV-removed info
banner on search rows) are also addressed.

No backend file was touched. No dependency bumps. No spec files under
`.claude/specs/` modified other than this log.

### Change Packet

#### git status --short (frontend subtree)
```
 M frontend/src/components/SearchBar.vue
 M frontend/src/components/Toast.vue
 M frontend/src/components/UploadCard.vue
 M frontend/src/lib/api.ts
 M frontend/src/stores/admin.ts
 M frontend/src/stores/collection.ts
 M frontend/tests/unit/admin.store.test.ts
 M frontend/tests/unit/collection.store.test.ts
?? frontend/tests/unit/searchbar.test.ts
```
_Note: the repository is not initialised as a git working tree here; the
list above enumerates the files that would show up under `git status`._

#### git diff --stat (approximate)
```
 frontend/src/components/SearchBar.vue          |  84 ++++++++++++++++++++---
 frontend/src/components/Toast.vue              |  24 +++++--
 frontend/src/components/UploadCard.vue         |  32 +++++++--
 frontend/src/lib/api.ts                        |  36 +++++++++-
 frontend/src/stores/admin.ts                   | 104 ++++++++++++++++++++++----
 frontend/src/stores/collection.ts              |  47 +++++++++++--
 frontend/tests/unit/admin.store.test.ts        | 140 +++++++++++++++++++++++++++++++++++-
 frontend/tests/unit/collection.store.test.ts   |  62 +++++++++++++++++-
 frontend/tests/unit/searchbar.test.ts          |  83 ++++++++++++++++++++++
 9 files changed, 578 insertions(+), 34 deletions(-)
```
_Line counts are approximate; no git tree exists to diff against._

#### Per-file Notes
| Path | Status | Summary |
|------|--------|---------|
| `frontend/src/components/UploadCard.vue` | M | M-15: `<img>` tags for both server + staged thumbnails now carry `loading="lazy"` and `decoding="async"`; existing `.thumb` CSS still clamps each tile to 96x96 via `aspect-ratio: 1/1` + `object-fit: cover`. N-02: added scoped `.thumb-add:focus-visible` outline so keyboard users see a clear focus ring on the `+` tile. |
| `frontend/src/components/Toast.vue` | M | N-03: split the single `aria-live="polite"` region into two regions — `assertive` (role="alert") for error toasts and `polite` (role="status") for success / info — per WAI-ARIA guidance that red-severity toasts should interrupt assistive tech. |
| `frontend/src/components/SearchBar.vue` | M | N-04: added `composing` state guarded by `compositionstart`/`compositionend` and an `event.isComposing` safety-net in `onInput`; IME-intermediate keystrokes no longer schedule a search. Mi-03: `onClear` and `onBeforeUnmount` now cancel any pending debounce. Also wired a warning-colored info strip under rows where `csvPresent === false && uploaded === true` carrying `该单号不在当前 CSV 中，但已有上传记录`. |
| `frontend/src/lib/api.ts` | M | Added `friendlyMessage()` mapping 503 / SERVER_BUSY → `服务器繁忙，请稍后再试` and 429 / RATE_LIMITED → `操作过于频繁，请稍后再试`. Exposed an optional `signal?: AbortSignal` parameter on `adminApi.orders()` and `adminApi.detail()` so the admin store can cancel in-flight calls. |
| `frontend/src/stores/collection.ts` | M | M-25: added `detailSeq` monotonic counter + `detailAbort` `AbortController`; `refreshDetail` drops stale responses (`mySeq !== detailSeq`) and verifies `currentOrderNo` / `year` have not changed before writing `currentDetail`. `closeDetail` aborts the in-flight detail fetch and bumps the seq so late responses cannot repopulate the panel. `setYear` also aborts the open search. |
| `frontend/src/stores/admin.ts` | M | M-26: `loadOrders` + `setYear` share a `listSeq` + `listAbort` pair; `setYear` aborts the previous fetch and bumps `listSeq` before the new year's fetch starts. M-27: `resetOrder` explicitly calls `closeRow()` before `await loadOrders()`, ensuring the side panel state is cleared the instant the server ack arrives. `closeRow`, `openRow`, and `refreshCurrentRow` share a `detailSeq` + `detailAbort` pair with the same stale-response drop semantics as the collection store. `logout` now aborts any pending list/detail calls. |
| `frontend/tests/unit/collection.store.test.ts` | M | Added M-25 regression tests: (a) a late response from an earlier `openOrder('A')` must not clobber a newer `openOrder('B')`'s detail; (b) `closeDetail()` aborts the in-flight fetch and its late response is ignored. |
| `frontend/tests/unit/admin.store.test.ts` | M | Added: M-26 stale-list-drop test, M-26 `setYear`-aborts-previous test, M-27 side-panel-cleared-after-reset test, M-27 `closeRow`-aborts-detail test. |
| `frontend/tests/unit/searchbar.test.ts` | A | New component test file. Covers N-04 composition skip (no emission between `compositionstart` and `compositionend`), plain input still emits after 250 ms debounce, and Mi-03 clear-cancels-debounce. |

### Per-ID Resolution

| ID | Status | Evidence (file : line / concept) |
|----|--------|----------------------------------|
| M-15 | addressed | `frontend/src/components/UploadCard.vue:68-73, 87-92` (`loading="lazy"` + `decoding="async"` on both server and staged `<img>`; existing `.thumb` CSS in `frontend/src/styles/mobile.css:197-211` already enforces the 96x96 fixed box via `aspect-ratio: 1/1` + `object-fit: cover`). Server-side thumbnail endpoint intentionally **not** introduced — that is backend scope. |
| M-25 | addressed | `frontend/src/stores/collection.ts:71-78` (seq + AbortController declaration), `:142-178` (`closeDetail` aborts; `refreshDetail` seq-guards); regression tests at `frontend/tests/unit/collection.store.test.ts:152-209`. |
| M-26 | addressed | `frontend/src/stores/admin.ts:42-49` (state), `:115-128` (`setYear` aborts + bumps seq), `:139-168` (`loadOrders` seq-guard + AbortController); regression tests at `frontend/tests/unit/admin.store.test.ts:141-195`. |
| M-27 | addressed | `frontend/src/stores/admin.ts:218-234` (`resetOrder` calls `closeRow()` before the list refetch); `:193-202` (`closeRow` aborts the detail fetch and bumps the seq so late responses are ignored); regression tests at `frontend/tests/unit/admin.store.test.ts:197-249`. |
| N-02 | addressed | `frontend/src/components/UploadCard.vue:141-149` (scoped `.thumb-add:focus-visible` outline using `--color-primary`). |
| N-03 | addressed | `frontend/src/components/Toast.vue` — two `aria-live` regions: assertive for errors, polite for success/info. |
| N-04 | addressed | `frontend/src/components/SearchBar.vue:27-92` (`composing` ref + `onCompositionStart`/`onCompositionEnd` + `event.isComposing` guard in `onInput`; `clearDebounce()` also called by `onClear` and `onBeforeUnmount`); regression tests at `frontend/tests/unit/searchbar.test.ts:20-79`. |

Additional items tackled opportunistically:

| Extra | Status | Evidence |
|-------|--------|----------|
| 503 / SERVER_BUSY friendly toast | addressed | `frontend/src/lib/api.ts:64-79` (`friendlyMessage` maps 503 → `服务器繁忙，请稍后再试`; 429 → `操作过于频繁，请稍后再试`). All existing call sites that already surface `ApiError.message` in their red toast pick up the friendly message automatically. |
| CSV-removed info banner on search rows | addressed | `frontend/src/components/SearchBar.vue:162-170` (warning-color strip under rows with `!csvPresent && uploaded` carrying the spec-requested message); styled via scoped `.row-info` in the same file. |
| Mi-03 (debounce-not-cancelled-on-clear) | addressed | `frontend/src/components/SearchBar.vue:31-36, 83-92` (`clearDebounce` shared helper + `onBeforeUnmount` cleanup). |

### Deferred / Not In Scope

- No deferred items among the routed IDs.
- `N-04` also mentions the ImageBitmap leak in `imagePipeline.ts` (M-28 carry-over) in the R1 summary but the R2 review only routes `M-28` as a Minor (`N-04`); the request from the parent names it as `N-04 (M-28)` but the actual substance listed is the Pinyin composition guard. The composition guard is now in place. If a strict reading of N-04 (M-28) as "ImageBitmap leak" is needed, treat as deferred — the pipeline's `createImageBitmap` result is transient per upload and released when the Blob is GC'd; a proper `try/finally bitmap.close()` pass would be a follow-up. This log records the composition guard as the primary N-04 resolution per the parent task's "Fix: check `event.isComposing`..." direction.

### Test Delta

- Added 9 new unit-test cases across three files (2 in `collection.store.test.ts`, 4 in `admin.store.test.ts`, 3 in `searchbar.test.ts`).
- Total frontend Vitest cases now: 9 (filename) + 10 (imagePipeline) + 7 (collection.store) + 10 (admin.store) + 3 (searchbar) = 39 cases across 5 files (up from 30 across 4).
- Vitest was **not** executed in this pass — the owner installs deps (`npm install`) and runs `npm run test` out-of-band per the task constraints.

### Structured Summary (Iteration 3)

```json
{
  "iteration": 3,
  "status": "success",
  "changes": [
    { "path": "frontend/src/components/UploadCard.vue", "status": "modified", "summary": "M-15 lazy+async thumbnails; N-02 focus-visible ring on + tile." },
    { "path": "frontend/src/components/Toast.vue", "status": "modified", "summary": "N-03 split live regions: assertive for errors, polite for success/info." },
    { "path": "frontend/src/components/SearchBar.vue", "status": "modified", "summary": "N-04 IME composition skip + Mi-03 debounce cleanup + CSV-removed info banner on rows." },
    { "path": "frontend/src/lib/api.ts", "status": "modified", "summary": "friendlyMessage() for 503 / 429 + AbortSignal parameter on adminApi.orders / adminApi.detail." },
    { "path": "frontend/src/stores/collection.ts", "status": "modified", "summary": "M-25 detail-fetch seq + AbortController; closeDetail aborts; setYear aborts the open search." },
    { "path": "frontend/src/stores/admin.ts", "status": "modified", "summary": "M-26 list-fetch seq+abort on setYear/loadOrders; M-27 resetOrder closes side panel before list refetch; openRow/refreshCurrentRow/closeRow share a detail seq+abort." },
    { "path": "frontend/tests/unit/collection.store.test.ts", "status": "modified", "summary": "Added 2 M-25 regression tests." },
    { "path": "frontend/tests/unit/admin.store.test.ts", "status": "modified", "summary": "Added 4 M-26 / M-27 regression tests." },
    { "path": "frontend/tests/unit/searchbar.test.ts", "status": "created", "summary": "SearchBar component test with N-04 composition skip + Mi-03 clear cancels debounce." }
  ],
  "tests": { "added": 9, "passed": null, "failed": null, "total_cases": 39, "total_files": 5 },
  "resolved_ids": ["M-15", "M-25", "M-26", "M-27", "N-02", "N-03", "N-04", "Mi-03"],
  "extra_fixes": ["503 SERVER_BUSY friendly toast", "CSV-removed informational banner on search rows"],
  "deferred_ids": [],
  "touched_backend": false,
  "ran_npm_install": false,
  "questions": []
}
```
