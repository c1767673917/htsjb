// Invoice store: staged files for the currently open invoice, search debounce
// state, submit orchestration. Mirrors collection.ts pattern but without year
// grouping or per-kind categorization. Staged files are a flat array that can
// contain images (run through the image pipeline) or PDFs (stored as-is).

import { defineStore } from 'pinia';
import { computed, ref } from 'vue';
import {
  ApiError,
  invoiceApi,
  type InvoiceDetail,
  type InvoiceSearchItem,
  type InvoiceYearProgress,
} from '@/lib/api';
import { processImage, type PipelineResult } from '@/lib/imagePipeline';
import { useUiStore } from './ui';

export const PER_INVOICE_CAP = 1;

/** Staged (not yet submitted) file in the UI. */
export interface StagedFile {
  id: string;
  blob: Blob;
  previewUrl: string; // object URL for images, empty for PDFs
  origName: string;   // original filename
  origSize: number;
  outSize: number;
  isPdf: boolean;
  addedAt: number;
}

function makeId(): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return crypto.randomUUID();
  }
  return `${Date.now()}-${Math.random().toString(16).slice(2, 10)}`;
}

export const useInvoiceStore = defineStore('invoice', () => {
  const operator = ref<string>('');
  const yearProgress = ref<InvoiceYearProgress[]>([]);
  const searchQuery = ref<string>('');
  const searchResults = ref<InvoiceSearchItem[]>([]);
  const searching = ref<boolean>(false);
  const currentDetail = ref<InvoiceDetail | null>(null);
  const staged = ref<StagedFile[]>([]);
  const submitting = ref<boolean>(false);

  const uploadedCount = computed(() => currentDetail.value?.uploads.length ?? 0);
  const stagedCount = computed(() => staged.value.length);
  const totalCount = computed(() => uploadedCount.value + stagedCount.value);
  const canSubmit = computed(() => stagedCount.value > 0 && !submitting.value);

  let searchSeq = 0;
  let searchAbort: AbortController | null = null;
  let progressSeq = 0;
  let progressAbort: AbortController | null = null;
  let detailSeq = 0;
  let detailAbort: AbortController | null = null;
  let cacheBust = 0;

  function stampUploadUrls(detail: InvoiceDetail) {
    if (cacheBust === 0) return;
    const suffix = `?_v=${cacheBust}`;
    for (const u of detail.uploads) {
      u.url += suffix;
    }
  }

  async function fetchProgress() {
    const mySeq = ++progressSeq;
    if (progressAbort) progressAbort.abort();
    progressAbort = new AbortController();
    try {
      const items = await invoiceApi.progress(progressAbort.signal);
      if (mySeq !== progressSeq) return;
      yearProgress.value = items;
    } catch (err) {
      if ((err as Error).name === 'AbortError') return;
      if (mySeq !== progressSeq) return;
      const ui = useUiStore();
      ui.error(err instanceof ApiError ? err.message : '获取进度失败');
    }
  }

  function setOperator(op: string) {
    operator.value = (op ?? '').trim();
  }

  async function runSearch(q: string) {
    searchQuery.value = q;
    const trimmed = q.trim();
    if (trimmed.length < 2) {
      searchResults.value = [];
      searching.value = false;
      if (searchAbort) searchAbort.abort();
      return;
    }
    const mySeq = ++searchSeq;
    if (searchAbort) searchAbort.abort();
    searchAbort = new AbortController();
    searching.value = true;
    try {
      const items = await invoiceApi.search(trimmed, 20, searchAbort.signal);
      if (mySeq === searchSeq) {
        searchResults.value = items;
      }
    } catch (err) {
      if ((err as Error).name === 'AbortError') return;
      if (mySeq === searchSeq) {
        const ui = useUiStore();
        ui.error(err instanceof ApiError ? err.message : '搜索失败');
        searchResults.value = [];
      }
    } finally {
      if (mySeq === searchSeq) searching.value = false;
    }
  }

  async function openInvoice(invoiceNo: string) {
    clearStaged();
    currentDetail.value = null;
    if (searchAbort) searchAbort.abort();
    searching.value = false;
    searchResults.value = [];
    searchQuery.value = '';
    await refreshDetail(invoiceNo);
  }

  function closeDetail() {
    clearStaged();
    currentDetail.value = null;
    if (detailAbort) {
      detailAbort.abort();
      detailAbort = null;
    }
    detailSeq += 1;
  }

  async function refreshDetail(invoiceNo?: string) {
    const targetNo = invoiceNo ?? currentDetail.value?.invoiceNo;
    if (!targetNo) return;
    const mySeq = ++detailSeq;
    if (detailAbort) detailAbort.abort();
    detailAbort = new AbortController();
    const signal = detailAbort.signal;
    try {
      const resp = await invoiceApi.detail(targetNo, signal);
      if (mySeq !== detailSeq) return;
      stampUploadUrls(resp);
      currentDetail.value = resp;
    } catch (err) {
      if ((err as Error).name === 'AbortError') return;
      if (mySeq !== detailSeq) return;
      const ui = useUiStore();
      ui.error(err instanceof ApiError ? err.message : '加载发票详情失败');
    }
  }

  /**
   * Stage a list of picked files. PDFs bypass the image pipeline; images
   * are run through processImage(). Per-file errors are reported via the
   * UI store without aborting the rest.
   */
  async function stageFiles(files: File[]): Promise<void> {
    const ui = useUiStore();
    for (const file of files) {
      if (totalCount.value >= PER_INVOICE_CAP) {
        ui.error(`发票最多上传 ${PER_INVOICE_CAP} 个文件`);
        return;
      }
      const isPdf = file.type === 'application/pdf' ||
        file.name.toLowerCase().endsWith('.pdf');
      if (isPdf) {
        const entry: StagedFile = {
          id: makeId(),
          blob: file,
          previewUrl: '',
          origName: file.name,
          origSize: file.size,
          outSize: file.size,
          isPdf: true,
          addedAt: Date.now(),
        };
        staged.value.push(entry);
      } else {
        try {
          const result: PipelineResult = await processImage(file);
          const entry: StagedFile = {
            id: makeId(),
            blob: result.blob,
            previewUrl: URL.createObjectURL(result.blob),
            origName: file.name,
            origSize: result.origSize,
            outSize: result.outSize,
            isPdf: false,
            addedAt: Date.now(),
          };
          staged.value.push(entry);
        } catch (err) {
          const msg = err instanceof Error ? err.message : '图片处理失败';
          ui.error(msg);
        }
      }
    }
  }

  function removeStaged(id: string) {
    const idx = staged.value.findIndex((f) => f.id === id);
    if (idx >= 0) {
      const [removed] = staged.value.splice(idx, 1);
      if (removed && removed.previewUrl) {
        try {
          URL.revokeObjectURL(removed.previewUrl);
        } catch {
          /* ignore */
        }
      }
    }
  }

  function clearStaged() {
    for (const f of staged.value) {
      if (f.previewUrl) {
        try {
          URL.revokeObjectURL(f.previewUrl);
        } catch {
          /* ignore */
        }
      }
    }
    staged.value = [];
  }

  async function deleteUpload(id: number): Promise<boolean> {
    const ui = useUiStore();
    if (!currentDetail.value) return false;
    try {
      await invoiceApi.deleteUpload(currentDetail.value.invoiceNo, id);
      ui.success('已删除');
      cacheBust++;
      await Promise.all([refreshDetail(), fetchProgress()]);
      return true;
    } catch (err) {
      ui.error(err instanceof ApiError ? err.message : '删除失败');
      return false;
    }
  }

  async function submit(): Promise<boolean> {
    const ui = useUiStore();
    if (submitting.value) return false;
    if (!currentDetail.value) return false;
    if (stagedCount.value === 0) {
      ui.error('请先添加至少一个文件');
      return false;
    }

    const form = new FormData();
    if (operator.value) form.append('operator', operator.value);

    let imgIdx = 0;
    let pdfIdx = 0;
    for (const f of staged.value) {
      if (f.isPdf) {
        pdfIdx += 1;
        form.append('invoice_pdf[]', f.blob, f.origName || `invoice-${pdfIdx}.pdf`);
      } else {
        imgIdx += 1;
        form.append('invoice_photo[]', f.blob, `invoice-${imgIdx}.jpg`);
      }
    }

    submitting.value = true;
    try {
      await invoiceApi.submit(currentDetail.value.invoiceNo, form);
      clearStaged();
      cacheBust++;
      await Promise.all([refreshDetail(), fetchProgress()]);
      ui.success('提交成功');
      return true;
    } catch (err) {
      const msg = err instanceof ApiError ? err.message : '提交失败，请稍后重试';
      ui.error(msg);
      return false;
    } finally {
      submitting.value = false;
    }
  }

  return {
    operator,
    yearProgress,
    searchQuery,
    searchResults,
    searching,
    currentDetail,
    staged,
    submitting,
    stagedCount,
    canSubmit,
    setOperator,
    fetchProgress,
    runSearch,
    openInvoice,
    closeDetail,
    refreshDetail,
    stageFiles,
    removeStaged,
    clearStaged,
    deleteUpload,
    submit,
  };
});
