# Product Collection Form API Docs

所有错误响应统一为：

```json
{
  "error": {
    "code": "BAD_REQUEST",
    "message": "请求不合法"
  }
}
```

通用错误码：

- `400 BAD_REQUEST`
- `400 ORDER_HAS_NO_STAGED_FILES`
- `401 UNAUTHENTICATED`
- `404 YEAR_NOT_FOUND`
- `404 ORDER_NOT_FOUND`
- `404 FILE_NOT_FOUND`
- `409 UPLOAD_CAP_EXCEEDED`
- `413 REQUEST_TOO_LARGE`
- `415 UNSUPPORTED_MEDIA_TYPE`
- `423 ORDER_LOCKED`
- `429 RATE_LIMITED`
- `503 SERVER_BUSY`
- `500 INTERNAL`

## Public Collection API

### `GET /api/y/:year/progress`

用途：返回该年份的总订单数、已上传订单数、完成百分比。

响应：

```json
{
  "total": 5234,
  "uploaded": 120,
  "percent": 0.0229
}
```

状态码：`200`、`404 YEAR_NOT_FOUND`、`500 INTERNAL`

### `GET /api/y/:year/search?q=...&limit=20`

用途：按单据编号前缀搜索订单，默认最多 20 条，最大 50 条。

查询参数：

- `q`：至少 2 个字符，按 `orderNo` 前缀匹配
- `limit`：可选，默认 `20`

响应：

```json
{
  "items": [
    {
      "orderNo": "RX2101-22926",
      "customer": "哈尔滨金诺食品有限公司",
      "uploaded": true,
      "csvPresent": true,
      "counts": {
        "合同": 2,
        "发票": 1,
        "发货单": 0
      }
    }
  ]
}
```

状态码：`200`、`400 BAD_REQUEST`、`404 YEAR_NOT_FOUND`、`500 INTERNAL`

### `GET /api/y/:year/orders/:orderNo`

用途：返回订单详情、该单所有行项目以及当前已上传文件。

响应：

```json
{
  "orderNo": "RX2101-22926",
  "year": 2021,
  "customer": "哈尔滨金诺食品有限公司",
  "csvPresent": true,
  "lines": [
    {
      "orderNo": "RX2101-22926",
      "customer": "哈尔滨金诺食品有限公司",
      "date": "2021/1/4",
      "product": "满特起酥油（FM）",
      "quantity": 1000,
      "amount": 114545.45,
      "totalWithTax": 126000,
      "taxRate": 10,
      "invoiceNo": "2021/50122444"
    }
  ],
  "uploads": {
    "合同": [
      {
        "id": 12,
        "seq": 1,
        "filename": "RX2101-22926-哈尔滨金诺食品有限公司-合同-01.jpg",
        "url": "/files/y/2021/RX2101-22926/RX2101-22926-哈尔滨金诺食品有限公司-合同-01.jpg",
        "size": 384219
      }
    ],
    "发票": [],
    "发货单": []
  }
}
```

状态码：`200`、`404 YEAR_NOT_FOUND`、`404 ORDER_NOT_FOUND`、`500 INTERNAL`

### `POST /api/y/:year/orders/:orderNo/uploads`

用途：流式上传单个订单的新增图片，字段名支持：

- `contract` / `contract[]`
- `invoice` / `invoice[]`
- `delivery` / `delivery[]`

请求：`multipart/form-data`

限制：

- 总请求体不超过 `60 MB`
- 单种类型最多 `9` 张
- 单文件流式接收上限 `10 MB`
- 单文件解码上限 `20 MB`
- 解码前会先读取尺寸，超过 `50,000,000` 像素直接拒绝
- 服务端仅接受 `image/jpeg`、`image/png`、`image/webp`

成功响应：

```json
{
  "counts": {
    "合同": 3,
    "发票": 1,
    "发货单": 0
  },
  "progress": {
    "total": 5234,
    "uploaded": 1,
    "percent": 0.0191
  },
  "mergedPdfStale": false
}
```

如果 DB 已提交但 PDF 重建失败，仍返回 `200`，但 `mergedPdfStale` 为 `true`。

状态码：`200`、`400 BAD_REQUEST`、`400 ORDER_HAS_NO_STAGED_FILES`、`404 YEAR_NOT_FOUND`、`404 ORDER_NOT_FOUND`、`409 UPLOAD_CAP_EXCEEDED`、`413 REQUEST_TOO_LARGE`、`415 UNSUPPORTED_MEDIA_TYPE`、`423 ORDER_LOCKED`、`429 RATE_LIMITED`、`503 SERVER_BUSY`、`500 INTERNAL`

## File Serving

### `GET /files/y/:year/:orderNo/:filename`

用途：返回订单原图或合并 PDF。

校验规则：

- `year` 必须在 `2021..2025`
- `orderNo` 必须存在于 `orders`
- `filename` 必须是：
  - 精确匹配该订单的 `{orderNo}-{customer_clean}-合同与发票.pdf`
  - 或存在于 `uploads.filename`
- 最终路径必须通过路径穿越校验

响应：二进制文件，带 `Cache-Control: private, no-store` 与 `X-Content-Type-Options: nosniff`

状态码：`200`、`404 YEAR_NOT_FOUND`、`404 ORDER_NOT_FOUND`、`404 FILE_NOT_FOUND`、`500 INTERNAL`

## Admin Auth API

### `POST /api/admin/login`

用途：管理员登录。

要求：请求体必须是严格 JSON 对象，只接受 `password` 字段；未知字段会返回 `400 BAD_REQUEST`。

请求：

```json
{
  "password": "CHANGE-ME"
}
```

成功响应：

```json
{
  "ok": true
}
```

副作用：写入 `admin_session` Cookie，`HttpOnly`，`SameSite=Lax`，有效期 12 小时。Cookie 为服务端可验证的签名值，服务重启后仍可继续验证，直到过期。

状态码：`200`、`400 BAD_REQUEST`、`401 UNAUTHENTICATED`、`429 RATE_LIMITED`、`500 INTERNAL`

### `POST /api/admin/logout`

用途：登出管理员。

要求：有效 `admin_session` Cookie + `X-Admin-Csrf`

响应：

```json
{
  "ok": true
}
```

状态码：`200`、`400 BAD_REQUEST`、`401 UNAUTHENTICATED`

### `GET /api/admin/ping`

用途：校验登录态并返回 CSRF Token。

响应：

```json
{
  "ok": true,
  "csrfToken": "hex-token"
}
```

状态码：`200`、`401 UNAUTHENTICATED`

## Admin Query API

### `GET /api/admin/years`

用途：返回每个年份的总量、已上传量、CSV 已移除量。

响应：

```json
[
  {
    "year": 2021,
    "total": 5234,
    "uploaded": 120,
    "csvRemoved": 3
  }
]
```

状态码：`200`、`401 UNAUTHENTICATED`、`500 INTERNAL`

### `GET /api/admin/:year/orders?page=1&size=50&onlyUploaded=true&onlyCsvRemoved=true`

用途：分页查询管理列表。

响应：

```json
{
  "page": 1,
  "size": 50,
  "total": 5234,
  "items": [
    {
      "orderNo": "RX2101-22926",
      "customer": "哈尔滨金诺食品有限公司",
      "uploaded": true,
      "csvRemoved": false,
      "lastUploadAt": "2026-04-16T09:12:45Z",
      "counts": {
        "合同": 2,
        "发票": 1,
        "发货单": 0
      }
    }
  ]
}
```

状态码：`200`、`401 UNAUTHENTICATED`、`404 YEAR_NOT_FOUND`、`500 INTERNAL`

### `GET /api/admin/:year/orders/:orderNo`

用途：返回管理端订单详情，结构与公共详情接口一致。

状态码：`200`、`401 UNAUTHENTICATED`、`404 YEAR_NOT_FOUND`、`404 ORDER_NOT_FOUND`、`500 INTERNAL`

### `GET /api/admin/:year/orders/:orderNo/merged.pdf`

用途：下载该订单当前合并 PDF。

响应：PDF 文件下载。

状态码：`200`、`401 UNAUTHENTICATED`、`404 YEAR_NOT_FOUND`、`404 ORDER_NOT_FOUND`、`404 FILE_NOT_FOUND`

### `GET /api/admin/:year/orders/:orderNo/bundle.zip`

用途：下载单个订单完整资料包。

内容：

- 所有合同原图
- 所有发票原图
- 所有发货单原图
- 合并 PDF

响应：ZIP 文件。服务端会先在内存中完整构建资料包，再一次性返回，避免半截 ZIP。

状态码：`200`、`401 UNAUTHENTICATED`、`404 YEAR_NOT_FOUND`、`404 ORDER_NOT_FOUND`、`423 ORDER_LOCKED`、`503 SERVER_BUSY`、`500 INTERNAL`

### `GET /api/admin/:year/export.zip`

用途：导出整年资料 ZIP。

内容：

- 每个订单目录下的合并 PDF
- 每个订单目录下的发货单原图

不包含合同/发票原图。

响应：ZIP 流。若某个订单在导出时锁超时或文件缺失，ZIP 会附带 `ERRORS.txt` 记录失败项，并仍然正常关闭 ZIP。

状态码：`200`、`401 UNAUTHENTICATED`、`404 YEAR_NOT_FOUND`、`423 ORDER_LOCKED`、`429 RATE_LIMITED`、`500 INTERNAL`

## Admin Mutation API

以下接口都要求：

- 有效 `admin_session` Cookie
- `X-Admin-Csrf` 头

### `DELETE /api/admin/:year/orders/:orderNo/uploads/:id`

用途：删除单张图片，重排同类型序号并重建 PDF。

成功响应：

```json
{
  "ok": true,
  "mergedPdfStale": false
}
```

如果删除已提交但 PDF 重建失败，仍返回 `200`，并带 `mergedPdfStale: true`；管理员可后续调用 `rebuild-pdf` 修复。

状态码：`200`、`400 BAD_REQUEST`、`401 UNAUTHENTICATED`、`404 YEAR_NOT_FOUND`、`404 FILE_NOT_FOUND`、`423 ORDER_LOCKED`、`500 INTERNAL`

### `DELETE /api/admin/:year/orders/:orderNo`

用途：重置单个订单，删除该单所有上传记录与磁盘文件。

成功响应：

```json
{
  "ok": true
}
```

状态码：`200`、`400 BAD_REQUEST`、`401 UNAUTHENTICATED`、`404 YEAR_NOT_FOUND`、`423 ORDER_LOCKED`、`500 INTERNAL`

### `POST /api/admin/:year/orders/:orderNo/rebuild-pdf`

用途：按当前合同/发票原图重建合并 PDF。

成功响应：

```json
{
  "ok": true,
  "pages": 2
}
```

当该订单完全没有上传记录时，返回 `404 ORDER_NOT_FOUND`。

状态码：`200`、`400 BAD_REQUEST`、`401 UNAUTHENTICATED`、`404 YEAR_NOT_FOUND`、`404 ORDER_NOT_FOUND`、`423 ORDER_LOCKED`、`503 SERVER_BUSY`、`500 INTERNAL`

## Ops Endpoints

### `GET /healthz`

用途：进程存活检查。

响应：

```json
{
  "ok": true
}
```

状态码：`200`

### `GET /readyz`

用途：服务就绪检查，当前会验证 SQLite 连接可用。

响应：

```json
{
  "ok": true
}
```

状态码：`200`、`503 SERVER_BUSY`

### `GET /metrics`

用途：Prometheus 文本格式指标。

包含：

- HTTP 请求总数与延迟直方图
- 上传成功次数
- PDF 重建次数
- ZIP 导出次数
- 速率限制命中次数
- 应用观测到的 SQLite 错误次数

状态码：`200`
