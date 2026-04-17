<script setup lang="ts">
// Order detail panel: header (单据编号 + progress pill), read-only meta
// table of every CSV line for this 单据编号, then three upload cards in the
// exact required order — 合同 / 发票 / 发货单. The sticky submit button
// lives in the parent view (CollectionView) so it can remain at the bottom
// of the viewport regardless of how long this panel scrolls.
import { computed } from 'vue';
import UploadCard from './UploadCard.vue';
import type { OrderDetail, UploadedPhoto } from '@/lib/api';
import { KINDS, type StagedPhoto, type UploadKind } from '@/stores/collection';

const props = defineProps<{
  detail: OrderDetail;
  staged: Record<UploadKind, StagedPhoto[]>;
  mergedPdfStale?: boolean;
  currentOperator?: string;
}>();

const emit = defineEmits<{
  (e: 'close'): void;
  (e: 'add', kind: UploadKind, files: File[]): void;
  (e: 'remove-staged', kind: UploadKind, id: string): void;
  (e: 'user-delete', photo: UploadedPhoto): void;
  (e: 'preview', payload: { src: string; alt: string }): void;
}>();

const cardTitles: Record<UploadKind, string> = {
  合同: '合同图片拍照上传',
  发票: '发票拍照上传',
  发货单: '发货单拍照上传',
};

const pillText = computed(() => {
  const u = props.detail.uploads;
  const n = u.合同.length + u.发票.length + u.发货单.length;
  if (n === 0) return '未上传';
  return `合同 ${u.合同.length} / 发票 ${u.发票.length} / 发货单 ${u.发货单.length}`;
});
const pillClass = computed(() =>
  props.detail.uploads.合同.length + props.detail.uploads.发票.length + props.detail.uploads.发货单.length > 0
    ? 'pill'
    : 'pill pill-muted',
);
</script>

<template>
  <article class="detail-panel" :aria-label="`订单 ${detail.orderNo} 详情`">
    <header class="detail-header">
      <div>
        <div style="font-weight:600">{{ detail.orderNo }}</div>
        <div class="muted" style="font-size: var(--font-sm)">
          客户：{{ detail.customer || '未知客户' }}
          <span v-if="!detail.csvPresent" class="badge badge-muted" style="margin-left:6px">CSV 已移除</span>
        </div>
      </div>
      <div style="display:flex; gap:8px; align-items:center">
        <span :class="pillClass">{{ pillText }}</span>
        <button class="btn btn-ghost tap" type="button" @click="emit('close')">关闭</button>
      </div>
    </header>

    <div v-if="mergedPdfStale" class="banner" role="status">
      合并 PDF 暂未生成，稍后管理员可重建
    </div>

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
          <tr v-for="(line, i) in detail.lines" :key="`${detail.orderNo}-${i}`">
            <td>{{ line.orderNo }}</td>
            <td class="truncate" style="max-width: 140px">{{ line.customer }}</td>
            <td class="truncate" style="max-width: 160px">{{ line.product }}</td>
            <td>{{ line.quantity }}</td>
            <td>{{ line.totalWithTax }}</td>
            <td>{{ line.invoiceNo }}</td>
          </tr>
        </tbody>
      </table>
    </div>

    <div class="upload-cards">
      <UploadCard
        v-for="k in KINDS"
        :key="k"
        :kind="k"
        :title="cardTitles[k]"
        :server-photos="detail.uploads[k]"
        :staged-photos="staged[k]"
        :current-operator="currentOperator"
        @add="(files) => emit('add', k, files)"
        @remove-staged="(id) => emit('remove-staged', k, id)"
        @user-delete="(photo) => emit('user-delete', photo)"
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
</style>
