<script setup lang="ts">
// Back office: year tabs 2021..2025, filter toggles, paginated table, side
// panel for detail. Every destructive action requires a confirm dialog
// (FR-ADMIN-OVERWRITE) and sends the CSRF header via the admin store.
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { storeToRefs } from 'pinia';
import { useRouter } from 'vue-router';
import UploadCard from '@/components/UploadCard.vue';
import { useAdminStore } from '@/stores/admin';
import { adminApi, type AdminOrderRow, type CheckStatus, type UploadedPhoto } from '@/lib/api';
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

const totalPages = computed(() => {
  if (!orderList.value) return 1;
  return Math.max(1, Math.ceil(orderList.value.total / pageSize.value));
});

const cardTitles: Record<UploadKind, string> = {
  合同: '合同',
  发票: '发票',
  发货单: '发货单',
};

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

function mergedPdfHref(): string | null {
  if (!currentRow.value) return null;
  return adminApi.mergedPdfUrl(currentYear.value, currentRow.value.orderNo);
}
function bundleZipHref(): string | null {
  if (!currentRow.value) return null;
  return adminApi.bundleZipUrl(currentYear.value, currentRow.value.orderNo);
}
function yearExportHref(): string {
  return adminApi.yearExportUrl(currentYear.value);
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
  document.removeEventListener('click', closeCheckMenu);
});

function gotoPage(p: number) {
  store.setPage(p);
}
</script>

<template>
  <div class="admin-shell">
    <header class="admin-header">
      <strong style="font-size: var(--font-lg)">后台管理</strong>
      <div style="display:flex; gap: var(--space-2); align-items:center">
        <a class="btn btn-ghost tap" :href="yearExportHref()" download>导出本年 zip</a>
        <button type="button" class="btn btn-ghost tap" @click="onLogout">退出</button>
      </div>
    </header>

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
      <span v-if="orderList" class="muted" style="margin-left:auto; font-size: var(--font-sm)">
        共 {{ orderList.total }} 条
      </span>
    </div>

    <div class="admin-body">
      <section>
        <div v-if="listLoading" class="muted"><span class="spinner" /> 加载中…</div>
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

        <div v-if="detailLoading" class="muted"><span class="spinner" /> 加载详情…</div>
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
  </div>
</template>

<style scoped>
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
</style>
