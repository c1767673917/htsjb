<script setup lang="ts">
// Back office: year tabs 2021..2025, filter toggles, paginated table, side
// panel for detail. Every destructive action requires a confirm dialog
// (FR-ADMIN-OVERWRITE) and sends the CSRF header via the admin store.
import { computed, onMounted, watch } from 'vue';
import { storeToRefs } from 'pinia';
import { useRouter } from 'vue-router';
import UploadCard from '@/components/UploadCard.vue';
import { useAdminStore } from '@/stores/admin';
import { adminApi, type AdminOrderRow, type UploadedPhoto } from '@/lib/api';
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
              <td>{{ formatDate(row.lastUploadAt) }}</td>
              <td>
                <button type="button" class="btn btn-ghost tap" @click.stop="onRowClick(row)">查看</button>
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
            <div style="font-weight:600">{{ currentRow.orderNo }}</div>
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
