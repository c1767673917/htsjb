# Repository Scan — product-collection-form

## Status
- Working directory: `/Users/lichuansong/Desktop/projects/产品收集表`
- Git repository: **No**
- Platform: macOS (darwin 25.3.0), shell: zsh

## Structure
```
产品收集表/
├── 21-25订单.csv              # source dataset (29,847 rows, 3.5 MB)
└── .claude/specs/product-collection-form/   (workflow artifacts)
    └── 00-repo-scan.md
```

## Source Dataset — `21-25订单.csv`
- Encoding: UTF-8, 10 columns, ~29,846 data rows spanning Y2021–Y2025.
- Header (exact order): `年, 日期, 单据编号, 购货单位, 产品名称, 数量, 金额, 价税合计, 税率(%), 发票号`
- Sample row: `Y2021,2021/2/25,RX2102-23253.,杭州翘歌网络科技有限公司,满特起酥油（FM）,1200,124363.64,136800,10,2021/49856486`
- Notes:
  - 单据编号 has trailing `.` in many rows (needs trimming/normalization).
  - One 单据编号 may have multiple line items (different products) — must de-duplicate by 单据编号 for "unique order count".
  - 购货单位 may be a company name or a personal name (e.g., 谢国强).
  - Multiple 税率 (10% / 16%) present.
  - `年` is of the form `Y2021` .. `Y2025`.

## Detected Stack
- None. Green-field project; only a CSV source file is present.

## Dependencies
- None present.

## Constraints To Clarify With User
Because the repository is otherwise empty, confirm fundamentals before writing requirements/architecture:

1. **Deployment target** — Static site (GitHub Pages / Vercel / internal file share) or full backend needed?
2. **Data source strategy** — Import CSV once into a JSON/SQLite bundle shipped with the app, or keep live loading of CSV?
3. **Storage of uploads** — Where do the three image sets go?
   - In-browser only (PDFs downloaded locally, no server)
   - Uploaded to a cloud bucket (OSS / S3 / 七牛 / ...)
   - POSTed to a custom API + DB
4. **Progress persistence** — Should "uploaded order count" survive browser refresh / be shared across devices?
5. **Authentication / multi-user** — Anonymous internal tool, or login required?
6. **Tech preferences** — React / Vue / plain HTML? PWA / offline capable? Any existing hosting stack to reuse?
7. **PDF merge scope** — Confirm: only 合同 + 发票 combined into one PDF, 发货单 kept as raw image(s)?
8. **Image filename uniqueness** — If a 单据编号 has multiple photos of the same type, append an index? `{单据编号}-{客户名}-合同-1.jpg`, etc.?

The next step is the Requirements phase where these questions will be asked interactively.
