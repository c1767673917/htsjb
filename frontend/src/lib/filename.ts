// Client-side preview of the sanitized customer name. The regex mirrors the
// backend rule in architecture §10 (`[\\/:*?"<>|\s]+` → `_`, trim trailing
// `_`, fallback `未知客户`). Both sides MUST produce identical output so the
// UI filename preview never diverges from the name written to disk.

const ILLEGAL_PATTERN = /[\\/:*?"<>|\s]+/g;

export function sanitizeCustomer(input: string | null | undefined): string {
  if (input == null) return '未知客户';
  const replaced = input.replace(ILLEGAL_PATTERN, '_');
  const trimmed = replaced.replace(/^_+|_+$/g, '');
  if (trimmed.length === 0) return '未知客户';
  return trimmed;
}

/**
 * Build the deterministic filename that the backend will write. Used only for
 * display / preview (the backend is the source of truth for the actual name).
 *   {orderNo}-{customerClean}-{kind}-{seq:02}.jpg
 */
export function buildFilename(orderNo: string, customer: string, kind: '合同' | '发票' | '发货单', seq: number): string {
  const cleanCustomer = sanitizeCustomer(customer);
  const padded = seq.toString().padStart(2, '0');
  return `${orderNo}-${cleanCustomer}-${kind}-${padded}.jpg`;
}
