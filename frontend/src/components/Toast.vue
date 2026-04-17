<script setup lang="ts">
// Transient feedback stack. The ui store owns the lifecycle (auto-dismiss at
// 2.5s, vibrate on success). This component is purely presentational.
//
// N-03: a single aria-live region can only declare one politeness level, and
// `polite` is wrong for error toasts — red "提交失败" banners should interrupt
// the screen reader rather than wait for the next natural pause. We split
// toasts into two live regions: `assertive` for errors, `polite` for
// success/info. Visually they share the same stack via absolute positioning.
import { computed } from 'vue';
import { storeToRefs } from 'pinia';
import { useUiStore } from '@/stores/ui';

const ui = useUiStore();
const { toasts } = storeToRefs(ui);

const errorToasts = computed(() => toasts.value.filter((t) => t.kind === 'error'));
const politeToasts = computed(() => toasts.value.filter((t) => t.kind !== 'error'));
</script>

<template>
  <div class="toast-stack">
    <div class="toast-live" role="alert" aria-live="assertive" aria-atomic="true">
      <div v-for="t in errorToasts" :key="t.id" :class="['toast', t.kind]">
        {{ t.message }}
      </div>
    </div>
    <div class="toast-live" role="status" aria-live="polite" aria-atomic="true">
      <div v-for="t in politeToasts" :key="t.id" :class="['toast', t.kind]">
        {{ t.message }}
      </div>
    </div>
  </div>
</template>

<style scoped>
.toast-live {
  display: contents;
}
</style>
