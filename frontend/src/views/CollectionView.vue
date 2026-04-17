<script setup lang="ts">
// Per-year collection page (/y{year}). Owns the sticky header (progress +
// search) and the sticky submit bar, and delegates the middle scroll region
// to OrderDetailPanel once an order is selected. Layout rules:
//   - min-height 100dvh with safe-area padding
//   - sticky header + sticky submit share the viewport so both remain
//     visible on a 667px viewport (NFR-UX-2)
import { onBeforeUnmount, onMounted, watch } from 'vue';
import { storeToRefs } from 'pinia';
import ProgressBlock from '@/components/ProgressBlock.vue';
import SearchBar from '@/components/SearchBar.vue';
import OrderDetailPanel from '@/components/OrderDetailPanel.vue';
import { useCollectionStore, type UploadKind } from '@/stores/collection';
import type { SearchItem } from '@/lib/api';

const props = defineProps<{ year: number; operator?: string }>();

const store = useCollectionStore();
const {
  progress,
  searchResults,
  searching,
  searchQuery,
  currentDetail,
  staged,
  canSubmit,
  submitting,
  stagedCount,
  lastMergedPdfStale,
  operator,
} = storeToRefs(store);

async function init(year: number, operator: string) {
  store.setYear(year);
  store.setOperator(operator);
  await store.fetchProgress();
}

onMounted(() => init(props.year, props.operator ?? ''));
watch(
  () => [props.year, props.operator ?? ''] as const,
  ([y, op]) => init(Number(y), String(op)),
);

onBeforeUnmount(() => {
  store.closeDetail();
});

function onSearchQuery(q: string) {
  void store.runSearch(q);
}

async function onPick(item: SearchItem) {
  await store.openOrder(item.orderNo);
}

function onAdd(kind: UploadKind, files: File[]) {
  void store.stageFiles(kind, files);
}

function onRemoveStaged(kind: UploadKind, id: string) {
  store.removeStaged(kind, id);
}

async function onSubmit() {
  await store.submit();
}
</script>

<template>
  <main class="page-collection">
    <div class="sticky-top">
      <ProgressBlock
        :year="year"
        :total="progress?.total ?? null"
        :uploaded="progress?.uploaded ?? null"
        :percent="progress?.percent ?? null"
      />
      <div
        v-if="operator"
        aria-label="录入人"
        style="padding: 4px 12px; font-size: var(--font-sm); color: var(--color-text)"
      >
        录入人：<strong>{{ operator }}</strong>
      </div>
      <SearchBar
        v-model="searchQuery"
        :results="searchResults"
        :loading="searching"
        @query="onSearchQuery"
        @pick="onPick"
      />
    </div>

    <div class="main-scroll">
      <template v-if="currentDetail">
        <OrderDetailPanel
          :detail="currentDetail"
          :staged="staged"
          :merged-pdf-stale="lastMergedPdfStale"
          @close="store.closeDetail()"
          @add="onAdd"
          @remove-staged="onRemoveStaged"
        />
      </template>
      <template v-else>
        <section class="card" style="padding: 16px">
          <h2 style="margin: 0 0 8px 0; font-size: var(--font-md)">如何使用</h2>
          <ol class="muted" style="padding-left: 20px; margin: 0; line-height: 1.7">
            <li>在顶部搜索框输入 单据编号 的任意 2 位以上字符。</li>
            <li>点击结果行打开订单详情。</li>
            <li>依次为「合同」「发票」「发货单」拍照或从相册选择图片。</li>
            <li>点击底部「提交」一次性上传本次新增的图片。</li>
          </ol>
        </section>
      </template>
    </div>

    <footer v-if="currentDetail" class="sticky-submit">
      <span class="hint" v-if="stagedCount === 0">请先添加至少一张图片</span>
      <span class="hint" v-else>待提交 {{ stagedCount }} 张</span>
      <div style="margin-left:auto">
        <button
          type="button"
          class="btn btn-primary tap"
          :disabled="!canSubmit"
          @click="onSubmit"
        >
          <span v-if="submitting" class="spinner" style="margin-right:8px" />
          {{ submitting ? '提交中…' : '提交' }}
        </button>
      </div>
    </footer>
  </main>
</template>
