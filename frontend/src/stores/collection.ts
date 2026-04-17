// Collection store: staged photos for the currently open order, year
// progress, search debounce state, submit orchestration. Implements
// FR-SUBMIT-1/4/5: one multipart POST, on success clear staged + refetch
// server-side photos + refetch year progress + green toast (no route
// change), on 4xx/5xx keep staged + red toast.

import { defineStore } from 'pinia';
import { computed, ref } from 'vue';
import {
  ApiError,
  collectionApi,
  type OrderDetail,
  type ProgressShape,
  type SearchItem,
  type SubmitResponse,
} from '@/lib/api';
import { processImage, type PipelineResult } from '@/lib/imagePipeline';
import { useUiStore } from './ui';

export type UploadKind = '合同' | '发票' | '发货单';

export const KINDS: UploadKind[] = ['合同', '发货单', '发票'];
export const KIND_FIELD: Record<UploadKind, string> = {
  合同: 'contract[]',
  发票: 'invoice[]',
  发货单: 'delivery[]',
};
export const PER_KIND_CAP = 50;

/** Staged (not yet submitted) photo in the UI. */
export interface StagedPhoto {
  id: string; // local-only id (uuid-ish)
  kind: UploadKind;
  blob: Blob;
  previewUrl: string;
  origSize: number;
  outSize: number;
  width: number;
  height: number;
  addedAt: number;
}

function makeId(): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return crypto.randomUUID();
  }
  return `${Date.now()}-${Math.random().toString(16).slice(2, 10)}`;
}

export const useCollectionStore = defineStore('collection', () => {
  const year = ref<number>(0);
  const operator = ref<string>('');
  const progress = ref<ProgressShape | null>(null);
  const searchQuery = ref<string>('');
  const searchResults = ref<SearchItem[]>([]);
  const searching = ref<boolean>(false);
  const currentOrderNo = ref<string | null>(null);
  const currentDetail = ref<OrderDetail | null>(null);
  const staged = ref<Record<UploadKind, StagedPhoto[]>>({
    合同: [],
    发票: [],
    发货单: [],
  });
  const submitting = ref<boolean>(false);
  const lastMergedPdfStale = ref<boolean>(false);

  const stagedCount = computed(
    () => staged.value.合同.length + staged.value.发票.length + staged.value.发货单.length,
  );
  const canSubmit = computed(() => stagedCount.value > 0 && !submitting.value);

  let searchSeq = 0;
  let searchAbort: AbortController | null = null;
  // M-25: tapping two search results in quick succession can let the later
  // detail fetch resolve before the earlier one. Every detail fetch gets a
  // monotonically increasing seq and an AbortController so the store
  // discards stale responses and cancels the earlier network call.
  let detailSeq = 0;
  let detailAbort: AbortController | null = null;

  function setYear(y: number) {
    if (year.value !== y) {
      year.value = y;
      progress.value = null;
      searchQuery.value = '';
      searchResults.value = [];
      if (searchAbort) searchAbort.abort();
      closeDetail();
    }
  }

  function setOperator(op: string) {
    operator.value = (op ?? '').trim();
  }

  async function fetchProgress() {
    if (!year.value) return;
    try {
      progress.value = await collectionApi.progress(year.value);
    } catch (err) {
      const ui = useUiStore();
      ui.error(err instanceof ApiError ? err.message : '获取进度失败');
    }
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
      const items = await collectionApi.search(year.value, trimmed, 20, searchAbort.signal);
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

  async function openOrder(orderNo: string) {
    // Replace any previously open detail (FR-SEARCH-3). Staged photos from a
    // previous order are discarded because uploads always belong to the order
    // the user has open.
    clearStaged();
    currentOrderNo.value = orderNo;
    currentDetail.value = null;
    lastMergedPdfStale.value = false;
    // Dismiss the search results panel once an order is chosen so the detail
    // view is not obscured by the candidate list.
    if (searchAbort) searchAbort.abort();
    searching.value = false;
    searchResults.value = [];
    searchQuery.value = '';
    await refreshDetail();
  }

  function closeDetail() {
    clearStaged();
    currentOrderNo.value = null;
    currentDetail.value = null;
    lastMergedPdfStale.value = false;
    // Drop any in-flight detail fetch so its response cannot repopulate the
    // panel after it was explicitly closed.
    if (detailAbort) {
      detailAbort.abort();
      detailAbort = null;
    }
    detailSeq += 1;
  }

  async function refreshDetail() {
    if (!year.value || !currentOrderNo.value) return;
    const targetOrderNo = currentOrderNo.value;
    const targetYear = year.value;
    const mySeq = ++detailSeq;
    if (detailAbort) detailAbort.abort();
    detailAbort = new AbortController();
    const signal = detailAbort.signal;
    try {
      const resp = await collectionApi.detail(targetYear, targetOrderNo, signal);
      // M-25: ignore stale responses. A second openOrder() call bumps
      // detailSeq, so a slower earlier fetch resolving after it must not
      // overwrite the newer order's detail.
      if (mySeq !== detailSeq) return;
      if (currentOrderNo.value !== targetOrderNo || year.value !== targetYear) return;
      currentDetail.value = resp;
    } catch (err) {
      if ((err as Error).name === 'AbortError') return;
      if (mySeq !== detailSeq) return;
      const ui = useUiStore();
      ui.error(err instanceof ApiError ? err.message : '加载订单详情失败');
    }
  }

  /**
   * Stage a list of picked files into a kind bucket. Runs each file through
   * the image pipeline and reports per-file errors via the UI store without
   * aborting the rest.
   */
  async function stageFiles(kind: UploadKind, files: File[]): Promise<void> {
    const ui = useUiStore();
    for (const file of files) {
      if (staged.value[kind].length >= PER_KIND_CAP) {
        ui.error(`${kind}最多上传 ${PER_KIND_CAP} 张`);
        return;
      }
      try {
        const result: PipelineResult = await processImage(file);
        const entry: StagedPhoto = {
          id: makeId(),
          kind,
          blob: result.blob,
          previewUrl: URL.createObjectURL(result.blob),
          origSize: result.origSize,
          outSize: result.outSize,
          width: result.width,
          height: result.height,
          addedAt: Date.now(),
        };
        staged.value[kind].push(entry);
      } catch (err) {
        const msg = err instanceof Error ? err.message : '图片处理失败';
        ui.error(msg);
      }
    }
  }

  function removeStaged(kind: UploadKind, id: string) {
    const list = staged.value[kind];
    const idx = list.findIndex((p) => p.id === id);
    if (idx >= 0) {
      const [removed] = list.splice(idx, 1);
      if (removed && removed.previewUrl) {
        try {
          URL.revokeObjectURL(removed.previewUrl);
        } catch {
          /* ignore */
        }
      }
    }
  }

  async function deleteServerPhoto(id: number): Promise<boolean> {
    const ui = useUiStore();
    if (!year.value || !currentOrderNo.value) return false;
    if (!operator.value) {
      ui.error('仅能删除本人上传的图片');
      return false;
    }
    try {
      const resp = await collectionApi.deleteOwnPhoto(
        year.value,
        currentOrderNo.value,
        id,
        operator.value,
      );
      ui.success('已删除');
      lastMergedPdfStale.value = Boolean(resp.mergedPdfStale);
      await Promise.all([refreshDetail(), fetchProgress()]);
      return true;
    } catch (err) {
      ui.error(err instanceof ApiError ? err.message : '删除失败');
      return false;
    }
  }

  function clearStaged() {
    for (const k of KINDS) {
      for (const p of staged.value[k]) {
        try {
          URL.revokeObjectURL(p.previewUrl);
        } catch {
          /* ignore */
        }
      }
      staged.value[k] = [];
    }
  }

  /**
   * One multipart POST per submit. On 2xx we always clear staged photos —
   * even when mergedPdfStale=true — per architecture §9.2: the uploads are
   * persisted, so retrying would duplicate them. On 4xx/5xx we keep staged
   * photos so the operator can retry without re-photographing.
   */
  async function submit(): Promise<SubmitResponse | null> {
    const ui = useUiStore();
    if (submitting.value) return null;
    if (!year.value || !currentOrderNo.value) return null;
    if (stagedCount.value === 0) {
      ui.error('请先添加至少一张图片');
      return null;
    }

    const form = new FormData();
    if (operator.value) form.append('operator', operator.value);
    for (const kind of KINDS) {
      const field = KIND_FIELD[kind];
      let idx = 0;
      for (const p of staged.value[kind]) {
        idx += 1;
        const extension = 'jpg';
        const fname = `${kind}-${idx}.${extension}`;
        form.append(field, p.blob, fname);
      }
    }

    submitting.value = true;
    try {
      const resp = await collectionApi.submit(year.value, currentOrderNo.value, form);
      // Success path: clear staged, refetch detail + year progress. Never
      // route-change (FR-SUBMIT-4).
      clearStaged();
      lastMergedPdfStale.value = resp.mergedPdfStale;
      await Promise.all([refreshDetail(), fetchProgress()]);
      ui.success('提交成功');
      if (resp.mergedPdfStale) {
        ui.info('合并 PDF 暂未生成，稍后管理员可重建');
      }
      return resp;
    } catch (err) {
      // Keep staged photos intact so the user can retry (FR-SUBMIT-5).
      const msg = err instanceof ApiError ? err.message : '提交失败，请稍后重试';
      ui.error(msg);
      return null;
    } finally {
      submitting.value = false;
    }
  }

  return {
    year,
    operator,
    progress,
    searchQuery,
    searchResults,
    searching,
    currentOrderNo,
    currentDetail,
    staged,
    submitting,
    lastMergedPdfStale,
    stagedCount,
    canSubmit,
    setYear,
    setOperator,
    fetchProgress,
    runSearch,
    openOrder,
    closeDetail,
    refreshDetail,
    stageFiles,
    removeStaged,
    clearStaged,
    deleteServerPhoto,
    submit,
  };
});
