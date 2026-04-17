<script setup lang="ts">
// Sticky search input + result list. Debounced at 250 ms (FR-SEARCH-1) and
// requires ≥2 characters before asking the backend. Results show 单据编号 +
// 购货单位 (truncated) + per-type badge (FR-SEARCH-2).
//
// N-04: IME composition (Pinyin / 注音 / Kana) fires intermediate `input`
// events for every candidate keystroke. Skipping search while composing
// avoids bouncing the server on partial romanization.
// Mi-03: the clear button and component unmount both cancel any pending
// debounce so a stale query does not fire 250 ms after the user bails out.
import { computed, onBeforeUnmount, ref, watch } from 'vue';
import type { SearchItem } from '@/lib/api';

const props = defineProps<{
  modelValue: string;
  results: SearchItem[];
  loading: boolean;
  disabled?: boolean;
}>();

const emit = defineEmits<{
  (e: 'update:modelValue', v: string): void;
  (e: 'query', v: string): void;
  (e: 'pick', item: SearchItem): void;
}>();

const local = ref(props.modelValue);
const composing = ref(false);
let debounceHandle: ReturnType<typeof setTimeout> | null = null;

function clearDebounce() {
  if (debounceHandle) {
    clearTimeout(debounceHandle);
    debounceHandle = null;
  }
}

watch(
  () => props.modelValue,
  (v) => {
    if (v !== local.value) local.value = v;
  },
);

function scheduleSearch(v: string) {
  clearDebounce();
  debounceHandle = setTimeout(() => {
    debounceHandle = null;
    emit('query', v);
  }, 250);
}

function onInput(e: Event) {
  const target = e.target as HTMLInputElement;
  const v = target.value;
  local.value = v;
  emit('update:modelValue', v);
  // Skip search while an IME composition session is in progress; the final
  // value will arrive via `compositionend`. `event.isComposing` is true for
  // intermediate InputEvents while the composition is active.
  const ev = e as InputEvent;
  if (composing.value || ev.isComposing === true) {
    return;
  }
  scheduleSearch(v);
}

function onCompositionStart() {
  composing.value = true;
  // Drop any debounce scheduled before the composition started.
  clearDebounce();
}

function onCompositionEnd(e: Event) {
  composing.value = false;
  const target = e.target as HTMLInputElement;
  const v = target.value;
  local.value = v;
  emit('update:modelValue', v);
  scheduleSearch(v);
}

function onClear() {
  clearDebounce();
  local.value = '';
  emit('update:modelValue', '');
  emit('query', '');
}

onBeforeUnmount(() => {
  clearDebounce();
});

const showHint = computed(() => local.value.trim().length > 0 && local.value.trim().length < 2);

function badgeText(item: SearchItem): string {
  if (!item.uploaded) return '未上传';
  const { counts } = item;
  return `✓ 合同 ${counts.合同} / 发票 ${counts.发票} / 发货单 ${counts.发货单}`;
}

function badgeClass(item: SearchItem): string {
  return item.uploaded ? 'badge' : 'badge badge-muted';
}
</script>

<template>
  <div class="search-bar">
    <div class="input-wrap">
      <input
        class="input"
        type="search"
        inputmode="search"
        autocomplete="off"
        autocapitalize="off"
        spellcheck="false"
        :placeholder="disabled ? '加载中…' : '搜索 单据编号（至少 2 位）'"
        :value="local"
        :disabled="disabled"
        @input="onInput"
        @compositionstart="onCompositionStart"
        @compositionend="onCompositionEnd"
      />
      <button
        v-if="local.length > 0"
        type="button"
        class="clear-btn"
        aria-label="清空搜索"
        @click="onClear"
      >
        ×
      </button>
    </div>

    <p v-if="showHint" class="hint-row">请至少输入 2 个字符</p>

    <div v-else-if="loading" class="hint-row"><span class="spinner" /> 搜索中…</div>

    <div v-else-if="results.length > 0" class="search-results">
      <button
        v-for="item in results"
        :key="item.orderNo"
        type="button"
        class="search-row tap"
        @click="emit('pick', item)"
      >
        <div class="row-top">
          <span class="order-no">{{ item.orderNo }}</span>
          <span :class="badgeClass(item)">{{ badgeText(item) }}</span>
        </div>
        <div class="customer truncate">
          {{ item.customer || '未知客户' }}
          <span v-if="!item.csvPresent" class="badge badge-muted" style="margin-left:6px">CSV 已移除</span>
        </div>
        <!-- Orders that exist as uploads but are no longer present in the
             current CSV deserve a softer explanation than the bare badge,
             per the R2 review. -->
        <div v-if="!item.csvPresent && item.uploaded" class="row-info">
          该单号不在当前 CSV 中，但已有上传记录
        </div>
      </button>
    </div>

    <p v-else-if="local.trim().length >= 2" class="hint-row">没有匹配的订单</p>
  </div>
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
.row-info {
  margin-top: 2px;
  font-size: 12px;
  color: var(--color-warn);
  background: #fff9e6;
  border: 1px solid #eacb8a;
  border-radius: var(--radius-sm);
  padding: 2px 6px;
  align-self: flex-start;
}
</style>
