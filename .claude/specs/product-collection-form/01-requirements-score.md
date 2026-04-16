# Requirements Self-Score — product-collection-form

Rubric: each dimension 0–20, total 100. Gate: ≥90.

| # | Dimension | Score | Notes |
|---|-----------|-------|-------|
| 1 | Coverage of user intent | 19 | All user-stated features covered: search, 3 upload cards with photo capture, staged delete, naming rules, PDF merge (contract+invoice only), per-year progress, stay-on-page-after-submit, 5 year-scoped links, /admin. Minor deduction: user did not explicitly confirm 发货单 image size caps, chose them defensively. |
| 2 | Functional clarity & acceptance criteria | 19 | FR-ids for search, detail panel, image processing, submit flow, admin, edge cases. Eight numbered acceptance checks near end. |
| 3 | Non-functional coverage (perf, security, reliability, UX) | 18 | NFR-PERF/REL/SEC/UX/COMPAT all present and quantified. Minor: no accessibility/a11y targets beyond tap-size. |
| 4 | Data & integration model | 18 | Dataset layout, canonical fields, naming rules, PDF assembly rules, SQLite setup, and filesystem tree all specified. Minor: no explicit schema for `uploads` table yet — deferred to architecture. |
| 5 | Operational / deployment readiness | 18 | Config file, CLI import, data directory layout, backup strategy (rsync-friendly). Minor: no formal upgrade/migration plan across versions. |

**Total: 92 / 100 → PASS (≥90).**

Open items to cover in the architecture phase:
- Concrete DB schema for orders and uploads.
- Choice of HEIC→JPEG library (heic2any vs. libheif-wasm).
- Go PDF library choice (pdfcpu vs. gofpdf + imaging).
- Zip streaming approach.
