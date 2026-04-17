<script setup lang="ts">
// A single upload card for one kind (合同 / 发票 / 发货单). Renders existing
// server-side photos first (read-only, grayed), then staged photos with a
// delete icon, then a `+` tile that opens the camera (FR-DETAIL-3).
// Collection-page mode is read-only for server photos (adminMode=false). In
// admin mode, staged is never used — the parent decides whether to expose
// a delete-on-server hook per thumbnail.
import { computed, ref } from 'vue';
import type { UploadedPhoto } from '@/lib/api';
import type { StagedPhoto, UploadKind } from '@/stores/collection';
import { PER_KIND_CAP } from '@/stores/collection';

const props = defineProps<{
  kind: UploadKind;
  title: string;
  serverPhotos: UploadedPhoto[];
  stagedPhotos?: StagedPhoto[];
  /** When true, hides the camera input and the delete affordance on staged photos. */
  readOnly?: boolean;
  /** Admin: enable clicking the red x on server photos. */
  adminDelete?: boolean;
}>();

const emit = defineEmits<{
  (e: 'add', files: File[]): void;
  (e: 'remove-staged', id: string): void;
  (e: 'admin-delete', photo: UploadedPhoto): void;
}>();

const inputRef = ref<HTMLInputElement | null>(null);

const stagedCount = computed(() => (props.stagedPhotos ?? []).length);
const addDisabled = computed(() => stagedCount.value >= PER_KIND_CAP);

function pickFiles() {
  if (addDisabled.value || props.readOnly) return;
  inputRef.value?.click();
}

function onChange(e: Event) {
  const input = e.target as HTMLInputElement;
  const files = input.files ? Array.from(input.files) : [];
  if (files.length > 0) emit('add', files);
  // Reset so selecting the same file twice still fires change.
  input.value = '';
}

function humanSize(bytes: number): string {
  if (!bytes) return '';
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(0)} KB`;
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
}
</script>

<template>
  <section class="upload-card" :aria-label="title">
    <header class="upload-card-title">
      <span>{{ title }}</span>
      <span class="muted" style="font-size: var(--font-sm)">
        已提交 {{ serverPhotos.length }}<span v-if="!readOnly"> · 待提交 {{ stagedCount }}/{{ PER_KIND_CAP }}</span>
      </span>
    </header>

    <div class="thumb-grid">
      <!-- Server-side photos: grayed + read-only on collection page. The
           originals can be ~10 MB each; `loading="lazy"` + `decoding="async"`
           keep the main thread responsive, and the `.thumb` CSS box clamps
           the rendered size to 96x96 via `object-fit: cover` so the browser
           never has to paint the full-resolution bitmap into the layout
           (M-15). -->
      <div v-for="photo in serverPhotos" :key="`srv-${photo.id}`" class="thumb readonly">
        <img
          :src="photo.url"
          :alt="`${kind}-${photo.seq}`"
          loading="lazy"
          decoding="async"
        />
        <span class="badge-idx">{{ kind }} {{ String(photo.seq).padStart(2, '0') }}</span>
        <button
          v-if="adminDelete"
          type="button"
          class="delete-btn"
          aria-label="删除照片"
          @click.stop="emit('admin-delete', photo)"
        >
          ×
        </button>
        <div class="meta">
          <span>#{{ photo.seq }}</span>
          <span>{{ humanSize(photo.size) }}</span>
        </div>
        <div
          v-if="adminDelete && photo.operator"
          style="font-size: 11px; color: var(--color-text-muted, #666); padding: 2px 4px"
        >
          录入：{{ photo.operator }}
        </div>
      </div>

      <!-- Staged (not yet submitted) photos. -->
      <div v-for="photo in stagedPhotos ?? []" :key="photo.id" class="thumb">
        <img
          :src="photo.previewUrl"
          :alt="`staged-${kind}`"
          loading="lazy"
          decoding="async"
        />
        <button
          v-if="!readOnly"
          type="button"
          class="delete-btn"
          aria-label="移除"
          @click.stop="emit('remove-staged', photo.id)"
        >
          ×
        </button>
        <div class="meta">
          <span>待提交</span>
          <span>{{ humanSize(photo.outSize) }}</span>
        </div>
      </div>

      <!-- Add tile -->
      <button
        v-if="!readOnly"
        type="button"
        :class="['thumb thumb-add tap', addDisabled ? 'disabled' : '']"
        :aria-label="addDisabled ? `${title}已达 ${PER_KIND_CAP} 张上限` : `添加${title}`"
        :title="addDisabled ? `最多 ${PER_KIND_CAP} 张` : '拍照或从相册选择'"
        :disabled="addDisabled"
        @click="pickFiles"
      >
        ＋
      </button>
    </div>

    <input
      v-if="!readOnly"
      ref="inputRef"
      type="file"
      accept="image/*"
      capture="environment"
      multiple
      class="sr-only"
      @change="onChange"
    />
  </section>
</template>

<style scoped>
/* N-02: the `+` add-tile is a <button> with no default outline because the
   base.css reset clears it. Restore a visible outline for keyboard users
   (:focus-visible only, so mouse/touch taps stay visually clean). */
.thumb-add:focus-visible {
  outline: 2px solid var(--color-primary);
  outline-offset: 2px;
  border-color: var(--color-primary);
}
</style>
