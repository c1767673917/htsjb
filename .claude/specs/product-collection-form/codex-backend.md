# Change Packet

## git status --short

```text
 M .claude/specs/product-collection-form/02-architecture-score.md
 M .claude/specs/product-collection-form/02-architecture.md
?? .claude/scheduled_tasks.lock
?? .claude/specs/product-collection-form/02-architecture-codex-review.md
?? .claude/specs/product-collection-form/api-docs.md
?? .claude/specs/product-collection-form/codex-backend.md
?? .claude/specs/product-collection-form/frontend-impl.md
?? .gitignore
?? backend/
?? cmd/
?? config.yaml
?? dist_embed.go
?? frontend/
?? go.mod
?? go.sum
```

## Iteration 2

### Change Packet

- `backend/app/run.go`：接入 `slog` JSON 日志、HTTP 服务超时参数、优雅关停、WAL checkpoint、`--reimport` 语义和新的全局限流初始化。
- `backend/internal/db/db.go`：改为 modernc SQLite 连接钩子，在每个池连接上执行 WAL / `busy_timeout` / `foreign_keys` / `temp_store` PRAGMA，并设置连接池参数。
- `backend/internal/httpapi/router.go`、`backend/internal/httpapi/middleware.go`、`backend/internal/httpapi/limits/limits.go`：新增安全头、结构化请求日志、请求 ID、路径级超时分发、`SERVER_BUSY` 错误和全局 admission control。
- `backend/internal/uploads/service.go`：上传路径接入 upload/pdf/decode 三类闸门，严格执行单文件体积 / decode cap / 像素上限，统一全格式重编码为 JPEG，重建 PDF 走流式写盘并补目录 `fsync`，补 panic 清理与上下文传播。
- `backend/internal/pdfmerge/pdfmerge.go`：`Build(ctx, imagePaths, io.Writer)` 直接向目标 writer 输出 PDF，不再持有整份 PDF buffer；超大边长图片缩放到 2000px 再嵌入。
- `backend/internal/ingest/ingest.go`：CSV 导入改为 `BEGIN IMMEDIATE` 单连接全量刷新 `order_lines`，保留 `source_hash` 仅做导入内去重，并在重导入时刷新 `customer` / `customer_clean`。
- `backend/internal/admin/service.go`：严格 JSON 登录体校验、`SameSite=Lax`、5 次 / 5 分钟登录窗口、bundle/export admission control、bundle 改为 DB 驱动的文件清单、ZIP 改为 `zip.Store`、导出/打包路径接入上下文传播。
- `backend/internal/storage/storage.go`：修复路径穿越校验，拒绝 `/`、`\`、`..`、盘符、NUL 和 symlink。
- `backend/internal/*_test.go`、`backend/tests/integration/app_test.go`：更新接口适配并新增单文件上限、decode cap、像素上限、symlink、bundle 顺序等回归测试。
- `.claude/specs/product-collection-form/api-docs.md`：补充 `SERVER_BUSY`、year export `RATE_LIMITED`、上传图片上限和严格登录 JSON 说明。

### Review Findings

已处理：

- `ARCH-04`
- `ARCH-05`
- `ARCH-06`
- `C-01`
- `C-02`
- `C-03`
- `C-06`
- `C-07`
- `M-02`
- `M-03`
- `M-04`
- `M-05`
- `M-06`
- `M-12`
- `M-14`
- `M-17`
- `M-19`
- `M-20`
- `M-21`
- `M-22`
- `M-24`
- `M-29`
- `M-30`
- `M-32`
- `M-34`

延期：

- `C-04`：仍未引入 path-safe `order_no` 磁盘编码，当前仅修复现有路径穿越校验。
- `C-05`：删除图片后 DB 已提交但 PDF 重建失败时，仍是失败返回而非显式 partial-success 合约。
- `M-01`：默认管理员密码 `CHANGE-ME` 仍未在配置校验阶段拒绝。
- `M-07`：未实现更细粒度的公网配额 / 磁盘水位控制。
- `M-08`
- `M-09`
- `M-10`
- `M-11`
- `M-13`
- `M-15`：前端范围，按约束未修改。
- `M-16`
- `M-18`
- `M-23`
- `M-25`：前端范围，按约束未修改。
- `M-26`：前端范围，按约束未修改。
- `M-27`：前端范围，按约束未修改。
- `M-28`：前端范围，按约束未修改。
- `M-31`
- `M-33`
- `M-35`
- `M-36`

### Structured Summary

```json
{
  "status": "success",
  "changes": [
    {
      "path": "backend/internal/httpapi/limits/limits.go",
      "status": "created",
      "summary": "新增全局 admission control，覆盖 upload/pdf/bundle/year-export/image-decode"
    },
    {
      "path": "backend/internal/db/db.go",
      "status": "modified",
      "summary": "SQLite 改为每连接 PRAGMA + 连接池参数"
    },
    {
      "path": "backend/internal/uploads/service.go",
      "status": "modified",
      "summary": "上传链路接入大小/像素限制、统一 JPEG 重编码、上下文传播、panic 清理和流式 PDF 重建"
    },
    {
      "path": "backend/internal/pdfmerge/pdfmerge.go",
      "status": "modified",
      "summary": "PDF 直接输出到 writer，按页处理图片并在超大边长时降采样"
    },
    {
      "path": "backend/internal/ingest/ingest.go",
      "status": "modified",
      "summary": "CSV 重导入改为 BEGIN IMMEDIATE 全量刷新"
    },
    {
      "path": "backend/internal/admin/service.go",
      "status": "modified",
      "summary": "严格登录 JSON、修复登录限流、bundle/export admission control、DB 驱动 bundle 文件清单"
    },
    {
      "path": "backend/internal/storage/storage.go",
      "status": "modified",
      "summary": "修复文件路径校验并拒绝 symlink"
    },
    {
      "path": "backend/app/run.go",
      "status": "modified",
      "summary": "接入结构化日志、HTTP 超时参数、优雅关停和 WAL checkpoint"
    },
    {
      "path": ".claude/specs/product-collection-form/api-docs.md",
      "status": "modified",
      "summary": "更新接口错误码和行为说明"
    },
    {
      "path": ".claude/specs/product-collection-form/codex-backend.md",
      "status": "modified",
      "summary": "追加 Iteration 2 变更包、评审项状态与结构化摘要"
    }
  ],
  "tests": {
    "build": "passed",
    "go_test": "passed",
    "commands": [
      "go build ./...",
      "go test ./..."
    ]
  },
  "questions": []
}
```

## Iteration 3

### Change Packet

- `backend/internal/storage/storage.go`、`backend/internal/ingest/ingest.go`：把 `order_no` 纳入路径段校验，拒绝 `/`、`\`、`..`、NUL、绝对路径、点开头和 URL-encoded traversal；`ValidateOrderFilePath` 同时校验 `orderNo` 与 `filename`。
- `backend/internal/admin/service.go`：删除图片接口改为 `200 { ok, mergedPdfStale }` partial-success 语义；delete-photo 全链路 `rename` 改走 `renameAndSync`；admin session 改为稳定签名 cookie，重启后仍可验证；year export 改为批量查询并在 ZIP 内写 `ERRORS.txt`，bundle 改为先缓冲后返回。
- `backend/internal/uploads/service.go`：新增 per-IP 上传限流（20/min/IP，闲置 10 分钟回收），命中返回 `429 RATE_LIMITED`；去掉 stage 临时文件不必要的 `fsync`。
- `backend/internal/orders/service.go`：搜索改为前缀匹配并拆成“订单列表 + 上传计数”两段查询；进度/年份统计增加 5 秒 TTL 缓存；管理列表内部改 keyset 迭代以消除 `OFFSET`。
- `backend/internal/config/config.go`、`backend/app/run.go`、`config.yaml`：新增 `db_path` / `APP_DB_PATH`；`serve` 启动时拒绝空密码或 `CHANGE-ME`，`import-csv` 继续允许运行；`import-csv` 增加 `--dry-run` 与 `--error-report`。
- `backend/internal/httpapi/router.go`、`backend/internal/httpapi/middleware.go`、`backend/internal/metrics/metrics.go`：新增 `/healthz`、`/readyz`、`/metrics`，并暴露 Prometheus 文本指标（请求总数/延迟、上传、PDF、ZIP、限流、SQLite 错误）。
- `backend/internal/*_test.go`、`backend/tests/integration/app_test.go`：新增 `order_no` traversal、默认密码校验、删除后 `mergedPdfStale`、session 跨重启、上传 rate limit、healthz/readyz/metrics、CSV 非法单号等回归测试。

### Addressed

- `C-04`
- `C-05`
- `R2-Q-01`
- `M-01`
- `M-07`
- `M-08`
- `M-09`
- `M-10`
- `M-11`
- `M-13`
- `M-16`
- `M-18`
- `M-23`
- `M-31`
- `M-33`
- `M-35`
- `M-36`

### Deferred

- `M-07`：已补 per-IP 限流，但未额外实现磁盘高水位告警或更细的跨订单磁盘配额。

### Structured Summary

```json
{
  "status": "success",
  "changes": [
    {
      "path": "backend/internal/storage/storage.go",
      "status": "modified",
      "summary": "order_no 与 filename 统一走路径段校验，拦截 traversal 与 URL-encoded 绕过"
    },
    {
      "path": "backend/internal/admin/service.go",
      "status": "modified",
      "summary": "删除图片改为 partial-success，admin delete 全部 rename+fsync，session 改为稳定签名 cookie，ZIP 导出不再输出损坏流"
    },
    {
      "path": "backend/internal/uploads/service.go",
      "status": "modified",
      "summary": "新增 per-IP 上传限流并减少 stage 文件多余 fsync"
    },
    {
      "path": "backend/internal/orders/service.go",
      "status": "modified",
      "summary": "搜索前缀匹配 + 两段查询，进度/年份缓存，管理列表改 keyset"
    },
    {
      "path": "backend/internal/config/config.go",
      "status": "modified",
      "summary": "新增 db_path，并在 serve 模式下拒绝默认管理员密码"
    },
    {
      "path": "backend/internal/metrics/metrics.go",
      "status": "created",
      "summary": "新增 Prometheus 文本指标注册表"
    },
    {
      "path": "backend/app/run.go",
      "status": "modified",
      "summary": "import-csv 增加 dry-run / error-report，DB 路径改走配置"
    },
    {
      "path": ".claude/specs/product-collection-form/api-docs.md",
      "status": "modified",
      "summary": "更新删除接口响应、搜索语义、上传限流与运维端点文档"
    },
    {
      "path": ".claude/specs/product-collection-form/codex-backend.md",
      "status": "modified",
      "summary": "追加 Iteration 3 变更包、addressed/deferred 和结构化摘要"
    }
  ],
  "tests": {
    "build": "passed",
    "go_test": "passed",
    "commands": [
      "GOCACHE=$(pwd)/.tmp/gocache GOTMPDIR=$(pwd)/.tmp/gotmp go build ./...",
      "GOCACHE=$(pwd)/.tmp/gocache GOTMPDIR=$(pwd)/.tmp/gotmp go test ./..."
    ]
  },
  "questions": []
}
```

## git diff --stat

```text
 .../02-architecture-score.md                       |  38 +-
 .../product-collection-form/02-architecture.md     | 656 ++++++++++++++-------
 2 files changed, 459 insertions(+), 235 deletions(-)
```

## Per-file Notes

- `go.mod` / `go.sum`: 初始化 Go 模块与后端依赖。
- `.gitignore`: 忽略 `data/`、数据库文件、`node_modules/` 与 `frontend/dist` 构建产物。
- `config.yaml`: 提供运行模板配置。
- `dist_embed.go`: 在仓库根部嵌入 `frontend/dist`，供单二进制静态资源服务使用。
- `cmd/server/main.go`: CLI 入口，转调 `backend/app`。
- `backend/app/run.go`: `serve` 与 `import-csv` 子命令、启动装配与初始化流程。
- `backend/internal/apierror/apierror.go`: 统一错误码与 HTTP 映射。
- `backend/internal/config/config.go`: YAML 加载、默认值、环境变量覆盖与校验。
- `backend/internal/db/db.go`: SQLite 打开、WAL/PRAGMA 设置与基于 `user_version` 的迁移。
- `backend/internal/ingest/ingest.go`: CSV 导入、`source_hash` 去重、`csv_present` 翻转。
- `backend/internal/orders/service.go`: 进度、搜索、详情、管理列表/年份查询。
- `backend/internal/storage/storage.go`: 路径守卫、每单锁、`incoming`/`trash`/janitor、文件名清洗。
- `backend/internal/pdfmerge/pdfmerge.go`: JPEG 到 A4 PDF 合并、原子写入、大图保护。
- `backend/internal/uploads/service.go`: 流式上传、原子提交、PDF stale 恢复。
- `backend/internal/admin/service.go`: session、CSRF、列表、详情、删除、重置、重建 PDF、ZIP 导出。
- `backend/internal/httpapi/router.go`: 路由挂载、文件服务、SPA history fallback。
- `backend/internal/httpapi/middleware.go`: 简单请求日志。
- `backend/internal/**/*_test.go`: 后端单元测试草案。
- `backend/tests/integration/app_test.go`: HTTP 级集成测试草案。
- `.claude/specs/product-collection-form/api-docs.md`: 全量 API 文档。
- `.claude/specs/product-collection-form/codex-backend.md`: 本次实现摘要与变更包。
- `frontend/dist/index.html` / `.gitkeep`: 前端构建占位，保证嵌入编译路径存在。
- `.claude/specs/product-collection-form/02-architecture-score.md`、`.claude/specs/product-collection-form/02-architecture.md`、`.claude/scheduled_tasks.lock`、`.claude/specs/product-collection-form/02-architecture-codex-review.md`、`.claude/specs/product-collection-form/frontend-impl.md`: 这些出现在当前工作区状态里，但不是本次后端实现修改内容，保持原样未动。

## Structured Summary

```json
{
  "status": "success",
  "changes": [
    {"path": "go.mod", "status": "created", "summary": "初始化模块与依赖"},
    {"path": "go.sum", "status": "created", "summary": "生成依赖校验文件"},
    {"path": ".gitignore", "status": "created", "summary": "补充运行期与构建产物忽略规则"},
    {"path": "config.yaml", "status": "created", "summary": "添加服务配置模板"},
    {"path": "dist_embed.go", "status": "created", "summary": "嵌入 frontend/dist 静态资源"},
    {"path": "cmd/server/main.go", "status": "created", "summary": "添加 CLI 二进制入口"},
    {"path": "backend/app/run.go", "status": "created", "summary": "组装服务启动与导入命令"},
    {"path": "backend/internal/apierror/apierror.go", "status": "created", "summary": "定义统一 API 错误"},
    {"path": "backend/internal/config/config.go", "status": "created", "summary": "实现配置加载与环境变量覆盖"},
    {"path": "backend/internal/db/db.go", "status": "created", "summary": "实现 SQLite 打开与迁移"},
    {"path": "backend/internal/ingest/ingest.go", "status": "created", "summary": "实现 CSV 导入与幂等逻辑"},
    {"path": "backend/internal/orders/service.go", "status": "created", "summary": "实现订单查询与管理列表逻辑"},
    {"path": "backend/internal/storage/storage.go", "status": "created", "summary": "实现路径守卫、锁和清理器"},
    {"path": "backend/internal/pdfmerge/pdfmerge.go", "status": "created", "summary": "实现图片合并 PDF"},
    {"path": "backend/internal/uploads/service.go", "status": "created", "summary": "实现流式上传和原子提交"},
    {"path": "backend/internal/admin/service.go", "status": "created", "summary": "实现管理端鉴权、删除、导出和重建"},
    {"path": "backend/internal/httpapi/router.go", "status": "created", "summary": "实现 API 路由、静态文件和 SPA 回退"},
    {"path": "backend/internal/httpapi/middleware.go", "status": "created", "summary": "添加请求日志中间件"},
    {"path": "backend/internal/ingest/ingest_test.go", "status": "created", "summary": "添加导入幂等与 csv_present 测试"},
    {"path": "backend/internal/storage/storage_test.go", "status": "created", "summary": "添加路径穿越拒绝测试"},
    {"path": "backend/internal/pdfmerge/pdfmerge_test.go", "status": "created", "summary": "添加 PDF 页序测试"},
    {"path": "backend/internal/uploads/service_test.go", "status": "created", "summary": "添加上传错误码、原子提交流程与流式请求测试"},
    {"path": "backend/internal/admin/service_test.go", "status": "created", "summary": "添加管理端导出、CSRF 和重建测试"},
    {"path": "backend/tests/integration/app_test.go", "status": "created", "summary": "添加端到端上传与管理流程集成测试"},
    {"path": ".claude/specs/product-collection-form/api-docs.md", "status": "created", "summary": "补充接口文档"},
    {"path": ".claude/specs/product-collection-form/codex-backend.md", "status": "created", "summary": "记录变更包与结构化摘要"},
    {"path": "frontend/dist/index.html", "status": "created", "summary": "添加嵌入编译占位页"},
    {"path": "frontend/dist/.gitkeep", "status": "created", "summary": "保留空目录占位"}
  ],
  "tests": {
    "added": 8,
    "passed": null,
    "failed": null
  },
  "questions": []
}
```
