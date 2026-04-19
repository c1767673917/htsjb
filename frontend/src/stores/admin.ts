// Admin store: session state, CSRF token, year/orders browsing, detail
// panel and destructive actions. Every DELETE/POST is required to carry the
// HMAC-derived `X-Admin-Csrf` header returned by `/api/admin/ping`
// (architecture §14).

import { defineStore } from 'pinia';
import { computed, ref } from 'vue';
import {
  ApiError,
  adminApi,
  type AdminOrderList,
  type AdminOrderRow,
  type AdminYearStat,
  type CheckStatus,
  type OrderDetail,
} from '@/lib/api';
import { useUiStore } from './ui';

export interface AdminOrdersFilter {
  onlyUploaded: boolean;
  onlyCsvRemoved: boolean;
  q: string;
  checkStatus: CheckStatus | '';
}

export const useAdminStore = defineStore('admin', () => {
  const authed = ref<boolean>(false);
  const csrfToken = ref<string>('');
  const pingInFlight = ref<boolean>(false);

  const years = ref<AdminYearStat[]>([]);
  const currentYear = ref<number>(2021);
  const filters = ref<AdminOrdersFilter>({ onlyUploaded: false, onlyCsvRemoved: false, q: '', checkStatus: '' });
  const page = ref<number>(1);
  const pageSize = ref<number>(50);
  const orderList = ref<AdminOrderList | null>(null);
  const listLoading = ref<boolean>(false);

  const currentRow = ref<AdminOrderRow | null>(null);
  const currentDetail = ref<OrderDetail | null>(null);
  const detailLoading = ref<boolean>(false);

  const isEmpty = computed(() => !orderList.value || orderList.value.items.length === 0);

  // M-26: year switch / filter change / pagination must cancel any in-flight
  // list fetch so responses from the previous year cannot overwrite the new
  // year's result set. M-27: resetOrder also hands the side panel its own
  // seq counter so a reset cannot be shadowed by a late detail fetch.
  let listAbort: AbortController | null = null;
  let listSeq = 0;
  let detailAbort: AbortController | null = null;
  let detailSeq = 0;
  let cacheBust = 0;

  function stampPhotoUrls(detail: OrderDetail) {
    if (cacheBust === 0) return;
    const suffix = `?_v=${cacheBust}`;
    for (const photos of Object.values(detail.uploads)) {
      for (const p of photos) {
        p.url += suffix;
      }
    }
  }

  /**
   * Probe the session. Returns true when the cookie is still valid. On 401
   * we surface a flag so the router guard can redirect to /admin/login —
   * returning a plain boolean keeps the store router-agnostic.
   */
  async function ping(): Promise<boolean> {
    if (pingInFlight.value) return authed.value;
    pingInFlight.value = true;
    try {
      const resp = await adminApi.ping();
      authed.value = true;
      csrfToken.value = resp.csrfToken;
      return true;
    } catch (err) {
      authed.value = false;
      csrfToken.value = '';
      if (err instanceof ApiError && err.status === 401) {
        return false;
      }
      const ui = useUiStore();
      ui.error(err instanceof ApiError ? err.message : '会话验证失败');
      return false;
    } finally {
      pingInFlight.value = false;
    }
  }

  async function login(password: string): Promise<boolean> {
    const ui = useUiStore();
    try {
      await adminApi.login(password);
      // After a successful login the cookie is set; ping to pick up the
      // fresh csrfToken for subsequent state-changing calls.
      await ping();
      return authed.value;
    } catch (err) {
      ui.error(err instanceof ApiError ? err.message : '登录失败');
      return false;
    }
  }

  async function logout(): Promise<void> {
    try {
      if (csrfToken.value) await adminApi.logout(csrfToken.value);
    } catch {
      /* ignore — we always clear local state below */
    }
    authed.value = false;
    csrfToken.value = '';
    years.value = [];
    orderList.value = null;
    currentRow.value = null;
    currentDetail.value = null;
    if (listAbort) {
      listAbort.abort();
      listAbort = null;
    }
    if (detailAbort) {
      detailAbort.abort();
      detailAbort = null;
    }
  }

  async function loadYears() {
    try {
      years.value = await adminApi.years();
    } catch (err) {
      const ui = useUiStore();
      ui.error(err instanceof ApiError ? err.message : '加载年度统计失败');
    }
  }

  function setYear(y: number) {
    if (y === currentYear.value) return;
    currentYear.value = y;
    page.value = 1;
    orderList.value = null;
    // Abort the previous year's list fetch (M-26) and the open detail
    // fetch; both would arrive with rows from the wrong year.
    if (listAbort) {
      listAbort.abort();
      listAbort = null;
    }
    listSeq += 1;
    closeRow();
  }

  function setFilters(next: Partial<AdminOrdersFilter>) {
    filters.value = { ...filters.value, ...next };
    page.value = 1;
  }

  function setPage(p: number) {
    page.value = Math.max(1, p);
  }

  async function loadOrders() {
    // Cancel the previous in-flight list fetch so a slower response cannot
    // clobber the fresh filters / page / year (M-26).
    if (listAbort) listAbort.abort();
    listAbort = new AbortController();
    const mySeq = ++listSeq;
    const signal = listAbort.signal;
    listLoading.value = true;
    try {
      const resp = await adminApi.orders(
        currentYear.value,
        {
          page: page.value,
          size: pageSize.value,
          onlyUploaded: filters.value.onlyUploaded,
          onlyCsvRemoved: filters.value.onlyCsvRemoved,
          q: filters.value.q,
          checkStatus: filters.value.checkStatus,
        },
        signal,
      );
      if (mySeq !== listSeq) return;
      orderList.value = resp;
    } catch (err) {
      if ((err as Error).name === 'AbortError') return;
      if (mySeq !== listSeq) return;
      const ui = useUiStore();
      ui.error(err instanceof ApiError ? err.message : '加载订单列表失败');
    } finally {
      if (mySeq === listSeq) listLoading.value = false;
    }
  }

  async function openRow(row: AdminOrderRow) {
    currentRow.value = row;
    currentDetail.value = null;
    detailLoading.value = true;
    if (detailAbort) detailAbort.abort();
    detailAbort = new AbortController();
    const mySeq = ++detailSeq;
    const signal = detailAbort.signal;
    try {
      const resp = await adminApi.detail(currentYear.value, row.orderNo, signal);
      if (mySeq !== detailSeq) return;
      if (currentRow.value?.orderNo !== row.orderNo) return;
      stampPhotoUrls(resp);
      currentDetail.value = resp;
    } catch (err) {
      if ((err as Error).name === 'AbortError') return;
      if (mySeq !== detailSeq) return;
      const ui = useUiStore();
      ui.error(err instanceof ApiError ? err.message : '加载订单详情失败');
    } finally {
      if (mySeq === detailSeq) detailLoading.value = false;
    }
  }

  function closeRow() {
    currentRow.value = null;
    currentDetail.value = null;
    detailLoading.value = false;
    if (detailAbort) {
      detailAbort.abort();
      detailAbort = null;
    }
    detailSeq += 1;
  }

  async function deletePhoto(id: number): Promise<boolean> {
    const ui = useUiStore();
    if (!currentRow.value) return false;
    try {
      await adminApi.deletePhoto(currentYear.value, currentRow.value.orderNo, id, csrfToken.value);
      ui.success('已删除');
      cacheBust++;
      await Promise.all([refreshCurrentRow(), loadOrders()]);
      return true;
    } catch (err) {
      ui.error(err instanceof ApiError ? err.message : '删除失败');
      return false;
    }
  }

  async function resetOrder(): Promise<boolean> {
    const ui = useUiStore();
    if (!currentRow.value) return false;
    try {
      await adminApi.resetOrder(currentYear.value, currentRow.value.orderNo, csrfToken.value);
      ui.success('已重置');
      // M-27: the side panel must be cleared *before* refetching the list so
      // the deleted order's thumbnails cannot keep rendering while the list
      // fetch is in flight.
      closeRow();
      await loadOrders();
      return true;
    } catch (err) {
      ui.error(err instanceof ApiError ? err.message : '重置失败');
      return false;
    }
  }

  async function setCheckStatus(row: AdminOrderRow, status: CheckStatus): Promise<boolean> {
    const ui = useUiStore();
    try {
      await adminApi.setCheckStatus(currentYear.value, row.orderNo, status, csrfToken.value);
      if (orderList.value) {
        const target = orderList.value.items.find((it) => it.orderNo === row.orderNo);
        if (target) target.checkStatus = status;
      }
      if (currentRow.value && currentRow.value.orderNo === row.orderNo) {
        currentRow.value.checkStatus = status;
      }
      if (currentDetail.value && currentDetail.value.orderNo === row.orderNo) {
        currentDetail.value.checkStatus = status;
      }
      ui.success(`已标记为${status}`);
      return true;
    } catch (err) {
      ui.error(err instanceof ApiError ? err.message : '更新检查状态失败');
      return false;
    }
  }

  async function rebuildPdf(): Promise<boolean> {
    const ui = useUiStore();
    if (!currentRow.value) return false;
    try {
      const resp = await adminApi.rebuildPdf(
        currentYear.value,
        currentRow.value.orderNo,
        csrfToken.value,
      );
      ui.success(`PDF 已重建（${resp.pages} 页）`);
      return true;
    } catch (err) {
      ui.error(err instanceof ApiError ? err.message : '重建失败');
      return false;
    }
  }

  async function refreshCurrentRow() {
    if (!currentRow.value) return;
    const targetOrderNo = currentRow.value.orderNo;
    const targetYear = currentYear.value;
    if (detailAbort) detailAbort.abort();
    detailAbort = new AbortController();
    const mySeq = ++detailSeq;
    const signal = detailAbort.signal;
    try {
      const resp = await adminApi.detail(targetYear, targetOrderNo, signal);
      if (mySeq !== detailSeq) return;
      if (currentRow.value?.orderNo !== targetOrderNo) return;
      stampPhotoUrls(resp);
      currentDetail.value = resp;
    } catch {
      /* surfaced by loadOrders path */
    }
  }

  return {
    authed,
    csrfToken,
    pingInFlight,
    years,
    currentYear,
    filters,
    page,
    pageSize,
    orderList,
    listLoading,
    currentRow,
    currentDetail,
    detailLoading,
    isEmpty,
    ping,
    login,
    logout,
    loadYears,
    setYear,
    setFilters,
    setPage,
    loadOrders,
    openRow,
    closeRow,
    deletePhoto,
    resetOrder,
    rebuildPdf,
    setCheckStatus,
    refreshCurrentRow,
  };
});
