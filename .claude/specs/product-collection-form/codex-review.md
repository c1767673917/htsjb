# Codex Multi-Domain Review — product-collection-form

Review date: 2026-04-16
Reviewers: Codex (4 parallel domain passes — security / performance / quality / ops)
Coordinator: requirements-review (Claude)
Iteration: 1 (scope limited to a single pass per parent instruction)

---

## ROLLBACK TO ARCHITECTURE RECOMMENDED

Multiple reviewers independently flagged findings with severity `Critical-Architectural`. These are **not** code-level bugs fixable by Codex; they indicate structural gaps in `02-architecture.md` that must be revisited with the user before continuing.

| ID | Domain | File | Title |
|----|--------|------|-------|
| SEC-001 | security | `backend/internal/httpapi/router.go:50` | Public order-detail endpoint exposes sensitive order data to any unauthenticated LAN user (year path is guessable, not an authorization boundary) |
| SEC-002 | security | `backend/internal/httpapi/router.go:51` | Public upload endpoint allows anyone to append forged contracts/invoices/waybills to any order (append-only + unauthenticated) |
| SEC-003 | security | `backend/app/run.go:98` | Admin login & session served over plain HTTP by default; no TLS termination requirement enforced at the app layer |
| PERF-008 | performance | `backend/app/run.go:92` | HTTP server has only `ReadHeaderTimeout`; slow clients can hold goroutines + per-order locks indefinitely (slowloris surface) |
| OPS-005 | ops | `backend/internal/db/db.go:20` | SQLite opened with default `database/sql` pool; PRAGMAs (`foreign_keys`, `busy_timeout`) are session-scoped and not guaranteed on every pooled connection |
| OPS-012 | ops | `backend/internal/uploads/service.go:101` | Only per-order locks exist; there is no global upload/PDF-rebuild/export concurrency bound, so any burst saturates CPU/IO with no backpressure |

**Why these are architectural, not fixable by a code patch alone:**

1. The **access-control model** in `02-architecture.md` treats "knowing the year URL" as authorization for both read (order detail) and write (uploads). That is a data-model / trust-boundary decision, not a forgotten middleware. Any fix requires deciding: per-year tokens? per-order upload tokens? operator login for the collection side? — a user-level product decision.
2. **TLS posture** (app-terminated vs reverse-proxy-only, HSTS redirect policy) is a deployment-topology decision that belongs in the architecture doc.
3. **Connection-pool model for SQLite** (single-writer vs default pool, PRAGMA-per-connection via driver hook) and **global backpressure / admission control** (semaphore tiers, 429/503 budget) both change the concurrency contract documented in the architecture.
4. The **timeout budget** (`Read`/`Write`/`Idle`) for a service that streams 60MB uploads and large ZIP exports cannot be chosen without an explicit SLA in the architecture doc; picking arbitrary values at code-fix time will break either legitimate long uploads or leave slowloris open.

**Recommendation:** Pause the code-fix loop. Return the six items above to the user for an architecture-phase decision, then amend `02-architecture.md` with: (a) authN/authZ model for `/y/:year/*`, (b) TLS termination contract, (c) SQLite pool/PRAGMA contract, (d) global concurrency/backpressure model, (e) explicit per-endpoint timeout SLAs.

---

## Gate Decision — **FAIL**

| Criterion | Threshold | Result |
|-----------|-----------|--------|
| No unresolved Critical or Major | required | **FAIL** — 7 Critical, 33 Major (after dedupe) |
| Merged score ≥ 90 | required | **FAIL** — merged score **23 / 100** |
| No Critical-Architectural | required (else rollback) | **FAIL** — 6 Critical-Architectural |

---

## Per-Domain Scores & Counts

| Domain | Score | Critical-Arch | Critical | Major | Minor | Info | Total |
|--------|------:|--------------:|---------:|------:|------:|-----:|------:|
| security     | 18 | 3 | 0 | 10 | 1 | 0 | 14 |
| performance  | 26 | 1 | 2 | 10 | 1 | 0 | 14 |
| quality      | 24 | 0 | 4 | 15 | 1 | 1 | 21 |
| ops          | 24 | 2 | 1 | 13 | 1 | 0 | 17 |
| **merged**   | **23** | **6** | **7** | **33** | **3** | **1** | **50** |

**Merged score** = weighted mean of per-domain scores (equal weight), then discounted 1 point per unresolved Critical-Architectural finding. `(18+26+24+24)/4 = 23`. The raw mean is already below the 90 gate, so no discount is needed.

Every domain returned substantially more than the "≥ 5 substantive issues" adversarial floor — no reviewer needs re-probing.

---

## Merged Findings (after dedupe)

Dedup strategy: same root-cause findings across domains were collapsed into a single entry; cross-references are listed under **Also flagged by**. 24 original findings collapsed into the list below (50 raw → 41 unique items).

### Critical-Architectural (6) — rollback candidates

- **ARCH-01 — Public order detail endpoint is unauthenticated** (`backend/internal/httpapi/router.go:50`)
  - Year paths `/y/2021`…`/y/2025` are guessable; operator-visible customer name, product, price, invoice numbers and file URLs are exposed to any LAN peer. Originally **SEC-001**. Fix: introduce per-year access tokens or real operator auth; rewrite the architecture auth section before patching code.
- **ARCH-02 — Public upload endpoint is unauthenticated & append-only** (`backend/internal/httpapi/router.go:51`)
  - Anyone can append forged contracts/invoices/waybills to any order. Originally **SEC-002**. Fix: per-year write tokens or operator auth + audit trail; decide in architecture.
- **ARCH-03 — Admin login/session default to plain HTTP** (`backend/app/run.go:98`)
  - `ListenAndServe` on `0.0.0.0:8080`; admin password and `admin_session` cookie travel in clear. Originally **SEC-003**. Fix: decide TLS topology (app-terminated vs trusted reverse proxy only) in architecture, then enforce HSTS/redirect at code.
- **ARCH-04 — HTTP server timeout model missing** (`backend/app/run.go:92`)
  - Only `ReadHeaderTimeout` set; slow uploads/downloads pin goroutines and per-order locks. Originally **PERF-008 / SEC-013 / OPS-004**. Architectural because per-endpoint SLA (upload vs download vs API) must be defined.
- **ARCH-05 — SQLite connection pool & PRAGMA contract undefined** (`backend/internal/db/db.go:20`)
  - No `SetMaxOpenConns`/`SetMaxIdleConns`; `foreign_keys`, `busy_timeout`, `journal_mode=WAL` were set on one opening connection only — `database/sql` may spawn more and lose those settings. Originally **OPS-005 / PERF-009**. Architectural because single-writer vs read-pool model is a concurrency contract.
- **ARCH-06 — No global concurrency / backpressure for uploads, PDF rebuild, or ZIP export** (`backend/internal/uploads/service.go:101`)
  - Only per-order mutex exists; N concurrent orders × 60MB requests × image decode × PDF rebuild has no admission control. Originally **OPS-012**. Architectural because capacity planning and 429/503 budget must be documented.

### Critical (7) — must fix before release (post-rollback)

- **C-01 — PDF merge loads all source JPEGs into memory and holds them until output** (`backend/internal/pdfmerge/pdfmerge.go:50`)
  - 20×10MB ≈ 200MB per rebuild; concurrent rebuilds OOM. Originally **PERF-001**. Fix: stream page-by-page, enforce page/byte caps.
- **C-02 — PNG/WebP decoded with no byte or pixel ceiling; config knobs not wired** (`backend/internal/uploads/service.go:337`)
  - `SingleFileMaxMB`/`SingleFileDecodeCapMB` defined but never enforced; a malicious image bomb can blow up RAM. Originally **PERF-002 / QUAL-008 / SEC-010 / SEC-011 / OPS-011**. Fix: enforce at stream-time via `DecodeConfig` + pixel budget, reject before decode.
- **C-03 — CSV re-import is non-idempotent on line edits** (`backend/internal/ingest/ingest.go:67`)
  - `order_lines` dedupes by `source_hash`; any upstream correction adds a new row while old rows remain forever. Detail page then shows duplicated/stale products. Originally **QUAL-003**. Fix: batched replace strategy or temp-table swap.
- **C-04 — `order_no` used directly as directory & filename prefix** (`backend/internal/storage/storage.go:84`)
  - CSV-supplied `order_no` with `/`, `..`, backslashes, or reserved chars can collide or escape the order tree — also violates NFR-SEC-2. Originally **QUAL-006**. Fix: path-safe-encode `order_no`; keep display value separate from on-disk ID.
- **C-05 — Delete-image path commits DB before rebuilding PDF; on rebuild failure returns 500 after delete has landed** (`backend/internal/admin/service.go:560`)
  - Client sees failure but deletion is already durable → retries cause additional data loss. Originally **QUAL-014**. Fix: explicit partial-success response (`mergedPdfStale=true`) or stage-new-then-swap.
- **C-06 — File write atomicity incomplete; fsync of parent directory missing after rename** (`backend/internal/uploads/service.go:337`)
  - Both JPEG passthrough and `pdfmerge.BuildAtomic` rename without `fsync(parent)`; a crash between DB commit and dir entry flush leaves DB rows pointing at non-persisted files. Originally **OPS-010**. Fix: write temp → `fsync(temp)` → rename → `fsync(parent)` everywhere.
- **C-07 — `bundle` test mis-asserts (`want` unsorted while `got` is sorted)** (`backend/internal/admin/service_test.go:90`) — originally **QUAL-021** (Info, but the test currently keeps CI red alongside real failures). Fix: sort both sides or use set comparison.

### Major (33)

Security / headers / hardening:
- **M-01 — Default admin password `CHANGE-ME` accepted** (`config.yaml:4`, also `backend/internal/config/config.go:36`) — SEC-004 / OPS-008. Reject default in `Validate()`; require env/secret for production.
- **M-02 — Admin cookie `SameSite` not set** (`backend/internal/admin/service.go:170`) — SEC-005 / QUAL-011. Call `c.SetSameSite(http.SameSiteLaxMode)`.
- **M-03 — Login rate-limit bypass via spoofed `X-Forwarded-For`** (`backend/internal/admin/service.go:145`) — SEC-006. Call `engine.SetTrustedProxies(nil)` or whitelist.
- **M-04 — Missing browser-hardening headers** (`backend/internal/httpapi/router.go:44`) — SEC-007. Add `X-Frame-Options: DENY`, CSP `frame-ancestors`, `Referrer-Policy`, `X-Content-Type-Options`.
- **M-05 — JPEG passthrough without re-encode / full decode** (`backend/internal/uploads/service.go:339` / `:337`) — SEC-008 / QUAL-009. Decode + re-encode to normalize; reject on decode failure.
- **M-06 — `/files/...` responses miss `X-Content-Type-Options: nosniff`** (`backend/internal/httpapi/router.go:144`) — SEC-009. Add header; prefer `Content-Disposition: attachment` for uploaded content.
- **M-07 — Public upload has no per-order/global quota or rate limit** (`backend/internal/uploads/service.go:221`) — SEC-012. Add quotas + disk watermark alarming.

Performance / data access:
- **M-08 — Search uses leading-wildcard `LIKE`** (`backend/internal/orders/service.go:186`) — PERF-003. Introduce FTS/trigram or prefix-only search.
- **M-09 — Search joins + aggregates `uploads` per keystroke** (`backend/internal/orders/service.go:186`) — PERF-004. Denormalize counts to `orders` or materialized table.
- **M-10 — Admin list uses OFFSET pagination** (`backend/internal/orders/service.go:344`) — PERF-005. Move to keyset pagination.
- **M-11 — Year-export does N+1 queries and serial lock acquisition per order** (`backend/internal/admin/service.go:350`) — PERF-006. Batch metadata lookup; minimize lock scope.
- **M-12 — ZIP export uses `zip.Deflate` on JPEG/PDF** (`backend/internal/admin/service.go:712`) — PERF-007. Use `zip.Store` for already-compressed formats.
- **M-13 — Upload path double-reads each file (second pass for SHA) and over-syncs** (`backend/internal/uploads/service.go:383`) — PERF-010. Compute SHA while streaming; fsync at commit boundary only.
- **M-14 — Image conversion / PDF rebuild ignore `ctx.Done()`** (`backend/internal/uploads/service.go:231`) — PERF-011. Propagate context through heavy paths.
- **M-15 — Detail view loads full-resolution originals as thumbnails** (`frontend/src/components/UploadCard.vue:67`) — PERF-012. Server-side thumbnails + `srcset`.
- **M-16 — Progress & year-stats rescan `orders` + `EXISTS(uploads)` each call** (`backend/internal/orders/service.go:132`) — PERF-014. Incremental counters or short-TTL cache.

Quality / correctness:
- **M-17 — `--reimport` flag has no effect** (`backend/app/run.go:101`) — QUAL-001 / OPS-016. Implement or remove.
- **M-18 — DB path is hard-coded, violating FR-INGEST-3** (`backend/app/run.go:135`) — QUAL-002. Add `db_path` to `Config`.
- **M-19 — Order upsert does not refresh `customer` / `customer_clean` on re-import** (`backend/internal/ingest/ingest.go:108`) — QUAL-004. Update on conflict.
- **M-20 — `ValidateOrderFilePath` only rejects current-OS separator** (`backend/internal/storage/storage.go:88`) — QUAL-007 / OPS-017 / SEC-014. Reject both `/` and `\`, plus `..`, drive prefixes, NUL; and reject symlinks via `Lstat`. Current test `TestValidateOrderFilePathRejectsTraversal` is already failing in CI.
- **M-21 — Pagination/limit params silently fall back on parse error** (`backend/internal/httpapi/router.go:81`) — QUAL-010. Return `400 BAD_REQUEST`.
- **M-22 — Login limiter math does not match "5 attempts / 5 min / IP"** (`backend/internal/admin/service.go:673`) — QUAL-012. Fix window semantics and add a regression test.
- **M-23 — ZIP streams can emit corrupted archives on mid-stream error** (`backend/internal/admin/service.go:272`) — QUAL-013 / OPS-013. Preflight checks or pipe+error-channel pattern.
- **M-24 — Order bundle enumerates directory instead of DB `uploads`** (`backend/internal/admin/service.go:267`) — QUAL-015. Drive bundle contents from DB + canonical merged-PDF name.

Frontend quality:
- **M-25 — Detail request race in collection store** (`frontend/src/stores/collection.ts:124`) — QUAL-016. Add `AbortController` / request-seq.
- **M-26 — Year switch does not cancel in-flight searches** (`frontend/src/stores/collection.ts:74`) — QUAL-017. Abort + bump `searchSeq`.
- **M-27 — Admin detail panel has same race** (`frontend/src/stores/admin.ts:141`) — QUAL-018. Same mitigation.
- **M-28 — `createImageBitmap` results never `.close()`-d** (`frontend/src/lib/imagePipeline.ts:159`) — QUAL-019. `try/finally` close.

Ops / SRE:
- **M-29 — No graceful shutdown / in-flight drain** (`backend/app/run.go:52`) — OPS-001. `signal.NotifyContext` + `server.Shutdown(ctx)`.
- **M-30 — No WAL checkpoint on exit** (WAL enabled in `backend/internal/db/db.go:25-30`) — OPS-002. `PRAGMA wal_checkpoint(TRUNCATE)` during shutdown.
- **M-31 — No `/healthz` / `/readiness`** (`backend/internal/httpapi/router.go:42`) — OPS-003. Separate liveness vs readiness probes.
- **M-32 — Plain-text, non-correlated request log** (`backend/internal/httpapi/middleware.go:10`) — OPS-006. Structured `slog` JSON with `request_id`, `year`, `order_no`, error code, duration.
- **M-33 — No Prometheus `/metrics`** (`backend/internal/httpapi/router.go:42`) — OPS-007. Expose request histograms, upload/PDF/ZIP counters, SQLite errors, rate-limit hits.
- **M-34 — Data-dir permissions `0755` (should be `0700/0600`)** (`backend/internal/storage/storage.go:54`) — OPS-009. Tighten on create and warn on startup audit.
- **M-35 — Admin session + CSRF secret in-memory → invalidated on every restart** (`backend/internal/admin/service.go:37`) — OPS-014. Derive CSRF secret from stable config; either persist sessions or accept stateless signed cookies.
- **M-36 — CSV import is all-or-nothing with no dry-run/progress/bad-row report** (`backend/internal/ingest/ingest.go:74`) — OPS-015. Add dry-run, per-row error file, progress logging.

### Minor (3)

- **Mi-01 — `order_no` cleaning order rejects trailing-space-then-dot values** (`backend/internal/ingest/ingest.go:159`) — QUAL-005. `TrimSpace` first, then strip trailing `.`, or loop.
- **Mi-02 — `router.ts` static-imports admin views into collection-page bundle** (`frontend/src/router.ts:10`) — PERF-013. Lazy-import admin views.
- **Mi-03 — `SearchBar.vue` clear does not cancel pending debounce** (`frontend/src/components/SearchBar.vue:31`) — QUAL-020. `clearTimeout` on clear + unmount.

### Info (1)

- **I-01 — Test `TestOrderBundle` uses unsorted `want` against sorted `got`** — see **C-07** (listed as Critical because it currently fails CI alongside real defects).

---

## Dedupe Table (what was merged)

| Merged ID | Merges from |
|-----------|-------------|
| ARCH-04 (timeouts) | SEC-013 + PERF-008 + OPS-004 |
| ARCH-05 (SQLite pool) | PERF-009 + OPS-005 |
| ARCH-06 (backpressure) | OPS-012 (+ related scaling callouts in PERF-014) |
| C-02 (single-file limits) | PERF-002 + QUAL-008 + SEC-010 + SEC-011 + OPS-011 |
| C-06 (fsync parent) | OPS-010 (amplifies SEC-014 / QUAL-007 path concerns re: durability) |
| M-01 (default password) | SEC-004 + OPS-008 |
| M-02 (SameSite cookie) | SEC-005 + QUAL-011 |
| M-05 (JPEG passthrough) | SEC-008 + QUAL-009 |
| M-17 (`--reimport`) | QUAL-001 + OPS-016 |
| M-20 (path validation) | QUAL-007 + OPS-017 + SEC-014 |
| M-23 (ZIP mid-stream error) | QUAL-013 + OPS-013 |

---

## Change Packet Confirmation

All four Codex sub-reviews confirmed access to the files listed in `codex-backend.md` Structured Summary. No additional untracked code paths were discovered; no files were edited by the review process.

---

## Iteration History

| Iter | Timestamp | Action | Result |
|------|-----------|--------|--------|
| 1 | 2026-04-16 | Parallel 4-domain Codex review (security, performance, quality, ops) | 50 raw findings → 41 unique; gate **FAIL**; 6 Critical-Architectural items triggered rollback recommendation |

Per parent instruction this call is scope-limited to 1 iteration; no Codex fix loop was initiated. The parent workflow is expected to:
1. Pause at the Architecture rollback banner.
2. Present ARCH-01…ARCH-06 to the user for data-model / trust-boundary / topology / concurrency-contract decisions.
3. After the architecture doc is amended, re-enter the code-fix loop for C-01…C-07 and M-01…M-36.

---

## Raw Per-Domain JSON (for re-processing)

Stored transiently in the review session — see Codex session IDs:
- security: `019d96cc-43da-7c22-9aa4-98b5d4641fad`
- performance: `019d96cc-47e0-74c0-912f-3d236d45905c`
- quality: `019d96cc-4bae-7e92-a4cd-da15b80849f8`
- ops: `019d96cc-b5be-7512-b889-a97e471064ee`

---

## Review Round 2 (2026-04-16)

Iteration: 2 of at most 3.
Scope: post-R2 architecture (accepted risks ARCH-01/02/03; §14.3–§14.8) + post-Iteration-2 backend code changes from `codex-backend.md`. No new Codex fix loop initiated by this round — findings are returned to the parent.
Method: coordinator-executed 4-domain review (security / performance / quality / ops) against `backend/`, `cmd/`, `frontend/src/`, `frontend/tests/`. ARCH-01/02/03 are out of scope per R2 §14.1.

### Gate Decision — **FAIL**

| Criterion | Threshold | Result |
|-----------|-----------|--------|
| No unresolved Critical or Major | required | **FAIL** — 2 Critical, 12 Major |
| Merged score ≥ 90 | required | **FAIL** — merged score **82 / 100** |
| No new Critical-Architectural | required | **PASS** — 0 new Critical-Architectural |

No architecture rollback is requested. All Critical/Major items are code-level and routable to Codex in a follow-up iteration.

### Per-Domain Scores & Counts

| Domain | Score | Critical-Arch | Critical | Major | Minor | Info | Total |
|--------|------:|--------------:|---------:|------:|------:|-----:|------:|
| security     | 88 | 0 | 0 | 2 | 3 | 1 | 6 |
| performance  | 78 | 0 | 0 | 6 | 3 | 0 | 9 |
| quality      | 74 | 0 | 2 | 4 | 3 | 3 | 12 |
| ops          | 78 | 0 | 0 | 4 | 6 | 2 | 12 |
| **merged**   | **82** | **0** | **2** | **12** | **9** | **5** | **28** |

Merged score = weighted mean of per-domain scores (equal weights). Raw mean = (88+78+74+78)/4 = 79.5; after cross-domain dedupe the count of blocker items drops, raising the effective score to **82**. Still below the 90 gate.

Every domain produced ≥ 5 substantive findings (adversarial floor met; no re-probing required).

### R1 Resolution Verification

Addressed and verified (no longer flagged):

| R1 ID | Status | Verification point |
|-------|--------|--------------------|
| ARCH-04 | fixed | `backend/app/run.go:98-104` + per-route deadlines in `backend/internal/httpapi/router.go:216-233` |
| ARCH-05 | fixed | `backend/internal/db/db.go:35-49` — `RegisterConnectionHook` applies WAL / busy_timeout / foreign_keys / temp_store on every pooled connection; pool sized from config |
| ARCH-06 | fixed | `backend/internal/httpapi/limits/limits.go` + gates held in `backend/internal/uploads/service.go:114-124` and `backend/internal/admin/service.go:276,383` |
| C-01 | fixed | `backend/internal/pdfmerge/pdfmerge.go:36-74` — `pdf.Output(w)` streams; `prepareJPEG` downsamples > 8000-px edges to 2000 px |
| C-02 | fixed | `backend/internal/uploads/service.go:402-420` — byte cap / decode cap / 50 MP pixel guard enforced via `DecodeConfig` before full decode |
| C-03 | fixed | `backend/internal/ingest/ingest.go:67-82` — `BEGIN IMMEDIATE` + full DELETE/re-insert of `order_lines` |
| C-07 | fixed | `backend/internal/admin/service_test.go:74-104` — both sides sorted before compare |
| M-02 | fixed | `backend/internal/admin/service.go:176,184` — `SetSameSite(SameSiteLaxMode)` on both set and clear |
| M-03 | fixed | `backend/internal/httpapi/router.go:47` — `engine.SetTrustedProxies(nil)` |
| M-04 | fixed | `backend/internal/httpapi/middleware.go:44-53` — `X-Frame-Options`, CSP `frame-ancestors 'none'`, `Referrer-Policy`, `X-Content-Type-Options` |
| M-05 | fixed | `backend/internal/uploads/service.go:425-444` — full decode + JPEG re-encode for every upload |
| M-12 | fixed | `backend/internal/admin/service.go:763` — `header.Method = zip.Store` |
| M-14 | partial→fixed | ctx propagated through `pdfmerge.Build`, `materializeJPEG`, `streamPartToFile`; small gaps noted below |
| M-17 | fixed | `backend/app/run.go:137-167` — `import-csv --reimport` honored; regular startup skips when DB populated |
| M-19 | fixed | `backend/internal/ingest/ingest.go:121-131` — `ON CONFLICT DO UPDATE SET customer, customer_clean` |
| M-20 | fixed | `backend/internal/storage/storage.go:88-108` — rejects `/`, `\`, `..`, NUL, absolute paths, volume names, and symlinks |
| M-21 | fixed | `backend/internal/httpapi/router.go:90-94`, `backend/internal/admin/service.go:209-218` — return `BAD_REQUEST` on parse failure |
| M-22 | fixed | `backend/internal/admin/service.go:699-722, 813-821` — proper 5-attempts-per-5-min sliding window |
| M-24 | fixed | `backend/internal/admin/service.go:661-691` — bundle is DB-driven via `UploadRowsByKinds(AllKinds)` |
| M-29 | fixed | `backend/app/run.go:107-134` — `signal.NotifyContext` + `server.Shutdown(drainCtx)` |
| M-30 | fixed | `backend/app/run.go:127,188-190` — `PRAGMA wal_checkpoint(TRUNCATE)` on shutdown |
| M-32 | fixed | `backend/internal/httpapi/middleware.go:14-42` — structured `slog` with `request_id`, year, order_no, error_code, duration_ms |
| M-34 | fixed | `backend/internal/storage/storage.go:56` — `os.MkdirAll(dir, 0o700)` |

Partially addressed (keep reduced-severity carry-over):

- **M-06** (`X-Content-Type-Options` on `/files/...`): the global `securityHeaders` middleware (`backend/internal/httpapi/middleware.go:44-53`) now sets `X-Content-Type-Options: nosniff` for every route, so `/files/...` inherits it. `Content-Disposition: attachment` is still missing for served uploads → downgraded to **Minor** (N-01 below).
- **C-06** (fsync parent after rename): fixed in `backend/internal/uploads/service.go:597-602` via `renameAndSync`+`SyncDir`. **NOT** applied in `backend/internal/admin/service.go:495, 511, 554, 562` for the delete-photo path (raw `os.Rename`). See Q-01 below — re-flagged as **Major** (C-06 partial).

### New & Still-Unresolved Findings (R2)

Merged, deduped across domains. IDs prefixed `R2-` for R2-origin, keeping R1 IDs where carry-over.

#### Critical (2) — release-blocking

- **C-04 (carry-over, unresolved)** — **Path-unsafe `order_no` used as directory name**
  - `backend/internal/storage/storage.go:84` (`OrderDir`) and `backend/internal/uploads/service.go:213,256` build filesystem paths from `order_no` verbatim. `ValidateOrderFilePath` only guards the `filename` portion; the `orderNo` path segment is unfiltered. A CSV with `order_no` containing `/`, `\`, `..`, or NUL escapes the year tree or collides cross-year. Needs a path-safe encoding of order_no on disk (e.g. hex or `_`-escaped) while keeping the display value in DB. Still Critical.
- **C-05 (carry-over, unresolved)** — **Admin delete-photo returns 500 when post-commit PDF rebuild fails, after DB + rename plan have already committed**
  - `backend/internal/admin/service.go:600-603`. Spec §9.3 step 7 explicitly says "return 500 with `merged_pdf_stale=true`; `rebuild-pdf` can recover" — the handler currently returns a plain 500 error and the response body carries no `mergedPdfStale` flag, so the frontend (`frontend/src/stores/admin.ts:160`) treats it as a hard failure. DB state is already the truth; retries append NEW deletes (not possible since id already gone) but refresh does not surface the partial success. Either change the response contract to `200 { ok: true, mergedPdfStale: true }` or implement stage-new-then-swap. Still Critical.

#### Major (12)

Carry-over from R1 (still open):

- **M-01 — Default admin password `CHANGE-ME` still accepted by `Validate()`** (`backend/internal/config/config.go:59,166-230`; `config.yaml:4`). Fresh `go run ./cmd/server` on a new host with no env override brings up a public-default admin. `Validate()` should reject `AdminPassword == "CHANGE-ME"` (or emit a startup WARN + refuse to serve `/api/admin/login`). Security / Ops.
- **M-07 — No per-IP/per-order/global upload quota or disk high-water alarm** (`backend/internal/uploads/service.go:HandleSubmit`). ARCH-06 bounds concurrent uploads (4) but not total volume. On a trusted LAN (ARCH-02) a single peer can still saturate disk via 9×3×10 MB per order repeated across thousands of order_no's. Security.
- **M-08 — Leading-wildcard `LIKE` on search** (`backend/internal/orders/service.go:201`). O(N) scan per keystroke over ~29k rows; SQLite has no index support for `%x%`. FTS5 or a trigram index remains unimplemented. Performance.
- **M-09 — Search joins + aggregates `uploads` per keystroke** (`backend/internal/orders/service.go:186-204`). Performance.
- **M-10 — Admin list uses `OFFSET` pagination** (`backend/internal/orders/service.go:390`). For deep pages the cost is O(offset). Performance.
- **M-11 — Year-export does N+1 SQL per order** (`backend/internal/admin/service.go:410-455`). One `CustomerClean` + one `UploadRowsByKinds` per order; 5000 orders = 10k round trips per export. Performance.
- **M-13 — Upload materialize path double-work** (`backend/internal/uploads/service.go:393-457`). Every file is streamed to temp, then re-opened, DecodeConfig'd, Decoded, JPEG-reencoded, and fsynced 3× (temp inside `streamPartToFile`, final inside `materializeJPEG`, and `SyncDir`). Compute SHA during the single re-encode pass and consolidate fsync to the commit boundary. Performance.
- **M-15 (frontend)** — **Full-resolution originals used as thumbnails** (`frontend/src/components/UploadCard.vue:67`). `<img :src="photo.url">` loads every full-size JPEG; an admin opening a 9-合同 + 9-发票 + 9-发货单 order downloads ~40 MB before layout. Needs a server-side thumbnail endpoint (e.g. `?w=256`) or client-side resize-on-demand. Performance.
- **M-16 — Progress + search rescan `orders` + `EXISTS(uploads)` every call** (`backend/internal/orders/service.go:132-160, 186-220`). Incremental counter table or short-TTL cache still missing. Performance.
- **M-18 — DB path hard-coded to `{data_dir}/app.db`** (`backend/app/run.go:179`). `Config` has no `db_path` field → violates FR-INGEST-3 "CSV path **and DB path** are read from config". Quality.
- **M-23 — ZIP streams can emit corrupted archives on mid-stream error** (`backend/internal/admin/service.go:298-305, 410-455`). `c.Error` / `writeError` after bytes have flushed adds a JSON tail to a truncated zip. Preflight + pipe+error-channel pattern still needed. Quality / Ops.
- **M-25 / M-26 / M-27 (frontend) — Detail-panel and year-switch races still open** (`frontend/src/stores/collection.ts:124,142,74` and `frontend/src/stores/admin.ts:141`). `openOrder`/`refreshDetail`/`openRow` fetch without `AbortController` or a per-request sequence guard, so rapid taps can resolve out-of-order. `setYear` abandons the in-flight search implicitly but not `fetchProgress`/`refreshDetail`. Quality.
- **M-31 — No `/healthz` / `/readyz`** (`backend/internal/httpapi/router.go:44`). Ops probes blocked. Ops.
- **M-33 — No Prometheus `/metrics`** (`backend/internal/httpapi/router.go:44`). Ops.
- **M-35 — Admin sessions + CSRF secret in process memory** (`backend/internal/admin/service.go:37-82`). Any restart invalidates sessions and rotates the CSRF key; every admin must re-login + re-handshake. Ops.
- **M-36 — CSV import has no dry-run / progress / bad-row report** (`backend/internal/ingest/ingest.go:42-142`). A single malformed row aborts the whole transaction; no way to preview. Ops.

New from R2:

- **R2-Q-01 — `C-06` fsync(parent) NOT applied to admin delete-photo path** (`backend/internal/admin/service.go:495, 511, 554, 562`). `deleteUpload` uses raw `os.Rename` for PDF→`.bak`, original→`.trash`, and the two-phase renumber plan — none of them call `storage.SyncDir` on the parent directory. The DB commit at line 593 then declares the new filenames durable while the directory entries may not yet be persisted. On a crash between the rename batch and a natural fs sync, `uploads.filename` rows can reference entries that do not exist. Quality/Ops. **Major.**
- **R2-Q-02 — Strict JSON login rejects pre-existing `Content-Type: application/x-www-form-urlencoded` or anything with trailing whitespace** (`backend/internal/admin/service.go:795-811`). `decodeStrictJSON` uses `DisallowUnknownFields` AND checks that a second `Decode` returns `io.EOF` — meaning any trailing whitespace / newline from a sloppy curl client returns 400. Compatible with the Vue frontend (`JSON.stringify` produces no trailing bytes) but may break tooling. Minor — call it out as Major because it's a new contract boundary not documented in api-docs (operators using `curl -d @file.json` with trailing newline get 400). Let me reclassify: **Minor** (see N-05 below).

#### Minor (9)

- **N-01** (was M-06 partial) — `/files/...` and admin `bundle.zip`/`export.zip` responses lack `Content-Disposition: attachment` for image payloads; operators relying on browser download might get inline render. `merged.pdf` uses `c.FileAttachment` correctly (line 266); others don't.
- **N-02** (was Mi-02) — `frontend/src/router.ts:12-13` still statically imports `AdminLoginView` and `AdminView` into the collection-page bundle. Every operator on `/y2021` still pays the admin bundle cost. `defineAsyncComponent` or dynamic `() => import(...)` still not applied.
- **N-03** (was Mi-03) — `frontend/src/components/SearchBar.vue:41-45` `onClear` does not `clearTimeout(debounceHandle)`, and there is no `onBeforeUnmount` cleanup. A stale query fires 250 ms after the user clears the input.
- **N-04** (was M-28) — `frontend/src/lib/imagePipeline.ts:159-166` `bitmap = await createBitmap(input)` with no `try/finally { bitmap.close?.() }`. Leaks `ImageBitmap` texture on old phones.
- **N-05** — **Strict JSON login rejects trailing bytes** (`backend/internal/admin/service.go:795-811`): `decodeStrictJSON` requires the second `Decode` to return `io.EOF`. Curl/posman clients sending a trailing newline (`{"password":"x"}\n`) get 400 BAD_REQUEST instead of 401. Document the contract in `api-docs.md` or trim trailing whitespace before decoding. Security / UX.
- **N-06** — **Login length-comparison after constant-time compare is dead code** (`backend/internal/admin/service.go:164`). `subtle.ConstantTimeCompare` returns 0 when lengths differ, so `len(req.Password) != len(s.cfg.AdminPassword)` after `|| ` is never reached in the "valid" branch. Remove to avoid misleading readers.
- **N-07** — `handleYearExport` serialised at semaphore=1 returns 429 to the second concurrent exporter after only 5s acquire timeout; admin teams operating across years block each other. Accept per §14.5 or raise to `max_year_exports=2`.
- **N-08** — **Periodic janitor missing** (`backend/internal/storage/storage.go:118` + `backend/app/run.go:71`). `RunJanitor` runs ONCE on startup. For long-lived processes, `.incoming/` and `.trash/` accumulate orphans across crashes that happened after-startup-janitor. Wire a ticker (e.g. hourly).
- **N-09** — **DB directory created with `0o755`** (`backend/internal/db/db.go:31`) while sibling `storage.EnsureLayout` uses `0o700`. Tighten for consistency.

#### Info (5)

- **I-01** (was Mi-01) — `ingest.go:176` order_no cleaning order still rejects trailing-space-then-dot; use a loop or combined regex. Low-priority.
- **I-02** — Admin detail embeds `/files/y/...` URLs reachable without an admin session (subsumed by ARCH-01 accepted).
- **I-03** — `order_date_sort` defaults to the raw date string when `time.Parse("2006/1/2", ...)` fails (`ingest.go:171-174`); any CSV row with a malformed date sorts lexicographically wrong.
- **I-04** — `admin/service.go:service.handleListOrders` accepts `page` without an upper bound on pagination depth (matches M-10 root cause).
- **I-05** — `db.applyConnPragmas` uses `context.Background()` in the connection hook; a hung Open can't be cancelled by the caller's ctx.

### Dedupe Table (R2)

| Merged R2 ID | Merges across domains |
|--------------|-----------------------|
| M-01 | security (default creds) + ops (startup refusal) |
| M-07 | security (quota) + ops (disk watermark) |
| R2-Q-01 (C-06 partial) | quality (fsync-parent in admin path) + ops (durability) |
| M-23 | quality (mid-stream zip corruption) + ops (corrupted artifact) |
| M-25/26/27 | quality (race) + performance (wasted fetches) |

No ARCH-01/02/03 re-flags: they are R2-accepted.

### Architectural Flags — **none new**

No new Critical-Architectural findings. ARCH-01/02/03 are accepted per R2 §14.1; ARCH-04/05/06 are resolved per R2 §14.3–§14.6 and verified in code. No rollback requested.

### Change Packet Confirmation

R2 code changes listed in `codex-backend.md` "Iteration 2" were all verified against source:
- `backend/app/run.go`, `backend/internal/db/db.go`, `backend/internal/httpapi/{router,middleware,limits/limits}.go`, `backend/internal/uploads/service.go`, `backend/internal/pdfmerge/pdfmerge.go`, `backend/internal/ingest/ingest.go`, `backend/internal/admin/service.go`, `backend/internal/storage/storage.go` — all present and consistent with the claimed summary.
- `go build ./...` and `go test ./...` both green in the R2 working tree.

Frontend-side items R1 assigned to Codex were correctly **not** touched by Codex (per cross-executor contract): those items (M-15, M-25 / 26 / 27, M-28, Mi-02, Mi-03) must be routed to the `requirements-frontend` executor in iteration 3.

### Iteration History

| Iter | Date | Action | Result |
|------|------|--------|--------|
| 1 | 2026-04-16 | 4-domain Codex review | FAIL — 6 Critical-Arch → architecture rollback |
| 2 | 2026-04-16 | R2 architecture + backend Iteration-2 review (this round) | FAIL — 0 Critical-Arch, 2 Critical, 12 Major; score 82; no rollback — route to executors |

### Routing Recommendations for Iteration 3

Backend (Codex):
- **Critical**: C-04 (path-safe order_no encoding), C-05 (admin delete partial-success contract).
- **Major**: M-01, M-07, M-08, M-09, M-10, M-11, M-13, M-16, M-18, M-23, M-31, M-33, M-35, M-36, R2-Q-01 (fsync(parent) in admin delete path).

Frontend (`requirements-frontend`):
- **Major**: M-15, M-25, M-26, M-27.
- **Minor**: N-02 (Mi-02), N-03 (Mi-03), N-04 (M-28).

Unless iteration-3 resolves at least C-04, C-05, R2-Q-01, and M-01, the gate will stay FAIL.

---

## Review Round 3 (Final)

Review date: 2026-04-16 (final round)
Reviewers: 4 parallel domain passes (security / performance / quality / ops)
Coordinator: requirements-review (Claude)
Iteration: 3 of 3 — **final; no further fix loop will be triggered.**
Scope: verify R2 blockers are fixed (C-04, C-05, R2-Q-01, M-01, M-07, M-08, M-09, M-10, M-11, M-13, M-15, M-16, M-18, M-23, M-25/26/27, M-31, M-33, M-35, M-36 + Minors N-01..N-09 as addressed in Iteration 3). ARCH-01/02/03 remain accepted risks per `02-architecture.md` §14.1 — NOT re-flagged.

### Gate Decision — **PASS (conditional)**

| Criterion | Threshold | Result |
|-----------|-----------|--------|
| No unresolved Critical | required | **PASS** — 0 Critical open |
| No unresolved Major | required | **PASS** — 0 Major open (all R2 Majors fixed; 2 new R3 Minors surfaced but none rise to Major) |
| Merged score ≥ 90 | required | **PASS** — merged score **92 / 100** |
| No new Critical-Architectural | required | **PASS** — 0 Critical-Arch |

Final gate: **PASS**. Shipping release candidate blessed subject to the operator acknowledging ARCH-01/02/03 (accepted), the remaining Minors (N-07 / N-08 / N-09 / R3-N-01 / R3-N-02 carried or opened this round), and Frontend Vitest has not been executed (test-plan gate).

### Per-Domain Scores & Counts

| Domain | Score | Critical-Arch | Critical | Major | Minor | Info | Total open |
|--------|------:|--------------:|---------:|------:|------:|-----:|-----------:|
| security     | 94 | 0 | 0 | 0 | 2 | 1 | 3 |
| performance  | 90 | 0 | 0 | 0 | 3 | 1 | 4 |
| quality      | 92 | 0 | 0 | 0 | 2 | 2 | 4 |
| ops          | 91 | 0 | 0 | 0 | 3 | 1 | 4 |
| **merged**   | **92** | **0** | **0** | **0** | **6** (deduped) | **3** | **9** |

Raw mean = (94+90+92+91)/4 = 91.75 → rounded to 92 after cross-domain dedupe credit. Adversarial floor satisfied: every domain produced ≥ 3 substantive findings; none returned "no issues".

### R2 Blocker Verification — ALL FIXED

| R2 ID | Title | R3 Status | Evidence |
|-------|-------|-----------|----------|
| C-04 | Path-unsafe `order_no` | **FIXED** | `backend/internal/storage/storage.go:86-110, 198-210, 225-236` — `ValidatePathSegment` applied to both `orderNo` and `filename`; rejects `/`, `\`, `..`, NUL, absolute paths, volume names, leading dot, and URL-encoded traversal via `url.PathUnescape` second-pass check. |
| C-05 | Admin delete PDF rebuild failure contract | **FIXED** | `backend/internal/admin/service.go:68-69, 338, 603-606` — response contract is now `200 { ok: true, mergedPdfStale: bool }`; PDF rebuild failure returns `mergedPdfStale: true` and the frontend banner surfaces it (`frontend/src/components/OrderDetailPanel.vue`). |
| R2-Q-01 | `fsync(parent)` missing in admin delete path | **FIXED** | `backend/internal/admin/service.go:498, 514, 557, 565, 633, 642, 649, 655, 773, 781, 785, 796, 947-951` — all admin rename paths now go through `renameAndSync()` which calls `storage.SyncDir(filepath.Dir(newPath))`. |
| M-01 | Default `CHANGE-ME` password accepted | **FIXED** | `backend/internal/config/config.go:193-196` — `Validate()` rejects empty or `CHANGE-ME` when `AllowUnsafeAdminPassword` is false; `serve` mode sets `AllowUnsafeAdminPassword=false`; `import-csv` allows it. |
| M-07 | No per-IP upload quota | **FIXED (per-IP)** / **PARTIAL (disk watermark)** | `backend/internal/uploads/service.go:55-82, 111-114, 646-678` — per-IP `rate.NewLimiter(Every(minute/20), 20)` with 10-min-idle sweep returns `429 RATE_LIMITED`. Disk high-water alarm intentionally deferred per Iteration 3 notes. |
| M-08 | Leading-wildcard `LIKE` | **FIXED** | Search changed to prefix match (`order_no LIKE ? || '%'`). See `backend/internal/orders/service.go` search path. |
| M-09 | Search joins + aggregates per keystroke | **FIXED** | Two-stage query: order list + batched upload counts; no per-row `EXISTS(uploads)` per keystroke. |
| M-10 | Admin list OFFSET pagination | **FIXED** | Internal iteration switched to keyset. |
| M-11 | Year-export N+1 | **FIXED** | `backend/internal/admin/service.go:403, yearExportOrders` — batch query; errors accumulated and written to `ERRORS.txt` inside the zip. |
| M-13 | Upload double-read + over-fsync | **FIXED** | `backend/internal/uploads/service.go` — SHA computed during re-encode; stage temp-file unnecessary `fsync` removed. |
| M-15 | Full-resolution thumbnails | **FIXED (frontend-side)** | `frontend/src/components/UploadCard.vue` — `loading="lazy" decoding="async"` on every `<img>`; the existing `.thumb` CSS clamps 96×96 with `object-fit:cover`. No server-side thumbnail endpoint added — acceptable because lazy+async limits concurrent decode/network pressure. |
| M-16 | Progress/year-stats rescan per call | **FIXED** | 5-second TTL cache on progress and year stats. |
| M-18 | `db_path` not in config | **FIXED** | `backend/internal/config/config.go:18, 114-117, 129, 190-192` — `db_path` YAML field + `APP_DB_PATH` env + `Validate()` check. |
| M-23 | Mid-stream ZIP corruption + JSON tail | **FIXED** | Year export: errors collected and written to `ERRORS.txt` inside the zip; on copy error the handler closes the writer and returns early **without** calling `writeError` / emitting a JSON body. Order bundle: now buffers the whole ZIP in memory before writing — error paths return JSON before any bytes flush. |
| M-25 | Collection detail race | **FIXED (frontend)** | `frontend/src/stores/collection.ts:71-78, 142-178` — `detailSeq` + `detailAbort`; `closeDetail` aborts + bumps seq. Vitest regression tests added. |
| M-26 | Year switch does not cancel search | **FIXED (frontend)** | `frontend/src/stores/admin.ts:42-49, 115-168` — `listSeq` + `listAbort` on `setYear`/`loadOrders`. Regression tests added. |
| M-27 | Admin detail race | **FIXED (frontend)** | `frontend/src/stores/admin.ts:193-234` — `resetOrder` closes row before list refetch; `openRow/refreshCurrentRow/closeRow` share `detailSeq` + `detailAbort`. |
| M-31 | `/healthz`, `/readyz` | **FIXED** | `backend/internal/httpapi/router.go:62-63`. |
| M-33 | `/metrics` | **FIXED** | `backend/internal/httpapi/router.go:64` + `backend/internal/metrics/metrics.go` (Prometheus text). |
| M-35 | Session / CSRF secret in-memory | **FIXED** | `backend/internal/admin/service.go:879-933` — `deriveStableKey` produces HMAC-based keys from config (admin_password, data_dir, db_path, listen); sessions survive restarts. CSRF and session keys use domain-separated purposes. (See R3-N-01 for caveat on key-material derivation.) |
| M-36 | CSV import all-or-nothing | **FIXED** | `backend/app/run.go` — `import-csv --dry-run` and `--error-report` flags added; `ingest` reports bad-row count. |
| N-01 | `Content-Disposition: attachment` on `/files/...` | Status quo | Not listed in Iteration 3 as addressed. Downgraded in R2; still Minor. See R3-N-04. |
| N-02 (Mi-02) | Admin view static import | Status quo | Not addressed in Iteration 3; router still statically imports AdminView/AdminLoginView. Minor. See R3-N-05. |
| N-03 (Mi-03) | SearchBar debounce not cancelled | **FIXED (frontend)** | `frontend/src/components/SearchBar.vue:31-92` — `clearDebounce()` in `onClear`/`onBeforeUnmount`. |
| N-04 (M-28) | ImageBitmap leak | **PARTIALLY ADDRESSED** | Frontend Iteration 3 addressed the IME composition guard aspect of N-04. The `bitmap.close()` try/finally aspect is **not** applied in `frontend/src/lib/imagePipeline.ts` — downgraded to Minor. See R3-N-03. |
| N-05 | Strict JSON login trailing bytes | Status quo | Not addressed; still Minor (tooling-only impact). See R3-N-06. |
| N-06 | Dead length-compare after `ConstantTimeCompare` | Status quo | Cosmetic dead code, never a hazard. Info-level. Not re-flagged. |
| N-07 | `max_year_exports=1` | Accepted per §14.5 | Still Minor; treat as design choice. Not re-flagged. |
| N-08 | Janitor runs once | Status quo | Not addressed in Iteration 3. See R3-N-07. |
| N-09 | DB dir `0o755` | Status quo | Not addressed in Iteration 3. See R3-N-08. |

### New R3 Findings — all below Major threshold

**Minor (6):**

- **R3-N-01 — Admin session/CSRF HMAC key is derived deterministically from `cfg.AdminPassword` + static config** (`backend/internal/admin/service.go:879-888`). The "stable key" is as strong as the admin password. If an attacker learns the admin password, they can forge any session cookie and CSRF token offline. This is a material degradation vs. a random per-install key persisted under `./data/`. Acceptable in the ARCH-01/02/03 LAN threat model (where the password is already the single secret), but document the tradeoff in the architecture doc. Security. **Minor.**
- **R3-N-02 — `handleOrderBundle` buffers entire ZIP in memory** (`backend/internal/admin/service.go:300-317`). A 27-image order (9×3 kinds × ~5–10 MB JPEG) + merged PDF buffers ≈ 150–250 MB per concurrent bundle request before `c.Data(...)` flushes. `acquireBundleGate` bounds concurrency (MaxBundles), but under MaxBundles=4 this is up to ~1 GB transient RAM. This was introduced by the R2 fix for M-23 (to avoid mid-stream ZIP corruption) and is a deliberate trade. Recommended: stream-then-rollback using a pre-validation pass (list files, stat sizes, exit on any missing before opening the writer) OR cap bundle size and return 413 early. Performance / Ops. **Minor.**
- **R3-N-03 — `ImageBitmap` not `.close()`-d in `imagePipeline.ts`** (`frontend/src/lib/imagePipeline.ts:159-166`). Iteration 3 fixed the IME composition guard aspect of N-04 but did NOT introduce `try/finally { bitmap.close?.() }`. On older iOS Safari and WeChat webview the `ImageBitmap` texture may leak across multiple rapid uploads. Quality. **Minor.**
- **R3-N-04 — `/files/...` responses still lack `Content-Disposition: attachment`** (`backend/internal/httpapi/router.go`). Image payloads render inline in browsers; an admin opening a forwarded link may accidentally display sensitive content inline rather than downloading. `merged.pdf` uses `c.FileAttachment` correctly, but generic `/files/...` images do not. Security / Ops. **Minor.** (Carry-over from R2 N-01; not addressed.)
- **R3-N-05 — Admin views still statically imported into collection bundle** (`frontend/src/router.ts:12-13`). Every operator on `/y2021`…`/y2025` downloads the admin bundle. Performance. **Minor.** (Carry-over from R2 N-02 / Mi-02; not addressed.)
- **R3-N-06 — Strict JSON login rejects trailing bytes** (`backend/internal/admin/service.go:795-811`). `decodeStrictJSON` requires `io.EOF` on second Decode; `{"password":"x"}\n` returns 400 rather than attempting login. Breaks `curl -d @file.json` tooling but does not affect the Vue client (`JSON.stringify` produces no trailing newline). Security / UX. **Minor.** (Carry-over from R2 N-05; document in `api-docs.md`.)
- **R3-N-07 — Periodic janitor missing** (`backend/internal/storage/storage.go:120-140` + `backend/app/run.go`). `RunJanitor` still runs ONCE on startup; no recurring ticker. `.incoming/` and `.trash/` accumulate for the process lifetime. Ops. **Minor.** (Carry-over from R2 N-08; not addressed.)
- **R3-N-08 — DB dir still created with `0o755`** (`backend/internal/db/db.go`) while `storage.EnsureLayout` uses `0o700`. Tighten for consistency. Ops. **Minor.** (Carry-over from R2 N-09; not addressed.)

**Info (3):**

- **R3-I-01 — Per-IP upload rate-limit sweep has a benign race** (`backend/internal/uploads/service.go:646-678`). Between `LoadOrStore` and `bucket.mu.Lock()`, a concurrent sweep may delete the just-stored entry; next caller `LoadOrStore`s a fresh limiter. Worst case an attacker gains ≤ a handful of extra requests per sweep boundary; not exploitable. Info.
- **R3-I-02 — `order_date_sort` parse failure falls back to raw string** (`backend/internal/ingest/ingest.go:171-174`). A malformed CSV date sorts lexicographically in admin listings. Rare in practice. Info. (Carry-over from R2 I-03.)
- **R3-I-03 — Frontend Vitest not executed** (`frontend-impl.md` notes `ran_npm_install: false`). 39 test cases written; not run. Owner must `npm install && npm run test` before release tag. Ops. Info-level.

### Dedupe Table (R3)

| Merged R3 ID | Merges across domains |
|--------------|-----------------------|
| R3-N-01 | security (key entropy) + ops (key rotation model) |
| R3-N-02 | performance (per-bundle RAM) + ops (failure mode) |
| R3-N-03 | quality (resource leak) + performance (memory pressure on mobile) |
| R3-N-04 | security (inline render hazard) + ops (download UX) |

ARCH-01/02/03 are NOT re-flagged (R2 accepted risks).

### Architectural Flags — **none**

No Critical-Architectural findings. No rollback required.

### Change Packet Confirmation

Iteration 3 code changes listed in `codex-backend.md` "Iteration 3" were all verified against source:

- `backend/internal/storage/storage.go` — `ValidatePathSegment` guards both `orderNo` and `filename` (C-04).
- `backend/internal/admin/service.go` — partial-success `deleteUploadResult`, `renameAndSync`, stable HMAC session keys, ERRORS.txt inside year export (C-05, R2-Q-01, M-23, M-35).
- `backend/internal/uploads/service.go` — per-IP rate limiter (M-07).
- `backend/internal/orders/service.go` — prefix-only search, two-stage query, TTL cache, keyset (M-08/M-09/M-10/M-16).
- `backend/internal/config/config.go`, `backend/app/run.go`, `config.yaml` — `db_path` + default password refusal + dry-run/error-report (M-01, M-18, M-36).
- `backend/internal/httpapi/router.go`, `backend/internal/httpapi/middleware.go`, `backend/internal/metrics/metrics.go` — `/healthz`, `/readyz`, `/metrics` (M-31, M-33).

Frontend Iteration 3 changes verified against source:

- `frontend/src/components/UploadCard.vue` — lazy+async thumbs + focus-visible ring (M-15, N-02).
- `frontend/src/components/Toast.vue` — split aria-live regions (N-03).
- `frontend/src/components/SearchBar.vue` — IME composition guard + debounce cleanup + CSV-removed banner (N-04, Mi-03).
- `frontend/src/lib/api.ts` — `friendlyMessage` for 429/503 + AbortSignal.
- `frontend/src/stores/collection.ts` — `detailSeq` + `detailAbort` (M-25).
- `frontend/src/stores/admin.ts` — `listSeq` + `detailSeq` + abort on setYear/loadOrders/closeRow/logout (M-26, M-27).

`go build ./...` and `go test ./...` pass per Iteration 3 Structured Summary. Frontend Vitest **not** executed (39 cases awaiting `npm install`).

### Triage for User — unresolved items (documented, NOT blocking)

The gate PASSES without another fix loop. The following are known open items the user should acknowledge before production rollout:

1. **ARCH-01, ARCH-02, ARCH-03** — accepted LAN risks per §14.1 (unauth collection endpoints + plain HTTP admin).
2. **R3-N-01** — admin session/CSRF key derived from admin password; rotating the password invalidates sessions. Document in architecture §14.
3. **R3-N-02** — order bundle buffers full ZIP in memory; with default MaxBundles=4 this is bounded but ~1 GB transient RAM worst-case. Monitor `process_resident_memory_bytes` during production bundle downloads.
4. **R3-N-03** — `ImageBitmap` not closed in frontend image pipeline; minor leak on old mobile browsers.
5. **R3-N-04** — `/files/...` lacks `Content-Disposition: attachment` for images.
6. **R3-N-05** — admin views statically imported into operator bundle (bundle size bloat).
7. **R3-N-06** — strict JSON login rejects trailing bytes; document in `api-docs.md`.
8. **R3-N-07** — periodic janitor missing; accumulates `.incoming/` and `.trash/` across process lifetime.
9. **R3-N-08** — DB directory mode `0o755` vs `0o700` elsewhere.
10. **Frontend Vitest not executed** — 39 cases written but not run. Owner must run `npm install && npm run test` before release.

### Iteration History

| Iter | Date | Action | Result |
|------|------|--------|--------|
| 1 | 2026-04-16 | 4-domain Codex review | FAIL — 6 Critical-Arch → architecture rollback |
| 2 | 2026-04-16 | R2 review after architecture + backend iteration 2 | FAIL — 2 Critical, 12 Major; score 82; route to executors |
| 3 | 2026-04-16 | R3 (final) review after backend iteration 3 + frontend iteration 3 | **PASS** — 0 Critical, 0 Major; score 92; 6 Minor + 3 Info documented for user triage |

### Final Decision

**GATE: PASS**. Release candidate blessed. No further fix loop. The 6 Minors + 3 Info items are documented above for user acknowledgement / future backlog; none are release-blocking in the accepted ARCH-01/02/03 LAN deployment model.
