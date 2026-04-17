<script setup lang="ts">
// Fullscreen image preview. Click the backdrop or the × button to close.
// The Escape key also closes the preview so keyboard users are not trapped.
import { onBeforeUnmount, onMounted, watch } from 'vue';

const props = defineProps<{ src: string | null; alt?: string }>();
const emit = defineEmits<{ (e: 'close'): void }>();

function onKeydown(ev: KeyboardEvent) {
  if (ev.key === 'Escape' && props.src) emit('close');
}

onMounted(() => {
  window.addEventListener('keydown', onKeydown);
});
onBeforeUnmount(() => {
  window.removeEventListener('keydown', onKeydown);
});

watch(
  () => props.src,
  (v) => {
    document.body.style.overflow = v ? 'hidden' : '';
  },
);
</script>

<template>
  <div
    v-if="src"
    class="lightbox"
    role="dialog"
    aria-modal="true"
    aria-label="图片预览"
    @click.self="emit('close')"
  >
    <button
      type="button"
      class="lightbox-close tap"
      aria-label="关闭预览"
      @click="emit('close')"
    >
      ×
    </button>
    <img :src="src" :alt="alt ?? '预览'" class="lightbox-img" @click="emit('close')" />
  </div>
</template>

<style scoped>
.lightbox {
  position: fixed;
  inset: 0;
  z-index: 1000;
  background: rgba(0, 0, 0, 0.85);
  display: flex;
  align-items: center;
  justify-content: center;
  padding: env(safe-area-inset-top) env(safe-area-inset-right)
    env(safe-area-inset-bottom) env(safe-area-inset-left);
}
.lightbox-img {
  max-width: 100%;
  max-height: 100%;
  object-fit: contain;
  display: block;
  cursor: zoom-out;
}
.lightbox-close {
  position: absolute;
  top: calc(env(safe-area-inset-top) + 12px);
  right: calc(env(safe-area-inset-right) + 12px);
  width: 40px;
  height: 40px;
  border-radius: 50%;
  background: rgba(255, 255, 255, 0.15);
  color: #fff;
  font-size: 24px;
  line-height: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  border: 1px solid rgba(255, 255, 255, 0.35);
}
</style>
