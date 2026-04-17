# Architecture Scoring Log — product-collection-form

Rubric: 5 dimensions × 20 points. Gate: ≥ 90.

## Final score (after 3 Codex iterations)

**Codex total: 93 / 100 — PASS**

| # | Dimension | Score |
|---|-----------|-------|
| 1 | Requirements coverage | 19 |
| 2 | Component classification correctness | 20 |
| 3 | Data model & API contract clarity | 18 |
| 4 | Security + reliability | 18 |
| 5 | Implementation sequencing + testing | 18 |

`must_fix_before_impl`: **[] (empty)**

Residual nits (to handle during implementation, not spec blockers):
1. During merged-PDF rebuild, file-serving endpoints may race with `.bak`/`.new` swap — implement a read-through fallback that, on a primary-path 404, tries the `.bak-{txid}` atomic at serve time.
2. Server accepts `image/jpeg | image/png | image/webp` — narrower than `image/*`. This is deliberate (those are what the frontend pipeline emits). Requirements NFR-SEC-1 said "image/*"; the intent is "only image formats we trust"; these three are sufficient. Flagged as a documentation gap to close in `api-docs.md` on first API doc emission.
3. `DELETE /uploads/:id` mergedPdfStale response shape not in the canonical API table — mirror §6.3 submit response with `{counts, mergedPdfStale}` shape when the PDF post-commit step fails.

## Iteration History
- v1: Self-score 94, Codex score 67. Major gaps: missing CSV-drift handling, bundle.zip wrong scope, multipart not streaming, non-atomic submit, file serving self-contradiction, over-aggressive idempotency key.
- v2: Codex score 88. Residual gaps: bundle.zip still wrong, submit zero-file rejection too strict, error-code conflicts, post-commit atomicity incomplete, admin list shape missing.
- v3: Codex score 86. Residual: bundle.zip vs year-export still conflicting per §12, rebuild-pdf not in API table, /admin/ping shape undefined, post-commit 500 semantics still risky.
- v4: Codex score **93**. Gate passed.
