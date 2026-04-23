<script setup lang="ts">
// Back office: top-level tabs "订单管理" | "发票管理", year tabs 2021..2025
// (orders only), filter toggles, paginated table, side panel for detail.
// Every destructive action requires a confirm dialog (FR-ADMIN-OVERWRITE)
// and sends the CSRF header via the admin store.
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { storeToRefs } from 'pinia';
import { useRouter } from 'vue-router';
import UploadCard from '@/components/UploadCard.vue';
import InvoiceUploadCard from '@/components/InvoiceUploadCard.vue';
import { useAdminStore } from '@/stores/admin';
import {
  adminApi,
  type AdminOrderRow,
  type CheckStatus,
  type InvoiceAdminListItem,
  type InvoiceUploadFile,
  type UploadedPhoto,
} from '@/lib/api';
import { KINDS, type UploadKind } from '@/stores/collection';
import { useUiStore } from '@/stores/ui';

const router = useRouter();
const store = useAdminStore();
const ui = useUiStore();
const {
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
  // Invoice admin
  adminTab,
  invoiceList,
  invoiceListLoading,
  invoicePage,
  invoicePageSize,
  invoiceFilters,
  currentInvoiceRow,
  currentInvoiceDetail,
  invoiceDetailLoading,
} = storeToRefs(store);

onMounted(async () => {
  document.addEventListener('click', closeCheckMenu);
  await store.loadYears();
  await store.loadOrders();
});

watch(currentYear, () => {
  void store.loadOrders();
});

watch(
  () => ({ ...filters.value, page: page.value }),
  () => {
    void store.loadOrders();
  },
  { deep: true },
);

// Invoice list watchers
watch(
  () => ({ ...invoiceFilters.value, page: invoicePage.value }),
  () => {
    if (adminTab.value === 'invoices') {
      void store.loadInvoices();
    }
  },
  { deep: true },
);

const totalPages = computed(() => {
  if (!orderList.value) return 1;
  return Math.max(1, Math.ceil(orderList.value.total / pageSize.value));
});

const invoiceTotalPages = computed(() => {
  if (!invoiceList.value) return 1;
  return Math.max(1, Math.ceil(invoiceList.value.total / invoicePageSize.value));
});

const cardTitles: Record<UploadKind, string> = {
  合同: '合同',
  发票: '发票',
  发货单: '发货单',
};

// --- Tab switching ---

function switchTab(tab: 'orders' | 'invoices') {
  store.setAdminTab(tab);
  if (tab === 'invoices' && !invoiceList.value) {
    void store.loadInvoices();
  }
}

// --- Order admin ---

function pickYear(y: number) {
  store.setYear(y);
}

function onRowClick(row: AdminOrderRow) {
  void store.openRow(row);
}

const checkMenuFor = ref<string | null>(null);

function checkStatusClass(status: CheckStatus): string {
  if (status === '已检查') return 'badge check-badge check-ok';
  if (status === '错误') return 'badge check-badge check-error';
  return 'badge badge-muted check-badge';
}

async function onCheckClick(row: AdminOrderRow) {
  void store.openRow(row);
  if (row.checkStatus === '未检查') {
    await store.setCheckStatus(row, '已检查');
    checkMenuFor.value = null;
  } else {
    checkMenuFor.value = checkMenuFor.value === row.orderNo ? null : row.orderNo;
  }
}

async function pickCheckStatus(row: AdminOrderRow, status: CheckStatus) {
  checkMenuFor.value = null;
  if (row.checkStatus === status) return;
  await store.setCheckStatus(row, status);
}

function closeCheckMenu() {
  checkMenuFor.value = null;
}

function otherStatuses(current: CheckStatus): CheckStatus[] {
  return (['未检查', '已检查', '错误'] as CheckStatus[]).filter((s) => s !== current);
}

function formatDate(ts: string | null): string {
  if (!ts) return '—';
  const d = new Date(ts);
  if (Number.isNaN(d.getTime())) return ts;
  return d.toLocaleString('zh-CN', { hour12: false });
}

function countsText(row: AdminOrderRow): string {
  const c = row.counts;
  return `合同 ${c.合同} / 发票 ${c.发票} / 发货单 ${c.发货单}`;
}

function operatorsText(row: AdminOrderRow): string {
  if (!row.operators || row.operators.length === 0) return '—';
  return row.operators.join('、');
}

function invoiceOperatorsText(row: InvoiceAdminListItem): string {
  if (!row.operators || row.operators.length === 0) return '—';
  return row.operators.join('、');
}

function mergedPdfHref(): string | null {
  if (!currentRow.value) return null;
  return adminApi.mergedPdfUrl(currentYear.value, currentRow.value.orderNo);
}
function bundleZipHref(): string | null {
  if (!currentRow.value) return null;
  return adminApi.bundleZipUrl(currentYear.value, currentRow.value.orderNo);
}
const showExportDialog = ref(false);
const exportOperator = ref('');
const exportUploadFrom = ref('');
const exportUploadTo = ref('');

function openExportDialog() {
  exportOperator.value = '';
  exportUploadFrom.value = '';
  exportUploadTo.value = '';
  showExportDialog.value = true;
}

function closeExportDialog() {
  showExportDialog.value = false;
}

function yearExportHref(): string {
  return adminApi.yearExportUrl(currentYear.value, {
    operator: exportOperator.value || undefined,
    uploadFrom: exportUploadFrom.value || undefined,
    uploadTo: exportUploadTo.value || undefined,
  });
}

function csvExportHref(): string {
  return adminApi.csvExportUrl(currentYear.value, {
    onlyUploaded: filters.value.onlyUploaded || undefined,
    onlyCsvRemoved: filters.value.onlyCsvRemoved || undefined,
    q: filters.value.q || undefined,
    checkStatus: filters.value.checkStatus || undefined,
  });
}

function invoiceCsvExportHref(): string {
  return adminApi.invoiceCsvExportUrl({
    q: invoiceFilters.value.q || undefined,
    onlyUploaded: invoiceFilters.value.onlyUploaded || undefined,
  });
}

async function onDeletePhoto(photo: UploadedPhoto) {
  if (!currentRow.value) return;
  if (!window.confirm(`确定删除该照片？\n文件：${photo.filename}`)) return;
  await store.deletePhoto(photo.id);
}

async function onReset() {
  if (!currentRow.value) return;
  if (!window.confirm(`确定重置 ${currentRow.value.orderNo}？此操作将删除该单号的全部照片与 PDF，且不可撤销。`)) {
    return;
  }
  await store.resetOrder();
}

async function onRebuild() {
  if (!currentRow.value) return;
  await store.rebuildPdf();
}

async function onLogout() {
  await store.logout();
  ui.success('已退出');
  await router.replace('/admin/login');
}

function onToggleUploaded(e: Event) {
  store.setFilters({ onlyUploaded: (e.target as HTMLInputElement).checked });
}
function onToggleCsvRemoved(e: Event) {
  store.setFilters({ onlyCsvRemoved: (e.target as HTMLInputElement).checked });
}
function onCheckStatusChange(e: Event) {
  const v = (e.target as HTMLSelectElement).value as CheckStatus | '';
  store.setFilters({ checkStatus: v });
}

const queryInput = ref<string>(filters.value.q ?? '');
let queryTimer: ReturnType<typeof setTimeout> | null = null;

watch(
  () => filters.value.q,
  (v) => {
    if ((v ?? '') !== queryInput.value) queryInput.value = v ?? '';
  },
);

function commitQuery(v: string) {
  if ((filters.value.q ?? '') !== v) store.setFilters({ q: v });
}

function onQueryInput(e: Event) {
  const v = (e.target as HTMLInputElement).value;
  queryInput.value = v;
  if (queryTimer) clearTimeout(queryTimer);
  queryTimer = setTimeout(() => commitQuery(v.trim()), 250);
}

function onQueryClear() {
  if (queryTimer) clearTimeout(queryTimer);
  queryInput.value = '';
  commitQuery('');
}

onBeforeUnmount(() => {
  if (queryTimer) clearTimeout(queryTimer);
  if (invQueryTimer) clearTimeout(invQueryTimer);
  document.removeEventListener('click', closeCheckMenu);
});

function gotoPage(p: number) {
  store.setPage(p);
}

// --- Invoice admin ---

function onInvoiceRowClick(row: InvoiceAdminListItem) {
  void store.openInvoiceRow(row);
}

async function onDeleteInvoiceUpload(file: InvoiceUploadFile) {
  if (!currentInvoiceRow.value) return;
  if (!window.confirm(`确定删除该文件？\n文件：${file.filename}`)) return;
  await store.deleteInvoiceUpload(file.id);
}

async function onResetInvoice() {
  if (!currentInvoiceRow.value) return;
  if (!window.confirm(`确定重置 ${currentInvoiceRow.value.invoiceNo}？此操作将删除该发票的全部上传文件，且不可撤销。`)) {
    return;
  }
  await store.resetInvoice();
}

function onInvoiceToggleUploaded(e: Event) {
  store.setInvoiceFilters({ onlyUploaded: (e.target as HTMLInputElement).checked });
}

const invQueryInput = ref<string>(invoiceFilters.value.q ?? '');
let invQueryTimer: ReturnType<typeof setTimeout> | null = null;

watch(
  () => invoiceFilters.value.q,
  (v) => {
    if ((v ?? '') !== invQueryInput.value) invQueryInput.value = v ?? '';
  },
);

function commitInvQuery(v: string) {
  if ((invoiceFilters.value.q ?? '') !== v) store.setInvoiceFilters({ q: v });
}

function onInvQueryInput(e: Event) {
  const v = (e.target as HTMLInputElement).value;
  invQueryInput.value = v;
  if (invQueryTimer) clearTimeout(invQueryTimer);
  invQueryTimer = setTimeout(() => commitInvQuery(v.trim()), 250);
}

function onInvQueryClear() {
  if (invQueryTimer) clearTimeout(invQueryTimer);
  invQueryInput.value = '';
  commitInvQuery('');
}

function gotoInvoicePage(p: number) {
  store.setInvoicePage(p);
}

function isInvoiceImage(contentType: string): boolean {
  return contentType.startsWith('image/');
}

function formatAmount(n: number): string {
  return n.toLocaleString('zh-CN', { minimumFractionDigits: 2, maximumFractionDigits: 2 });
}
</script>

<template>
  <div class="admin-shell">
    <header class="admin-header">
      <strong style="font-size: var(--font-lg)">后台管理</strong>
      <div style="display:flex; gap: var(--space-2); align-items:center">
        <template v-if="adminTab === 'orders'">
          <a class="btn btn-ghost tap" :href="csvExportHref()" download>导出 CSV</a>
          <button type="button" class="btn btn-ghost tap" @click="openExportDialog">导出本年 zip</button>
        </template>
        <template v-else>
          <a class="btn btn-ghost tap" :href="invoiceCsvExportHref()" download>导出 CSV</a>
        </template>
        <button type="button" class="btn btn-ghost tap" @click="onLogout">退出</button>
      </div>
    </header>

    <!-- Top-level module tabs -->
    <nav class="module-tabs" aria-label="模块切换">
      <button
        :class="['module-tab', adminTab === 'orders' ? 'active' : '']"
        type="button"
        @click="switchTab('orders')"
      >
        订单管理
      </button>
      <button
        :class="['module-tab', adminTab === 'invoices' ? 'active' : '']"
        type="button"
        @click="switchTab('invoices')"
      >
        发票管理
      </button>
    </nav>

    <!-- ======== Orders Tab ======== -->
    <template v-if="adminTab === 'orders'">
      <nav class="admin-tabs" aria-label="年度">
        <button
          v-for="y in [2021, 2022, 2023, 2024, 2025]"
          :key="y"
          :class="['admin-tab', currentYear === y ? 'active' : '']"
          type="button"
          @click="pickYear(y)"
        >
          {{ y }}
          <span
            v-if="years.find((s) => s.year === y)"
            style="margin-left:4px; opacity:.8"
          >
            ({{ years.find((s) => s.year === y)?.uploaded }}/{{ years.find((s) => s.year === y)?.total }})
          </span>
        </button>
      </nav>

      <div class="admin-filters">
        <div class="admin-search">
          <input
            class="input"
            type="search"
            inputmode="search"
            autocomplete="off"
            autocapitalize="off"
            spellcheck="false"
            placeholder="搜索录入人 / 单据编号 / 客户"
            :value="queryInput"
            @input="onQueryInput"
          />
          <button
            v-if="queryInput.length > 0"
            type="button"
            class="clear-btn"
            aria-label="清空搜索"
            @click="onQueryClear"
          >
            ×
          </button>
        </div>
        <label><input type="checkbox" :checked="filters.onlyUploaded" @change="onToggleUploaded" /> 仅看已上传</label>
        <label><input type="checkbox" :checked="filters.onlyCsvRemoved" @change="onToggleCsvRemoved" /> 仅看 CSV 已移除</label>
        <label class="admin-select-label">
          检查状态
          <select class="input admin-status-select" :value="filters.checkStatus" @change="onCheckStatusChange">
            <option value="">全部</option>
            <option value="未检查">未检查</option>
            <option value="已检查">已检查</option>
            <option value="错误">错误</option>
          </select>
        </label>
        <span v-if="orderList" class="muted" style="margin-left:auto; font-size: var(--font-sm)">
          共 {{ orderList.total }} 条
        </span>
      </div>

      <div class="admin-body">
        <section>
          <div v-if="listLoading" class="muted"><span class="spinner" /> 加载中...</div>
          <table v-else-if="orderList && orderList.items.length > 0" class="admin-table">
            <thead>
              <tr>
                <th>单据编号</th>
                <th>客户</th>
                <th>已上传</th>
                <th>合同/发票/发货单</th>
                <th>录入人</th>
                <th>最后上传</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="row in orderList.items"
                :key="row.orderNo"
                :class="currentRow?.orderNo === row.orderNo ? 'selected' : ''"
                @click="onRowClick(row)"
              >
                <td>{{ row.orderNo }}</td>
                <td class="truncate" style="max-width: 180px">{{ row.customer || '未知客户' }}</td>
                <td>
                  <span :class="row.uploaded ? 'badge' : 'badge badge-muted'">
                    {{ row.uploaded ? '是' : '否' }}
                  </span>
                  <span v-if="row.csvRemoved" class="badge badge-muted" style="margin-left:4px">CSV 已移除</span>
                </td>
                <td>{{ countsText(row) }}</td>
                <td class="truncate" style="max-width: 140px">{{ operatorsText(row) }}</td>
                <td>{{ formatDate(row.lastUploadAt) }}</td>
                <td>
                  <div class="row-actions" @click.stop>
                    <button type="button" class="btn btn-ghost tap" @click="onRowClick(row)">查看</button>
                    <div class="check-action">
                      <button
                        type="button"
                        :class="['btn', 'tap', 'check-btn', row.checkStatus !== '未检查' ? 'btn-ghost' : 'btn-primary']"
                        @click="onCheckClick(row)"
                      >
                        <span :class="checkStatusClass(row.checkStatus)">{{ row.checkStatus }}</span>
                      </button>
                      <div v-if="checkMenuFor === row.orderNo" class="check-menu" role="menu">
                        <button
                          v-for="s in otherStatuses(row.checkStatus)"
                          :key="s"
                          type="button"
                          class="check-menu-item"
                          role="menuitem"
                          @click="pickCheckStatus(row, s)"
                        >
                          改为{{ s }}
                        </button>
                      </div>
                    </div>
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
          <p v-else class="muted">没有符合条件的订单</p>

          <nav class="pagination" v-if="orderList && totalPages > 1" aria-label="分页">
            <button class="btn btn-ghost tap" type="button" :disabled="page <= 1" @click="gotoPage(page - 1)">上一页</button>
            <span>第 {{ page }} / {{ totalPages }} 页</span>
            <button class="btn btn-ghost tap" type="button" :disabled="page >= totalPages" @click="gotoPage(page + 1)">下一页</button>
          </nav>
        </section>

        <aside v-if="currentRow" class="admin-side-panel" aria-label="订单详情">
          <header style="display:flex; justify-content:space-between; align-items:center; gap:var(--space-3)">
            <div>
              <div style="font-weight:600; display:flex; align-items:center; gap:8px">
                <span>{{ currentRow.orderNo }}</span>
                <span :class="checkStatusClass(currentRow.checkStatus)">{{ currentRow.checkStatus }}</span>
              </div>
              <div class="muted" style="font-size: var(--font-sm)">{{ currentRow.customer || '未知客户' }}</div>
            </div>
            <button type="button" class="btn btn-ghost tap" @click="store.closeRow()">关闭</button>
          </header>

          <div style="display:flex; gap:var(--space-2); flex-wrap:wrap">
            <a class="btn btn-ghost tap" :href="mergedPdfHref() ?? '#'" target="_blank" rel="noopener">下载合并 PDF</a>
            <a class="btn btn-ghost tap" :href="bundleZipHref() ?? '#'" download>下载所有原图 zip</a>
            <button type="button" class="btn btn-ghost tap" @click="onRebuild">重建 PDF</button>
            <button type="button" class="btn btn-danger tap" @click="onReset">重置此单号</button>
          </div>

          <div v-if="detailLoading" class="muted"><span class="spinner" /> 加载详情...</div>
          <template v-else-if="currentDetail">
            <div class="detail-meta-scroll">
              <table class="detail-meta-table" aria-label="订单明细">
                <thead>
                  <tr>
                    <th>单据编号</th>
                    <th>客户</th>
                    <th>产品名称</th>
                    <th>数量</th>
                    <th>价税合计</th>
                    <th>发票号</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="(line, i) in currentDetail.lines" :key="`${currentDetail.orderNo}-${i}`">
                    <td>{{ line.orderNo }}</td>
                    <td class="truncate" style="max-width:140px">{{ line.customer }}</td>
                    <td class="truncate" style="max-width:160px">{{ line.product }}</td>
                    <td>{{ line.quantity }}</td>
                    <td>{{ line.totalWithTax }}</td>
                    <td>{{ line.invoiceNo }}</td>
                  </tr>
                </tbody>
              </table>
            </div>

            <UploadCard
              v-for="k in KINDS"
              :key="k"
              :kind="k"
              :title="cardTitles[k]"
              :server-photos="currentDetail.uploads[k]"
              :staged-photos="[]"
              :read-only="true"
              :admin-delete="true"
              @admin-delete="onDeletePhoto"
            />
          </template>
        </aside>
      </div>
    </template>

    <!-- ======== Invoices Tab ======== -->
    <template v-if="adminTab === 'invoices'">
      <div class="admin-filters">
        <div class="admin-search">
          <input
            class="input"
            type="search"
            inputmode="search"
            autocomplete="off"
            autocapitalize="off"
            spellcheck="false"
            placeholder="搜索发票号码 / 客户"
            :value="invQueryInput"
            @input="onInvQueryInput"
          />
          <button
            v-if="invQueryInput.length > 0"
            type="button"
            class="clear-btn"
            aria-label="清空搜索"
            @click="onInvQueryClear"
          >
            ×
          </button>
        </div>
        <label><input type="checkbox" :checked="invoiceFilters.onlyUploaded" @change="onInvoiceToggleUploaded" /> 仅看已上传</label>
        <span v-if="invoiceList" class="muted" style="margin-left:auto; font-size: var(--font-sm)">
          共 {{ invoiceList.total }} 条
        </span>
      </div>

      <div class="admin-body">
        <section>
          <div v-if="invoiceListLoading" class="muted"><span class="spinner" /> 加载中...</div>
          <table v-else-if="invoiceList && invoiceList.items.length > 0" class="admin-table">
            <thead>
              <tr>
                <th>发票号码</th>
                <th>客户</th>
                <th>开票日期</th>
                <th>上传数</th>
                <th>录入人</th>
                <th>最后上传</th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="row in invoiceList.items"
                :key="row.invoiceNo"
                :class="currentInvoiceRow?.invoiceNo === row.invoiceNo ? 'selected' : ''"
                @click="onInvoiceRowClick(row)"
              >
                <td>{{ row.invoiceNo }}</td>
                <td class="truncate" style="max-width: 180px">{{ row.customer || '未知客户' }}</td>
                <td>{{ row.invoiceDate }}</td>
                <td>
                  <span :class="row.uploaded ? 'badge' : 'badge badge-muted'">
                    {{ row.uploaded ? row.uploadCount : 0 }}
                  </span>
                </td>
                <td class="truncate" style="max-width: 140px">{{ invoiceOperatorsText(row) }}</td>
                <td>{{ formatDate(row.lastUploadAt) }}</td>
              </tr>
            </tbody>
          </table>
          <p v-else class="muted">没有符合条件的发票</p>

          <nav class="pagination" v-if="invoiceList && invoiceTotalPages > 1" aria-label="分页">
            <button class="btn btn-ghost tap" type="button" :disabled="invoicePage <= 1" @click="gotoInvoicePage(invoicePage - 1)">上一页</button>
            <span>第 {{ invoicePage }} / {{ invoiceTotalPages }} 页</span>
            <button class="btn btn-ghost tap" type="button" :disabled="invoicePage >= invoiceTotalPages" @click="gotoInvoicePage(invoicePage + 1)">下一页</button>
          </nav>
        </section>

        <aside v-if="currentInvoiceRow" class="admin-side-panel" aria-label="发票详情">
          <header style="display:flex; justify-content:space-between; align-items:center; gap:var(--space-3)">
            <div>
              <div style="font-weight:600">{{ currentInvoiceRow.invoiceNo }}</div>
              <div class="muted" style="font-size: var(--font-sm)">{{ currentInvoiceRow.customer || '未知客户' }}</div>
            </div>
            <button type="button" class="btn btn-ghost tap" @click="store.closeInvoiceRow()">关闭</button>
          </header>

          <div style="display:flex; gap:var(--space-2); flex-wrap:wrap">
            <button type="button" class="btn btn-danger tap" @click="onResetInvoice">重置此发票</button>
          </div>

          <div v-if="invoiceDetailLoading" class="muted"><span class="spinner" /> 加载详情...</div>
          <template v-else-if="currentInvoiceDetail">
            <div style="font-size: var(--font-sm); display:flex; flex-direction:column; gap:4px">
              <div><span class="muted">发票号码：</span>{{ currentInvoiceDetail.invoiceNo }}</div>
              <div><span class="muted">购买方名称：</span>{{ currentInvoiceDetail.customer }}</div>
              <div><span class="muted">开票日期：</span>{{ currentInvoiceDetail.invoiceDate }}</div>
              <div><span class="muted">销方名称：</span>{{ currentInvoiceDetail.seller }}</div>
            </div>

            <div class="detail-meta-scroll" v-if="currentInvoiceDetail.lines.length > 0">
              <table class="detail-meta-table" aria-label="发票明细">
                <thead>
                  <tr>
                    <th>货物名称</th>
                    <th>数量</th>
                    <th>金额</th>
                    <th>税额</th>
                    <th>价税合计</th>
                    <th>税率</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="(line, i) in currentInvoiceDetail.lines" :key="`inv-${currentInvoiceDetail.invoiceNo}-${i}`">
                    <td class="truncate" style="max-width:180px">{{ line.product }}</td>
                    <td>{{ line.quantity }}</td>
                    <td>{{ formatAmount(line.amount) }}</td>
                    <td>{{ formatAmount(line.taxAmount) }}</td>
                    <td>{{ formatAmount(line.totalWithTax) }}</td>
                    <td>{{ line.taxRate }}</td>
                  </tr>
                </tbody>
              </table>
            </div>

            <InvoiceUploadCard
              :server-files="currentInvoiceDetail.uploads"
              :staged-files="[]"
              :read-only="true"
              :admin-delete="true"
              @admin-delete="onDeleteInvoiceUpload"
            />
          </template>
        </aside>
      </div>
    </template>

    <teleport to="body">
      <div v-if="showExportDialog" class="export-overlay" @click.self="closeExportDialog">
        <div class="export-dialog">
          <h3 style="margin:0 0 var(--space-3)">导出本年 ZIP</h3>
          <label class="export-field">
            <span>录入人</span>
            <input class="input" type="text" v-model="exportOperator" placeholder="留空则不筛选" />
          </label>
          <label class="export-field">
            <span>最后上传时间（起）</span>
            <input class="input" type="date" v-model="exportUploadFrom" />
          </label>
          <label class="export-field">
            <span>最后上传时间（止）</span>
            <input class="input" type="date" v-model="exportUploadTo" />
          </label>
          <div style="display:flex; gap:var(--space-2); justify-content:flex-end; margin-top:var(--space-3)">
            <button type="button" class="btn btn-ghost tap" @click="closeExportDialog">取消</button>
            <a class="btn btn-primary tap" :href="yearExportHref()" download @click="closeExportDialog">开始导出</a>
          </div>
        </div>
      </div>
    </teleport>
  </div>
</template>

<style scoped>
.module-tabs {
  display: flex;
  gap: 0;
  background: var(--color-surface);
  border-bottom: 1px solid var(--color-border);
}
.module-tab {
  flex: 1;
  padding: 10px 16px;
  font-weight: 600;
  font-size: var(--font-md);
  color: var(--color-text-muted);
  background: transparent;
  border: none;
  border-bottom: 3px solid transparent;
  cursor: pointer;
  transition: color 0.15s, border-color 0.15s;
  text-align: center;
}
.module-tab.active {
  color: var(--color-primary);
  border-bottom-color: var(--color-primary);
}
.module-tab:hover:not(.active) {
  color: var(--color-text);
}

.admin-search {
  position: relative;
  flex: 1 1 220px;
  min-width: 180px;
  max-width: 320px;
}
.admin-search .input {
  padding-right: 32px;
}
.admin-search .clear-btn {
  position: absolute;
  right: 4px;
  top: 50%;
  transform: translateY(-50%);
  width: 28px;
  height: 28px;
  border-radius: 50%;
  background: transparent;
  color: var(--color-text-muted);
  font-size: 18px;
  display: flex;
  align-items: center;
  justify-content: center;
}

.admin-select-label {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}
.admin-status-select {
  width: auto;
  min-width: 96px;
  padding: 4px 8px;
  height: auto;
}

.row-actions {
  display: inline-flex;
  gap: 6px;
  align-items: center;
}
.check-action {
  position: relative;
}
.check-btn {
  padding: 4px 10px;
  min-height: 32px;
}
.check-badge {
  font-size: var(--font-sm);
  line-height: 1.2;
}
.check-ok {
  background: #d4f8d4;
  color: #166534;
}
.check-error {
  background: #fde2e2;
  color: #991b1b;
}
.check-menu {
  position: absolute;
  right: 0;
  top: calc(100% + 4px);
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-md);
  box-shadow: var(--shadow-sm);
  min-width: 120px;
  z-index: 20;
  padding: 4px;
  display: flex;
  flex-direction: column;
}
.check-menu-item {
  padding: 8px 12px;
  background: transparent;
  border: none;
  text-align: left;
  font-size: var(--font-sm);
  color: var(--color-text);
  border-radius: var(--radius-sm);
  cursor: pointer;
}
.check-menu-item:hover {
  background: var(--color-surface-alt);
}

.export-overlay {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.4);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 100;
}
.export-dialog {
  background: var(--color-surface);
  border-radius: var(--radius-md);
  padding: var(--space-4);
  min-width: 320px;
  max-width: 400px;
  box-shadow: var(--shadow-lg, 0 8px 32px rgba(0,0,0,.2));
}
.export-field {
  display: flex;
  flex-direction: column;
  gap: 4px;
  margin-bottom: var(--space-2);
  font-size: var(--font-sm);
}
</style>
