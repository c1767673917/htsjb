// Thin wrapper around the native fetch API. Keeps the request-building
// boilerplate in one place (credentials, JSON decoding, typed errors) so
// every view and store can call the backend contract described in
// architecture §6 with consistent error handling.

export interface ApiErrorPayload {
  code: string;
  message: string;
}

export class ApiError extends Error {
  public readonly status: number;
  public readonly code: string;
  public readonly payload: unknown;

  constructor(status: number, code: string, message: string, payload?: unknown) {
    super(message);
    this.status = status;
    this.code = code;
    this.payload = payload;
  }
}

export type HttpMethod = 'GET' | 'POST' | 'DELETE' | 'PUT' | 'PATCH';

export interface RequestOptions {
  method?: HttpMethod;
  query?: Record<string, string | number | boolean | undefined | null>;
  body?: unknown;
  headers?: Record<string, string>;
  /** Attach Pinia admin store CSRF token on state-changing admin calls. */
  adminCsrf?: string;
  /** If true, the response body is returned as a Blob (file download). */
  asBlob?: boolean;
  /** If true, the response is a multipart form. body must be FormData. */
  multipart?: boolean;
  /** Abort signal for cancellation (used by debounced search). */
  signal?: AbortSignal;
}

type On401Handler = (() => void) | null;
let on401Handler: On401Handler = null;

/**
 * Register a global 401 handler. The router guard sets this so every
 * `/api/admin/*` 401 can redirect to `/admin/login` no matter which store
 * initiated the request.
 */
export function setOn401Handler(handler: On401Handler): void {
  on401Handler = handler;
}

function buildUrl(path: string, query?: RequestOptions['query']): string {
  if (!query) return path;
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(query)) {
    if (value === undefined || value === null) continue;
    params.append(key, String(value));
  }
  const qs = params.toString();
  return qs ? `${path}?${qs}` : path;
}

/**
 * Map server error codes to friendly Chinese strings. Only codes that the
 * server emits verbatim but whose raw `message` is not operator-friendly
 * belong here (e.g. 503 SERVER_BUSY); everything else falls back to the
 * server-provided message.
 */
function friendlyMessage(status: number, code: string, serverMsg: string): string {
  if (status === 503 || code === 'SERVER_BUSY') {
    return '服务器繁忙，请稍后再试';
  }
  if (status === 429 || code === 'RATE_LIMITED' || code === 'TOO_MANY_REQUESTS') {
    return '操作过于频繁，请稍后再试';
  }
  return serverMsg;
}

async function parseError(resp: Response): Promise<ApiError> {
  const status = resp.status;
  let code = 'HTTP_ERROR';
  let message = `HTTP ${status}`;
  let payload: unknown = undefined;
  const contentType = resp.headers.get('Content-Type') ?? '';
  try {
    if (contentType.includes('application/json')) {
      payload = await resp.json();
      const p = payload as { error?: ApiErrorPayload };
      if (p && p.error) {
        code = p.error.code || code;
        message = p.error.message || message;
      }
    } else {
      const text = await resp.text();
      if (text) message = text.slice(0, 300);
    }
  } catch {
    /* ignore body-parse errors */
  }
  message = friendlyMessage(status, code, message);
  return new ApiError(status, code, message, payload);
}

/** Generic request helper. */
export async function request<T = unknown>(path: string, options: RequestOptions = {}): Promise<T> {
  const {
    method = 'GET',
    query,
    body,
    headers = {},
    adminCsrf,
    asBlob,
    multipart,
    signal,
  } = options;

  const finalHeaders: Record<string, string> = { ...headers };
  if (adminCsrf) finalHeaders['X-Admin-Csrf'] = adminCsrf;

  let finalBody: BodyInit | undefined = undefined;
  if (body !== undefined) {
    if (multipart && body instanceof FormData) {
      finalBody = body;
      // Do not set Content-Type; the browser picks the multipart boundary.
    } else if (body instanceof FormData) {
      finalBody = body;
    } else {
      finalBody = JSON.stringify(body);
      if (!finalHeaders['Content-Type']) {
        finalHeaders['Content-Type'] = 'application/json';
      }
    }
  }

  const url = buildUrl(path, query);
  const resp = await fetch(url, {
    method,
    headers: finalHeaders,
    body: finalBody,
    credentials: 'same-origin',
    signal,
  });

  if (resp.status === 401 && path.startsWith('/api/admin/') && on401Handler) {
    try {
      on401Handler();
    } catch {
      /* ignore handler errors */
    }
  }

  if (!resp.ok) {
    throw await parseError(resp);
  }

  if (asBlob) {
    return (await resp.blob()) as unknown as T;
  }
  if (resp.status === 204) {
    return undefined as unknown as T;
  }
  const ct = resp.headers.get('Content-Type') ?? '';
  if (ct.includes('application/json')) {
    return (await resp.json()) as T;
  }
  return (await resp.text()) as unknown as T;
}

/* Typed response shapes that mirror architecture §6 so call sites stay honest. */

export interface ProgressShape {
  total: number;
  uploaded: number;
  percent: number;
}

export interface SearchItem {
  orderNo: string;
  customer: string;
  uploaded: boolean;
  counts: { 合同: number; 发票: number; 发货单: number };
  csvPresent: boolean;
}

export interface OrderLine {
  orderNo: string;
  customer: string;
  date: string;
  product: string;
  quantity: number;
  amount: number;
  totalWithTax: number;
  taxRate: number;
  invoiceNo: string;
}

export interface UploadedPhoto {
  id: number;
  seq: number;
  filename: string;
  url: string;
  size: number;
}

export interface OrderDetail {
  orderNo: string;
  year: number;
  customer: string;
  csvPresent: boolean;
  lines: OrderLine[];
  uploads: {
    合同: UploadedPhoto[];
    发票: UploadedPhoto[];
    发货单: UploadedPhoto[];
  };
}

export interface SubmitResponse {
  counts: { 合同: number; 发票: number; 发货单: number };
  progress: ProgressShape;
  mergedPdfStale: boolean;
}

export interface AdminYearStat {
  year: number;
  total: number;
  uploaded: number;
  csvRemoved: number;
}

export interface AdminOrderRow {
  orderNo: string;
  customer: string;
  uploaded: boolean;
  counts: { 合同: number; 发票: number; 发货单: number };
  lastUploadAt: string | null;
  csvRemoved: boolean;
  mergedPdfStale?: boolean;
}

export interface AdminOrderList {
  page: number;
  size: number;
  total: number;
  items: AdminOrderRow[];
}

export interface AdminPingResponse {
  ok: true;
  csrfToken: string;
}

/* Collection endpoints (public). */

export const collectionApi = {
  progress(year: number, signal?: AbortSignal) {
    return request<ProgressShape>(`/api/y/${year}/progress`, { signal });
  },
  async search(year: number, q: string, limit = 20, signal?: AbortSignal) {
    const res = await request<{ items: SearchItem[] }>(`/api/y/${year}/search`, {
      query: { q, limit },
      signal,
    });
    return res.items;
  },
  detail(year: number, orderNo: string, signal?: AbortSignal) {
    const enc = encodeURIComponent(orderNo);
    return request<OrderDetail>(`/api/y/${year}/orders/${enc}`, { signal });
  },
  submit(year: number, orderNo: string, form: FormData) {
    const enc = encodeURIComponent(orderNo);
    return request<SubmitResponse>(`/api/y/${year}/orders/${enc}/uploads`, {
      method: 'POST',
      body: form,
      multipart: true,
    });
  },
};

/* Admin endpoints. Every state-changing call requires the CSRF token from
   /api/admin/ping to be echoed in X-Admin-Csrf (architecture §14). */

export const adminApi = {
  ping() {
    return request<AdminPingResponse>('/api/admin/ping');
  },
  login(password: string) {
    return request<{ ok: true }>('/api/admin/login', {
      method: 'POST',
      body: { password },
    });
  },
  logout(csrf: string) {
    return request<{ ok: true }>('/api/admin/logout', {
      method: 'POST',
      adminCsrf: csrf,
    });
  },
  years() {
    return request<AdminYearStat[]>('/api/admin/years');
  },
  orders(
    year: number,
    params: { page: number; size: number; onlyUploaded?: boolean; onlyCsvRemoved?: boolean },
    signal?: AbortSignal,
  ) {
    return request<AdminOrderList>(`/api/admin/${year}/orders`, {
      query: {
        page: params.page,
        size: params.size,
        onlyUploaded: params.onlyUploaded ? true : undefined,
        onlyCsvRemoved: params.onlyCsvRemoved ? true : undefined,
      },
      signal,
    });
  },
  detail(year: number, orderNo: string, signal?: AbortSignal) {
    const enc = encodeURIComponent(orderNo);
    return request<OrderDetail>(`/api/admin/${year}/orders/${enc}`, { signal });
  },
  mergedPdfUrl(year: number, orderNo: string) {
    return `/api/admin/${year}/orders/${encodeURIComponent(orderNo)}/merged.pdf`;
  },
  bundleZipUrl(year: number, orderNo: string) {
    return `/api/admin/${year}/orders/${encodeURIComponent(orderNo)}/bundle.zip`;
  },
  yearExportUrl(year: number) {
    return `/api/admin/${year}/export.zip`;
  },
  deletePhoto(year: number, orderNo: string, id: number, csrf: string) {
    const enc = encodeURIComponent(orderNo);
    return request<{ ok: true; mergedPdfStale?: boolean }>(
      `/api/admin/${year}/orders/${enc}/uploads/${id}`,
      { method: 'DELETE', adminCsrf: csrf },
    );
  },
  resetOrder(year: number, orderNo: string, csrf: string) {
    const enc = encodeURIComponent(orderNo);
    return request<{ ok: true }>(`/api/admin/${year}/orders/${enc}`, {
      method: 'DELETE',
      adminCsrf: csrf,
    });
  },
  rebuildPdf(year: number, orderNo: string, csrf: string) {
    const enc = encodeURIComponent(orderNo);
    return request<{ ok: true; pages: number }>(
      `/api/admin/${year}/orders/${enc}/rebuild-pdf`,
      { method: 'POST', adminCsrf: csrf },
    );
  },
};
