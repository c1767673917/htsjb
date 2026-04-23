<script setup lang="ts">
// Invoice detail panel: header (发票号码 + upload pill), read-only meta
// section, line items table, then the upload card for images + PDFs. The
// sticky submit button lives in the parent view so it can remain at the
// bottom of the viewport.
import { computed } from 'vue';
import InvoiceUploadCard from './InvoiceUploadCard.vue';
import type { InvoiceDetail, InvoiceUploadFile } from '@/lib/api';
import type { StagedFile } from '@/stores/invoice';

const props = defineProps<{
  detail: InvoiceDetail;
  staged: StagedFile[];
  currentOperator?: string;
}>();

const emit = defineEmits<{
  (e: 'close'): void;
  (e: 'add', files: File[]): void;
  (e: 'remove-staged', id: string): void;
  (e: 'user-delete', file: InvoiceUploadFile): void;
  (e: 'preview', payload: { src: string; alt: string }): void;
}>();

const pillText = computed(() => {
  const n = props.detail.uploads.length;
  if (n === 0) return '未上传';
  return `已上传 ${n} 个文件`;
});
const pillClass = computed(() =>
  props.detail.uploads.length > 0 ? 'pill' : 'pill pill-muted',
);

function formatAmount(n: number): string {
  return n.toLocaleString('zh-CN', { minimumFractionDigits: 2, maximumFractionDigits: 2 });
}
</script>

<template>
  <article class="detail-panel" :aria-label="`发票 ${detail.invoiceNo} 详情`">
    <header class="detail-header">
      <div>
        <div style="font-weight:600">{{ detail.invoiceNo }}</div>
        <div class="muted" style="font-size: var(--font-sm)">
          购买方：{{ detail.customer || '未知客户' }}
        </div>
      </div>
      <div style="display:flex; gap:8px; align-items:center">
        <span :class="pillClass">{{ pillText }}</span>
        <button class="btn btn-ghost tap" type="button" @click="emit('close')">关闭</button>
      </div>
    </header>

    <div class="invoice-info" style="padding: var(--space-3) var(--space-4); font-size: var(--font-sm)">
      <div><span class="muted">发票号码：</span>{{ detail.invoiceNo }}</div>
      <div><span class="muted">购买方名称：</span>{{ detail.customer }}</div>
      <div><span class="muted">开票日期：</span>{{ detail.invoiceDate }}</div>
      <div><span class="muted">销方名称：</span>{{ detail.seller }}</div>
    </div>

    <div class="detail-meta-scroll" v-if="detail.lines.length > 0">
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
          <tr v-for="(line, i) in detail.lines" :key="`${detail.invoiceNo}-${i}`">
            <td class="truncate" style="max-width: 180px">{{ line.product }}</td>
            <td>{{ line.quantity }}</td>
            <td>{{ formatAmount(line.amount) }}</td>
            <td>{{ formatAmount(line.taxAmount) }}</td>
            <td>{{ formatAmount(line.totalWithTax) }}</td>
            <td>{{ line.taxRate }}</td>
          </tr>
        </tbody>
      </table>
    </div>

    <div class="upload-cards">
      <InvoiceUploadCard
        :server-files="detail.uploads"
        :staged-files="staged"
        @add="(files) => emit('add', files)"
        @remove-staged="(id) => emit('remove-staged', id)"
        @user-delete="(file) => emit('user-delete', file)"
        @preview="(payload) => emit('preview', payload)"
      />
    </div>
  </article>
</template>

<style scoped>
.pill-muted {
  background: #eceef1;
  color: var(--color-text-muted);
}

.invoice-info {
  display: flex;
  flex-direction: column;
  gap: 4px;
  border-bottom: 1px solid var(--color-border);
}
</style>
