<script setup lang="ts">
// Invoice collection page (/invoices). Mirrors CollectionView but without year
// grouping or progress block. Owns the sticky header (search) and the sticky
// submit bar, and delegates the middle scroll region to InvoiceDetailPanel
// once an invoice is selected.
import { onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { storeToRefs } from 'pinia';
import InvoiceDetailPanel from '@/components/InvoiceDetailPanel.vue';
import ImageLightbox from '@/components/ImageLightbox.vue';
import { useInvoiceStore } from '@/stores/invoice';
import type { InvoiceSearchItem, InvoiceUploadFile } from '@/lib/api';

const props = defineProps<{ operator?: string }>();

const store = useInvoiceStore();
const {
  searchResults,
  searching,
  searchQuery,
  currentDetail,
  staged,
  canSubmit,
  submitting,
  stagedCount,
  operator,
  yearProgress,
} = storeToRefs(store);

function init(op: string) {
  store.setOperator(op);
  void store.fetchProgress();
}

onMounted(() => init(props.operator ?? ''));
watch(
  () => props.operator ?? '',
  (op) => init(String(op)),
);

onBeforeUnmount(() => {
  store.closeDetail();
});

// --- Search (inline, no reuse of SearchBar since it's typed for SearchItem) ---

const localQuery = ref('');
const composing = ref(false);
let debounceHandle: ReturnType<typeof setTimeout> | null = null;

function clearDebounce() {
  if (debounceHandle) {
    clearTimeout(debounceHandle);
    debounceHandle = null;
  }
}

watch(searchQuery, (v) => {
  if (v !== localQuery.value) localQuery.value = v;
});

function scheduleSearch(v: string) {
  clearDebounce();
  debounceHandle = setTimeout(() => {
    debounceHandle = null;
    void store.runSearch(v);
  }, 250);
}

function onSearchInput(e: Event) {
  const target = e.target as HTMLInputElement;
  const v = target.value;
  localQuery.value = v;
  const ev = e as InputEvent;
  if (composing.value || ev.isComposing === true) return;
  scheduleSearch(v);
}

function onCompositionStart() {
  composing.value = true;
  clearDebounce();
}

function onCompositionEnd(e: Event) {
  composing.value = false;
  const target = e.target as HTMLInputElement;
  scheduleSearch(target.value);
}

function onSearchClear() {
  clearDebounce();
  localQuery.value = '';
  void store.runSearch('');
}

onBeforeUnmount(() => {
  clearDebounce();
});

const showHint = ref(false);
watch(localQuery, (v) => {
  showHint.value = v.trim().length > 0 && v.trim().length < 2;
});

function badgeText(item: InvoiceSearchItem): string {
  if (!item.uploaded) return '未上传';
  return `已上传 ${item.uploadCount} 个`;
}

function badgeClass(item: InvoiceSearchItem): string {
  return item.uploaded ? 'badge' : 'badge badge-muted';
}

async function onPick(item: InvoiceSearchItem) {
  await store.openInvoice(item.invoiceNo);
}

function onAdd(files: File[]) {
  void store.stageFiles(files);
}

function onRemoveStaged(id: string) {
  store.removeStaged(id);
}

async function onSubmit() {
  await store.submit();
}

const previewSrc = ref<string | null>(null);
const previewAlt = ref<string>('');

function onPreview(payload: { src: string; alt: string }) {
  previewSrc.value = payload.src;
  previewAlt.value = payload.alt;
}

function closePreview() {
  previewSrc.value = null;
  previewAlt.value = '';
}

async function onUserDelete(file: InvoiceUploadFile) {
  if (!window.confirm(`确定删除该文件？\n${file.filename}`)) return;
  await store.deleteUpload(file.id);
}
</script>

<template>
  <main class="page-collection">
    <div class="sticky-top">
      <div
        v-if="operator"
        aria-label="录入人"
        style="padding: 4px 0; font-size: var(--font-sm); color: var(--color-text)"
      >
        录入人：<strong>{{ operator }}</strong>
      </div>

      <!-- Inline search bar for invoices -->
      <div class="search-bar">
        <div class="input-wrap">
          <input
            class="input"
            type="search"
            inputmode="search"
            autocomplete="off"
            autocapitalize="off"
            spellcheck="false"
            placeholder="搜索 发票号码（至少 2 位）"
            :value="localQuery"
            @input="onSearchInput"
            @compositionstart="onCompositionStart"
            @compositionend="onCompositionEnd"
          />
          <button
            v-if="localQuery.length > 0"
            type="button"
            class="clear-btn"
            aria-label="清空搜索"
            @click="onSearchClear"
          >
            ×
          </button>
        </div>

        <p v-if="showHint" class="hint-row">请至少输入 2 个字符</p>

        <div v-else-if="searching" class="hint-row"><span class="spinner" /> 搜索中...</div>

        <div v-else-if="searchResults.length > 0" class="search-results">
          <button
            v-for="item in searchResults"
            :key="item.invoiceNo"
            type="button"
            class="search-row tap"
            @click="onPick(item)"
          >
            <div class="row-top">
              <span class="order-no">{{ item.invoiceNo }}</span>
              <span :class="badgeClass(item)">{{ badgeText(item) }}</span>
            </div>
            <div class="customer truncate">
              {{ item.customer || '未知客户' }}
              <span class="muted" style="margin-left: 8px; font-size: 12px">{{ item.invoiceDate }}</span>
            </div>
          </button>
        </div>

        <p v-else-if="localQuery.trim().length >= 2" class="hint-row">没有匹配的发票</p>
      </div>
    </div>

    <div class="main-scroll">
      <template v-if="currentDetail">
        <InvoiceDetailPanel
          :detail="currentDetail"
          :staged="staged"
          :current-operator="operator"
          @close="store.closeDetail()"
          @add="onAdd"
          @remove-staged="onRemoveStaged"
          @user-delete="onUserDelete"
          @preview="onPreview"
        />
      </template>
      <template v-else>
        <section v-if="yearProgress.length > 0" class="card" style="padding: 16px">
          <h2 style="margin: 0 0 12px 0; font-size: var(--font-md)">上传进度</h2>
          <div class="year-progress-list">
            <div v-for="yp in yearProgress" :key="yp.year" class="year-progress-item">
              <div class="year-progress-text">
                <strong>{{ yp.year }} 年</strong>
                <span> · 共 {{ yp.total }} 张</span>
                <span> · 已上传 {{ yp.uploaded }}</span>
                <span> · {{ Math.round(yp.percent * 100) }}%</span>
              </div>
              <div class="progress-bar" aria-hidden="true">
                <span :style="{ width: `${Math.min(yp.percent * 100, 100).toFixed(1)}%` }" />
              </div>
            </div>
          </div>
        </section>
        <section class="card" style="padding: 16px">
          <h2 style="margin: 0 0 8px 0; font-size: var(--font-md)">如何使用</h2>
          <ol class="muted" style="padding-left: 20px; margin: 0; line-height: 1.7">
            <li>在顶部搜索框输入 发票号码 的任意 2 位以上字符。</li>
            <li>点击结果行打开发票详情。</li>
            <li>拍照、选择 1 个图片或 PDF 文件上传。</li>
            <li>点击底部「提交」一次性上传本次新增的文件。</li>
          </ol>
        </section>
      </template>
    </div>

    <footer v-if="currentDetail" class="sticky-submit">
      <span class="hint" v-if="stagedCount === 0">请先添加至少一个文件</span>
      <span class="hint" v-else>待提交 {{ stagedCount }} 个</span>
      <div style="margin-left:auto">
        <button
          type="button"
          class="btn btn-primary tap"
          :disabled="!canSubmit"
          @click="onSubmit"
        >
          <span v-if="submitting" class="spinner" style="margin-right:8px" />
          {{ submitting ? '提交中...' : '提交' }}
        </button>
      </div>
    </footer>

    <ImageLightbox :src="previewSrc" :alt="previewAlt" @close="closePreview" />
  </main>
</template>

<style scoped>
.input-wrap {
  position: relative;
}
.clear-btn {
  position: absolute;
  right: 6px;
  top: 50%;
  transform: translateY(-50%);
  width: 32px;
  height: 32px;
  border-radius: 50%;
  background: transparent;
  color: var(--color-text-muted);
  font-size: 20px;
  display: flex;
  align-items: center;
  justify-content: center;
}
.hint-row {
  margin: 6px 2px 0;
  color: var(--color-text-muted);
  font-size: var(--font-sm);
}
.year-progress-list {
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.year-progress-item {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.year-progress-text {
  font-size: var(--font-sm);
  color: var(--color-text);
}
</style>
