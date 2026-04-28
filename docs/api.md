# API 文档

本文档描述“产品收集表”服务端 HTTP API。默认服务地址来自 `config.yaml` 的 `listen`，开发环境通常为：

- 后端：`http://127.0.0.1:8080`
- 前端开发代理：`http://127.0.0.1:5173`

## 通用约定

### 数据格式

- JSON 接口请求头：`Content-Type: application/json`
- 文件上传接口请求头：`Content-Type: multipart/form-data`
- 文件下载接口直接返回文件流。
- 时间字段通常为 ISO 8601 / RFC3339 字符串，例如：`2026-04-24T10:00:00Z`。

### 错误响应

大多数错误返回统一 JSON：

```json
{
  "error": {
    "code": "BAD_REQUEST",
    "message": "请求参数错误"
  }
}
```

常见状态码：

| 状态码 | 含义 |
| --- | --- |
| `400` | 请求参数或请求体格式错误 |
| `401` | 管理员未登录或会话失效 |
| `404` | 订单、发票或文件不存在 |
| `409` | 上传数量超限等业务冲突 |
| `413` | 请求体或单文件过大 |
| `429` | 请求过于频繁 |
| `500` | 服务器内部错误 |
| `503` | 并发资源暂不可用 |

### 年份参数

订单相关公开接口与管理接口中的 `:year` 为年份路径参数，当前前端使用 `2021` 至 `2025`。

### 上传限制

限制以 `config.yaml` 为准，当前配置包括：

- 每类订单文件最多 `50` 个。
- 单次提交请求体最大 `60MB`。
- 单文件最大 `10MB`。
- 订单图片支持 `image/jpeg`、`image/png`、`image/webp`，服务端会保存为 JPEG。
- 发票录入专区最多上传 `1` 个文件，支持图片字段与 PDF 字段。

## 健康检查

### `GET /healthz`

服务存活检查。

响应：

```json
{
  "ok": true
}
```

### `GET /readyz`

服务就绪检查，会检查数据库连通性。

响应：

```json
{
  "ok": true
}
```

### `GET /metrics`

Prometheus 指标接口。

响应：`text/plain` 指标文本。

## 公开订单接口

### `GET /api/y/{year}/progress`

获取某年度订单上传进度。

路径参数：

| 参数 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `year` | number | 是 | 年份 |

响应：

```json
{
  "total": 100,
  "uploaded": 35,
  "percent": 0.35
}
```

字段说明：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `total` | number | 年度订单总数 |
| `uploaded` | number | 已上传至少一个文件的订单数 |
| `percent` | number | 完成百分比 |

### `GET /api/y/{year}/search`

搜索某年度订单。

查询参数：

| 参数 | 类型 | 必填 | 默认值 | 说明 |
| --- | --- | --- | --- | --- |
| `q` | string | 否 | 空 | 搜索关键词，匹配订单号/客户等 |
| `limit` | number | 否 | `20` | 返回数量 |

响应：

```json
{
  "items": [
    {
      "orderNo": "SO20240001",
      "customer": "示例客户",
      "csvPresent": true,
      "uploaded": true,
      "counts": {
        "合同": 1,
        "发票": 2,
        "发货单": 1
      }
    }
  ]
}
```

### `GET /api/y/{year}/orders/{orderNo}`

获取订单详情。

路径参数：

| 参数 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `year` | number | 是 | 年份 |
| `orderNo` | string | 是 | 单据编号 |

响应：

```json
{
  "orderNo": "SO20240001",
  "year": 2024,
  "customer": "示例客户",
  "csvPresent": true,
  "checkStatus": "未检查",
  "lines": [
    {
      "orderNo": "SO20240001",
      "customer": "示例客户",
      "date": "2024-01-02",
      "product": "产品A",
      "quantity": 10,
      "amount": 1000,
      "totalWithTax": 1130,
      "taxRate": 0.13,
      "invoiceNo": "INV001"
    }
  ],
  "uploads": {
    "合同": [
      {
        "id": 1,
        "seq": 1,
        "filename": "SO20240001-示例客户-合同-01.jpg",
        "size": 123456,
        "url": "/files/y/2024/SO20240001/SO20240001-示例客户-合同-01.jpg",
        "operator": "张三"
      }
    ],
    "发票": [],
    "发货单": []
  }
}
```

`checkStatus` 可取值：`未检查`、`已检查`、`错误`。

### `POST /api/y/{year}/orders/{orderNo}/uploads`

为订单上传图片文件。

请求类型：`multipart/form-data`

表单字段：

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `operator` | string | 否 | 录入人，最长读取 128 字节 |
| `contract` / `contract[]` | file[] | 否 | 合同图片 |
| `invoice` / `invoice[]` | file[] | 否 | 发票图片 |
| `delivery` / `delivery[]` | file[] | 否 | 发货单图片 |

至少需要上传一个有效文件。服务端按已有最大序号递增命名并保存。

示例：

```bash
curl -X POST http://127.0.0.1:8080/api/y/2024/orders/SO20240001/uploads \
  -F 'operator=张三' \
  -F 'contract=@合同.jpg' \
  -F 'invoice=@发票.png' \
  -F 'delivery=@发货单.webp'
```

响应：

```json
{
  "counts": {
    "合同": 1,
    "发票": 1,
    "发货单": 1
  },
  "progress": {
    "total": 100,
    "uploaded": 36,
    "percent": 0.36
  },
  "mergedPdfStale": false
}
```

### `DELETE /api/y/{year}/orders/{orderNo}/uploads/{id}`

公开端删除订单上传文件。

路径参数：

| 参数 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `year` | number | 是 | 年份 |
| `orderNo` | string | 是 | 单据编号 |
| `id` | number | 是 | 上传文件 ID |

响应：

```json
{
  "ok": true,
  "mergedPdfStale": false
}
```

## 公开发票接口

### `GET /api/invoices/progress`

获取发票按年度统计的上传进度。

响应：

```json
[
  {
    "year": 2024,
    "total": 100,
    "uploaded": 80,
    "percent": 0.8
  }
]
```

### `GET /api/invoices/search`

搜索发票。

查询参数：

| 参数 | 类型 | 必填 | 默认值 | 说明 |
| --- | --- | --- | --- | --- |
| `q` | string | 否 | 空 | 搜索关键词，匹配发票号/客户等 |
| `limit` | number | 否 | `20` | 返回数量 |

响应：

```json
{
  "items": [
    {
      "invoiceNo": "INV001",
      "customer": "示例客户",
      "invoiceDate": "2024-01-02",
      "uploaded": true,
      "uploadCount": 1
    }
  ]
}
```

### `GET /api/invoices/{invoiceNo}`

获取发票详情。

响应：

```json
{
  "invoiceNo": "INV001",
  "customer": "示例客户",
  "seller": "销方公司",
  "invoiceDate": "2024-01-02",
  "lines": [
    {
      "product": "产品A",
      "quantity": 10,
      "amount": 1000,
      "taxAmount": 130,
      "totalWithTax": 1130,
      "taxRate": "13%"
    }
  ],
  "uploads": [
    {
      "id": 1,
      "seq": 1,
      "filename": "INV001-01.pdf",
      "size": 123456,
      "contentType": "application/pdf",
      "url": "/files/invoices/INV001/INV001-01.pdf",
      "operator": "李四"
    }
  ]
}
```

### `POST /api/invoices/{invoiceNo}/uploads`

为发票上传一个文件。

请求类型：`multipart/form-data`

表单字段：

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `operator` | string | 否 | 录入人，最长读取 128 字节 |
| `invoice_photo` / `invoice_photo[]` | file | 否 | 发票图片，保存为 JPEG |
| `invoice_pdf` / `invoice_pdf[]` | file | 否 | 发票 PDF |

约束：

- 每张发票最多上传 `1` 个文件。
- `invoice_photo` 与 `invoice_pdf` 合计只能提交 `1` 个文件。

示例：

```bash
curl -X POST http://127.0.0.1:8080/api/invoices/INV001/uploads \
  -F 'operator=李四' \
  -F 'invoice_pdf=@发票.pdf'
```

响应：

```json
{
  "uploadCount": 1
}
```

### `DELETE /api/invoices/{invoiceNo}/uploads/{id}`

公开端删除发票上传文件。

响应：

```json
{
  "ok": true
}
```

## 文件访问接口

### `GET /files/y/{year}/{orderNo}/{filename}`

访问订单上传文件或合并 PDF。

说明：

- `filename` 必须是数据库中存在的上传文件名，或该订单允许访问的合并 PDF 文件名。
- 成功时返回文件流。
- 失败时返回 `404` 错误 JSON。

### `GET /files/invoices/{invoiceNo}/{filename}`

访问发票上传文件。

说明：

- `filename` 必须是数据库中存在的发票上传文件名。
- 成功时返回文件流。
- 失败时返回 `404` 错误 JSON。

## 管理员认证

管理员接口前缀：`/api/admin`

### 会话 Cookie

登录成功后服务端写入 HttpOnly Cookie：

- Cookie 名称：`admin_session`
- SameSite：`Lax`
- 过期时间：由 `config.yaml` 的 `session_ttl_hours` 控制

### CSRF Token

管理员“写操作”需要请求头：

```http
X-Admin-Csrf: <csrfToken>
```

获取方式：登录后调用 `GET /api/admin/ping`，响应中包含 `csrfToken`。

写操作包括：

- `POST /api/admin/logout`
- `POST /api/admin/{year}/orders/{orderNo}/rebuild-pdf`
- `POST /api/admin/{year}/orders/{orderNo}/check`
- `DELETE /api/admin/{year}/orders/{orderNo}/uploads/{id}`
- `DELETE /api/admin/{year}/orders/{orderNo}`
- `DELETE /api/admin/invoices/{invoiceNo}/uploads/{id}`
- `DELETE /api/admin/invoices/{invoiceNo}`

### `POST /api/admin/login`

管理员登录。

请求体：

```json
{
  "password": "admin123"
}
```

响应：

```json
{
  "ok": true
}
```

### `POST /api/admin/logout`

管理员退出登录。需要会话 Cookie 与 `X-Admin-Csrf`。

响应：

```json
{
  "ok": true
}
```

### `GET /api/admin/ping`

检查管理员会话，并获取 CSRF Token。

响应：

```json
{
  "ok": true,
  "csrfToken": "csrf-token-string"
}
```

## 管理员订单接口

### `GET /api/admin/years`

获取各年度订单统计。

响应：

```json
[
  {
    "year": 2024,
    "total": 100,
    "uploaded": 80,
    "csvRemoved": 2
  }
]
```

### `GET /api/admin/{year}/orders`

分页获取订单列表。

查询参数：

| 参数 | 类型 | 必填 | 默认值 | 说明 |
| --- | --- | --- | --- | --- |
| `page` | number | 否 | `1` | 页码 |
| `size` | number | 否 | `50` | 每页数量 |
| `q` | string | 否 | 空 | 搜索关键词 |
| `onlyUploaded` | boolean | 否 | `false` | 仅看已上传 |
| `onlyCsvRemoved` | boolean | 否 | `false` | 仅看 CSV 已移除订单 |
| `checkStatus` | string | 否 | 空 | 检查状态：`未检查`、`已检查`、`错误` |

响应：

```json
{
  "page": 1,
  "size": 50,
  "total": 1,
  "items": [
    {
      "orderNo": "SO20240001",
      "customer": "示例客户",
      "csvRemoved": false,
      "checkStatus": "未检查",
      "lastUploadAt": "2026-04-24T10:00:00Z",
      "uploaded": true,
      "counts": {
        "合同": 1,
        "发票": 1,
        "发货单": 1
      },
      "operators": ["张三"]
    }
  ]
}
```

### `GET /api/admin/{year}/orders/{orderNo}`

获取管理员视角订单详情。响应结构同公开订单详情。

### `GET /api/admin/{year}/orders/{orderNo}/merged.pdf`

下载该订单合并 PDF。

响应：`application/pdf` 文件流。

### `GET /api/admin/{year}/orders/{orderNo}/bundle.zip`

下载该订单文件包。

响应：`application/zip` 文件流。

### `POST /api/admin/{year}/orders/{orderNo}/rebuild-pdf`

重新生成订单合并 PDF。需要 `X-Admin-Csrf`。

响应：

```json
{
  "ok": true,
  "pages": 3
}
```

### `POST /api/admin/{year}/orders/{orderNo}/check`

设置订单检查状态。需要 `X-Admin-Csrf`。

请求体：

```json
{
  "status": "已检查"
}
```

`status` 可取值：`未检查`、`已检查`、`错误`。

响应：

```json
{
  "ok": true,
  "checkStatus": "已检查"
}
```

### `DELETE /api/admin/{year}/orders/{orderNo}/uploads/{id}`

管理员删除订单上传文件。需要 `X-Admin-Csrf`。

响应：

```json
{
  "ok": true,
  "mergedPdfStale": false
}
```

### `DELETE /api/admin/{year}/orders/{orderNo}`

重置订单上传数据。需要 `X-Admin-Csrf`。

响应：

```json
{
  "ok": true
}
```

### `GET /api/admin/{year}/export.csv`

导出订单列表 CSV。

查询参数：

| 参数 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `q` | string | 否 | 搜索关键词 |
| `onlyUploaded` | boolean | 否 | 仅导出已上传 |
| `onlyCsvRemoved` | boolean | 否 | 仅导出 CSV 已移除订单 |
| `checkStatus` | string | 否 | 检查状态过滤 |

响应：`text/csv; charset=utf-8` 文件流。

CSV 表头：

```text
单据编号,客户,已上传,检查状态,合同数量,发票数量,发货单数量,录入人,最后上传时间
```

### `GET /api/admin/{year}/export.zip`

导出年度订单文件 ZIP。

查询参数：

| 参数 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `operator` | string | 否 | 按录入人模糊过滤 |
| `uploadFrom` | string | 否 | 上传开始日期/时间 |
| `uploadTo` | string | 否 | 上传结束日期/时间 |

响应：`application/zip` 文件流。

## 管理员发票接口

### `GET /api/admin/invoices`

分页获取发票列表。

查询参数：

| 参数 | 类型 | 必填 | 默认值 | 说明 |
| --- | --- | --- | --- | --- |
| `page` | number | 否 | `1` | 页码 |
| `size` | number | 否 | `50` | 每页数量 |
| `q` | string | 否 | 空 | 搜索关键词 |
| `onlyUploaded` | boolean | 否 | `false` | 仅看已上传 |

响应：

```json
{
  "page": 1,
  "size": 50,
  "total": 1,
  "items": [
    {
      "invoiceNo": "INV001",
      "customer": "示例客户",
      "seller": "销方公司",
      "invoiceDate": "2024-01-02",
      "uploaded": true,
      "uploadCount": 1,
      "operators": ["李四"],
      "lastUploadAt": "2026-04-24T10:00:00Z"
    }
  ]
}
```

### `GET /api/admin/invoices/{invoiceNo}`

获取管理员视角发票详情。响应结构同公开发票详情。

### `DELETE /api/admin/invoices/{invoiceNo}/uploads/{id}`

管理员删除发票上传文件。需要 `X-Admin-Csrf`。

响应：

```json
{
  "ok": true
}
```

### `DELETE /api/admin/invoices/{invoiceNo}`

重置发票上传数据。需要 `X-Admin-Csrf`。

响应：

```json
{
  "ok": true
}
```

### `GET /api/admin/invoices/export.csv`

导出发票列表 CSV。

查询参数：

| 参数 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `q` | string | 否 | 搜索关键词 |
| `onlyUploaded` | boolean | 否 | 仅导出已上传 |

响应：`text/csv; charset=utf-8` 文件流。

CSV 表头：

```text
发票号码,客户,销方,开票日期,已上传,上传数量,录入人,最后上传时间
```

### `GET /api/admin/invoices/export.zip`

导出发票文件 ZIP。

查询参数：

| 参数 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `q` | string | 否 | 搜索关键词 |
| `onlyUploaded` | boolean | 否 | 仅导出已上传 |
| `operator` | string | 否 | 按录入人模糊过滤 |
| `uploadFrom` | string | 否 | 上传开始日期/时间 |
| `uploadTo` | string | 否 | 上传结束日期/时间 |

响应：`application/zip` 文件流。

## 前端页面路由参考

这些不是 API，但便于联调：

| 路径 | 说明 |
| --- | --- |
| `/` | 重定向到 `/y2021` |
| `/y2021` 至 `/y2025` | 各年度订单收集页 |
| `/y2021/{operator}` 至 `/y2025/{operator}` | 带默认录入人的订单收集页 |
| `/invoices` | 发票录入页 |
| `/invoices/{operator}` | 带默认录入人的发票录入页 |
| `/admin/login` | 管理员登录页 |
| `/admin` | 管理后台 |
