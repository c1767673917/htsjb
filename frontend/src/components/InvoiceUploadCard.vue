<script setup lang="ts">
// Upload card for invoice files (images + PDFs). Renders existing server-side
// uploads first, then staged files, then a + tile to add more. PDFs show a
// file icon + filename; images show thumbnail previews. Clipboard paste is
// supported for images only.
import { computed, ref } from 'vue';
import type { InvoiceUploadFile } from '@/lib/api';
import type { StagedFile } from '@/stores/invoice';
import { PER_INVOICE_CAP } from '@/stores/invoice';

const props = defineProps<{
  serverFiles: InvoiceUploadFile[];
  stagedFiles: StagedFile[];
  /** When true, hides the add button and delete affordances on staged files. */
  readOnly?: boolean;
  /** Admin: enable clicking the red x on server files. */
  adminDelete?: boolean;
}>();

const emit = defineEmits<{
  (e: 'add', files: File[]): void;
  (e: 'remove-staged', id: string): void;
  (e: 'admin-delete', file: InvoiceUploadFile): void;
  (e: 'user-delete', file: InvoiceUploadFile): void;
  (e: 'preview', payload: { src: string; alt: string }): void;
}>();

const inputRef = ref<HTMLInputElement | null>(null);

const totalCount = computed(() => props.serverFiles.length + props.stagedFiles.length);
const addDisabled = computed(() => totalCount.value >= PER_INVOICE_CAP);

function isImage(contentType: string): boolean {
  return contentType.startsWith('image/');
}

function previewServerFile(file: InvoiceUploadFile) {
  if (isImage(file.contentType)) {
    emit('preview', { src: file.url, alt: file.filename });
    return;
  }
  window.open(file.url, '_blank', 'noopener,noreferrer');
}

function pickFiles() {
  if (addDisabled.value || props.readOnly) return;
  inputRef.value?.click();
}

function onChange(e: Event) {
  const input = e.target as HTMLInputElement;
  const files = input.files ? Array.from(input.files) : [];
  if (files.length > 0) emit('add', files);
  input.value = '';
}

function onPaste(e: ClipboardEvent) {
  if (props.readOnly || addDisabled.value) return;
  const items = e.clipboardData?.items;
  if (!items) return;
  const files: File[] = [];
  for (const item of items) {
    if (item.type.startsWith('image/')) {
      const file = item.getAsFile();
      if (file) files.push(file);
    }
  }
  if (files.length > 0) {
    e.preventDefault();
    emit('add', files);
  }
}

function humanSize(bytes: number): string {
  if (!bytes) return '';
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(0)} KB`;
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
}
</script>

<template>
  <section class="upload-card" aria-label="发票文件上传" @paste="onPaste">
    <header class="upload-card-title">
      <span>发票文件上传</span>
      <span class="muted" style="font-size: var(--font-sm)">
        已提交 {{ serverFiles.length }}<span v-if="!readOnly"> · 待提交 {{ stagedFiles.length }}/{{ PER_INVOICE_CAP }}</span>
      </span>
    </header>

    <div class="thumb-grid">
      <!-- Server-side files: images as thumbnails, PDFs as icons -->
      <div
        v-for="file in serverFiles"
        :key="`srv-${file.id}`"
        :class="['thumb', 'readonly', isImage(file.contentType) ? '' : 'thumb-pdf thumb-link']"
        role="button"
        tabindex="0"
        :aria-label="isImage(file.contentType) ? `预览 ${file.filename}` : `打开 ${file.filename}`"
        @click="previewServerFile(file)"
        @keydown.enter.prevent="previewServerFile(file)"
        @keydown.space.prevent="previewServerFile(file)"
      >
        <img
          v-if="isImage(file.contentType)"
          :src="file.url"
          :alt="file.filename"
          loading="lazy"
          decoding="async"
        />
        <div v-else class="pdf-icon-wrap">
          <span class="pdf-icon">PDF</span>
          <span class="pdf-name truncate">{{ file.filename }}</span>
        </div>
        <span class="badge-idx">#{{ String(file.seq).padStart(2, '0') }}</span>
        <button
          v-if="adminDelete"
          type="button"
          class="delete-btn"
          aria-label="删除文件"
          @click.stop="emit('admin-delete', file)"
        >
          ×
        </button>
        <button
          v-else-if="!readOnly"
          type="button"
          class="delete-btn"
          aria-label="删除文件"
          @click.stop="emit('user-delete', file)"
        >
          ×
        </button>
        <div class="meta">
          <span>#{{ file.seq }}</span>
          <span>{{ humanSize(file.size) }}</span>
        </div>
        <div
          v-if="adminDelete && file.operator"
          style="font-size: 11px; color: var(--color-text-muted, #666); padding: 2px 4px"
        >
          录入：{{ file.operator }}
        </div>
      </div>

      <!-- Staged files -->
      <div
        v-for="file in stagedFiles"
        :key="file.id"
        :class="['thumb', file.isPdf ? 'thumb-pdf' : '']"
        :role="file.isPdf ? undefined : 'button'"
        :tabindex="file.isPdf ? undefined : 0"
        :aria-label="file.isPdf ? `待提交 PDF ${file.origName}` : '预览待提交文件'"
        @click="!file.isPdf ? emit('preview', { src: file.previewUrl, alt: '待提交' }) : undefined"
        @keydown.enter.prevent="!file.isPdf ? emit('preview', { src: file.previewUrl, alt: '待提交' }) : undefined"
        @keydown.space.prevent="!file.isPdf ? emit('preview', { src: file.previewUrl, alt: '待提交' }) : undefined"
      >
        <img
          v-if="!file.isPdf"
          :src="file.previewUrl"
          alt="待提交"
          loading="lazy"
          decoding="async"
        />
        <div v-else class="pdf-icon-wrap">
          <span class="pdf-icon">PDF</span>
          <span class="pdf-name truncate">{{ file.origName }}</span>
        </div>
        <button
          v-if="!readOnly"
          type="button"
          class="delete-btn"
          aria-label="移除"
          @click.stop="emit('remove-staged', file.id)"
        >
          ×
        </button>
        <div class="meta">
          <span>待提交</span>
          <span>{{ humanSize(file.outSize) }}</span>
        </div>
      </div>

      <!-- Add tile -->
      <button
        v-if="!readOnly"
        type="button"
        :class="['thumb thumb-add tap', addDisabled ? 'disabled' : '']"
        :aria-label="addDisabled ? `已达 ${PER_INVOICE_CAP} 个上限` : '添加文件'"
        :title="addDisabled ? `最多 ${PER_INVOICE_CAP} 个` : '拍照、选择图片或 PDF，也可粘贴截图'"
        :disabled="addDisabled"
        @click="pickFiles"
      >
        +
        <span class="paste-hint">可粘贴截图</span>
      </button>
    </div>

    <input
      v-if="!readOnly"
      ref="inputRef"
      type="file"
      accept="image/*,.pdf,application/pdf"
      multiple
      class="sr-only"
      @change="onChange"
    />
  </section>
</template>

<style scoped>
.thumb-pdf {
  cursor: default;
}

.thumb-link {
  cursor: pointer;
}

.pdf-icon-wrap {
  width: 100%;
  height: 100%;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 4px;
  padding: 4px;
}

.pdf-icon {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 40px;
  height: 40px;
  background: #e63946;
  color: #fff;
  border-radius: var(--radius-sm);
  font-size: 12px;
  font-weight: 700;
  letter-spacing: 0.5px;
}

.pdf-name {
  font-size: 10px;
  color: var(--color-text-muted);
  text-align: center;
  max-width: 100%;
  padding: 0 2px;
}

.thumb-add {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
}

.paste-hint {
  font-size: 11px;
  color: var(--color-text-muted, #999);
  margin-top: 2px;
  font-weight: 400;
}
</style>
