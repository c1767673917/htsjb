<script setup lang="ts">
// Year-level progress display, sticky at the top of the collection page.
// Shows the canonical sentence required by FR-PROGRESS-1:
//   "本年共 N 个订单号 · 已上传 M · 进度 M/N (P %)"
import { computed } from 'vue';

const props = defineProps<{
  year: number;
  total: number | null;
  uploaded: number | null;
  percent: number | null;
}>();

const percentDisplay = computed(() => {
  if (props.percent == null) return '--';
  return `${Math.round(props.percent * 100)} %`;
});

const barWidth = computed(() => {
  if (props.percent == null) return '0%';
  const clamped = Math.max(0, Math.min(1, props.percent));
  return `${(clamped * 100).toFixed(1)}%`;
});

const isLoading = computed(() => props.total == null);
</script>

<template>
  <section class="progress-block" :aria-label="`${year} 年进度`">
    <div class="progress-text">
      <template v-if="isLoading">
        <span class="muted">加载进度中…</span>
      </template>
      <template v-else>
        <strong>{{ year }} 年</strong>
        <span> · 共 {{ total }} 个订单号</span>
        <span> · 已上传 {{ uploaded }}</span>
        <span>
          · 进度 {{ uploaded }}/{{ total }} ({{ percentDisplay }})
        </span>
      </template>
    </div>
    <div class="progress-bar" aria-hidden="true">
      <span :style="{ width: barWidth }" />
    </div>
  </section>
</template>

<style scoped>
.progress-text {
  font-size: var(--font-sm);
  color: var(--color-text);
}
</style>
