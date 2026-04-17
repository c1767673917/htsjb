# Codex Architecture Review — product-collection-form

Reviewer: Codex (session `019d969a-6880-71d0-be83-63ed76e526ad`)
Reviewed: `02-architecture.md` against `01-requirements.md` and `00-repo-scan.md`.

## Per-Dimension Scores

| # | Dimension | Score | Summary |
|---|-----------|-------|---------|
| 1 | Component classification completeness & correctness | 12/20 | Session storage, export jobs, migrations, healthchecks are under-specified; `go:embed` / `dist` ownership is self-contradictory. |
| 2 | Data model + API contract clarity | 7/20 | `order_lines` UNIQUE key silently drops real CSV rows (3031 collisions measured); submission idempotency contract missing; `/files` vs merged PDF contracts conflict. |
| 3 | Technology choices justified | 11/20 | Core choices are defensible, but if implemented as written will break browser compat, streaming upload NFR, and session persistence. |
| 4 | Security + reliability coverage | 6/20 | No explicit CSRF for cookie-auth admin endpoints; upload+PDF is not a single atomic boundary; path guard is prefix-only; no backup/recovery or observability story. |
| 5 | Implementation sequencing & testing | 8/20 | Delivery sequence is feasible, but "tests and hardening" last is optimistic; test matrix misses double-submit, old iOS Safari, export concurrency, recovery/migration, error-shape consistency. |

**Total: 44/100 — FAIL (gate is ≥90).**

## Critical Issues (must fix before implementation)

1. **`order_lines` UNIQUE key drops real rows.** `UNIQUE(year, order_no, product, invoice_no)` + `INSERT OR IGNORE` (02-architecture.md:148–163, 386) will silently discard 3031 duplicate combinations observed in `21-25订单.csv`, contradicting the requirement that every CSV line becomes one DB row (01-requirements.md:50–59, 80–84).
2. **Submission has no atomic boundary or idempotency.** Per-type transactions + file writes + PDF rebuild are separate side-effects (02-architecture.md:358–377). A second tap or a retry after partial failure appends duplicates; the architecture dismisses this as a "UX caveat" (line 495) — it is actually a data-consistency defect.
3. **Admin cookie sessions lack CSRF defense.** Admin endpoints use `POST`/`DELETE`, but the security section (02-architecture.md:440–451) only lists SameSite + random token, no CSRF token, no `Origin`/`Referer` check. Destructive endpoints (reset, delete photo) demand stronger defense.
4. **Frontend image pipeline fallback is wrong.** Architecture claims a fallback from `OffscreenCanvas` to `canvas` and then calls `canvas.convertToBlob(...)` (02-architecture.md:341–350). `HTMLCanvasElement` has only `toBlob`, not `convertToBlob`. The documented path is broken on old iOS Safari / WeChat WebView.

## Major Issues (should fix)

- **Year export diverges from requirements and lacks snapshot semantics.** Requirements ask for "merged PDF + 发货单 originals" (01-requirements.md:163–165), architecture writes "originals + merged PDF" (02-architecture.md:414–421). No lock or snapshot means concurrent edits tear the zip.
- **Detail API contract misses required columns.** `lines` returns `date / product / quantity / totalWithTax / invoiceNo` (02-architecture.md:224–239); requirements mandate six columns including 单据编号 and 客户 (01-requirements.md:80–84).
- **`/files` contract contradicts itself.** `/files/...` must check the filename exists in DB (02-architecture.md:250–253), but merged PDF is explicitly not stored in DB (02-architecture.md:177–190). 404 / error shape / permission semantics diverge.
- **CSV refresh / migration story incomplete.** Requirements demand "orders missing from new CSV stay uploaded and are marked `CSV 已移除`" (01-requirements.md:178–180), but the schema has no `csv_removed`, `import_batch`, `last_seen_at`. `PRAGMA user_version` is a one-liner.
- **Upload performance plan fights its own NFR.** Requirements require streaming to disk, never buffering full payloads (01-requirements.md:188–190). `ParseMultipartForm(64<<20)` (02-architecture.md:359–360) parses eagerly, then the stdlib copies files again to their final destination.
- **Date sorting has no data-layer guarantee.** Search must be date-ascending (01-requirements.md:62–64), but `order_date` is stored as `YYYY/M/D` text (02-architecture.md:154). String sort places `2021/10/1` before `2021/2/1`.
- **Session and rate limit are memory-only with no sliding TTL.** Restart-wipes-sessions (02-architecture.md:272–274) and IP-keyed counter (02-architecture.md:450–451) are acceptable for LAN, but the doc never clarifies expiration, cleanup, or NAT/shared-exit edge cases.
- **Path guard is string-prefix only.** `filepath.Clean + HasPrefix(data_dir)` (02-architecture.md:443–445) is unsafe under symlinks and case-insensitive filesystems.
- **Observability / operations gaps.** No healthcheck, no metrics, no error-code catalog, no documented backup story (SQLite WAL + `uploads/`), though requirements call out `./data/` backup (01-requirements.md:193–199).

## Minor Issues (nice to fix)

- `go:embed` description is inconsistent: one section says `frontend/dist` is embedded (02-architecture.md:65–66), another says it is copied to `backend/dist` first (02-architecture.md:127–144).
- Deployment mentions `./data/exports/` (01-requirements.md:228–241), but the architecture describes only streaming export — no statement on caching, TTL, or cleanup.
- Error envelope is defined (02-architecture.md:201–203) but no stable error-code catalog is published, so the frontend cannot branch on semantics.

## Concrete Fix Suggestions

- **Split raw vs aggregate models.** `order_line_raw` keyed by `(import_batch_id, source_row_no)` or whole-row hash preserves every CSV row; `orders` and a display view do aggregation. No business-UNIQUE that can drop rows.
- **Introduce order-level submission sessions.** Every submit carries `client_submission_id`. Server records `upload_submissions`, lands files in `staging/{submission_id}`, then under an order lock does `assign seq → bulk insert → rebuild PDF → atomic swap`. Repeated submission IDs return the first result.
- **Add CSRF defense on admin.** Double-submit `csrf_token` cookie (or server-bound token) + mandatory `Origin`/`Referer` validation on all admin `POST/DELETE`. Document reject codes and the frontend injection point.
- **Repair image pipeline branches.** Prefer `createImageBitmap + OffscreenCanvas.convertToBlob`; fall back to `HTMLImageElement/ImageDecoder + HTMLCanvasElement.toBlob`; detect HEIC by MIME + extension; add a "client-side conversion failed → send original, server transcodes or rejects" tail.
- **Year export = snapshot job.** Add `export_jobs`; record a manifest at job start, package `merged PDF + 发货单 originals` only; concurrent order edits either wait on the manifest or are invisible to it.
- **Separate `/files` for raw vs derived.** Originals check the `uploads` row; merged PDF uses a `derived_files` table (or an explicit well-known path rule). Unify error codes across JSON and file paths.
- **CSV refresh schema.** Add `orders.csv_removed`, `orders.last_seen_import_id`, and an `imports` table. Each reimport writes a new batch, then marks orders absent from the batch as removed. Migrations → explicit ordered list, not a single `user_version` bump.
- **Streaming upload path.** Replace `ParseMultipartForm` primary path with `multipart.NewReader`, processing each part directly onto a temp file in the target directory.
- **Normalize dates.** Store ISO `YYYY-MM-DD` (or Unix day). Return the six required columns in `lines` (orderNo, customer, product, quantity, totalWithTax, invoiceNo).
- **Document session / rate-limit behavior.** TTL, last-seen cleanup, accept-restart-relogin as an explicit operational premise; rate-limit key ⊇ IP (+ optional UA) with admin allow-list flag.
- **Harden path guard.** `EvalSymlinks` the result and compare to canonical `data_dir`; forbid symlinks under `uploads`.
- **Observability & recovery.** Add `/healthz`, `/readyz`, basic counters (requests, errors, export duration, PDF failures). Document backup as `app.db + app.db-wal + app.db-shm + uploads/`; write out recovery steps.

## Critical-Architectural Findings (trigger rollback)

1. **`order_lines` data model must be redesigned.** Every downstream capability (detail page, reimport, "CSV 已移除" flag) is built on a boundary that discards real rows.
2. **Submission boundary must be rebuilt as a resumable, replayable, deduplicated order-level transaction.** Double-tap, retry after partial failure, crash recovery, and export consistency all depend on this.
