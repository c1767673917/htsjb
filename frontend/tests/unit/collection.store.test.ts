import { beforeEach, describe, expect, it, vi } from 'vitest';
import { createPinia, setActivePinia } from 'pinia';

// Mock the api module before importing the store so the store picks up the mock.
vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api');
  return {
    ...actual,
    collectionApi: {
      progress: vi.fn(),
      search: vi.fn(),
      detail: vi.fn(),
      submit: vi.fn(),
    },
  };
});

// Mock the image pipeline so stageFiles doesn't try to invoke real canvas APIs.
vi.mock('@/lib/imagePipeline', async () => {
  const actual = await vi.importActual<typeof import('@/lib/imagePipeline')>('@/lib/imagePipeline');
  return {
    ...actual,
    processImage: vi.fn(async (file: File) => ({
      blob: new Blob([new Uint8Array(10)], { type: 'image/jpeg' }),
      mime: 'image/jpeg',
      width: 800,
      height: 600,
      origSize: file.size,
      outSize: 10,
    })),
  };
});

import { ApiError, collectionApi, type OrderDetail } from '@/lib/api';
import { useCollectionStore } from '@/stores/collection';

function makeFile(name = 'x.jpg'): File {
  return new File([new Uint8Array(128)], name, { type: 'image/jpeg' });
}

function makeDetail(overrides: Partial<OrderDetail> = {}): OrderDetail {
  return {
    orderNo: 'RX2101-22926',
    year: 2021,
    customer: '哈尔滨金诺食品有限公司',
    csvPresent: true,
    checkStatus: '未检查',
    lines: [],
    uploads: { 合同: [], 发票: [], 发货单: [] },
    ...overrides,
  };
}

// jsdom needs a URL polyfill shim for URL.createObjectURL / revokeObjectURL.
beforeEach(() => {
  if (!URL.createObjectURL) {
    // @ts-ignore jsdom stub
    URL.createObjectURL = () => 'blob:mock';
  }
  if (!URL.revokeObjectURL) {
    // @ts-ignore jsdom stub
    URL.revokeObjectURL = () => {};
  }
  setActivePinia(createPinia());
  vi.clearAllMocks();
});

describe('collection store', () => {
  it('stages files into the chosen kind bucket', async () => {
    const store = useCollectionStore();
    store.setYear(2021);
    await store.stageFiles('合同', [makeFile()]);
    expect(store.staged.合同.length).toBe(1);
    expect(store.staged.发票.length).toBe(0);
  });

  it('submit success clears staged photos and refetches progress + detail', async () => {
    (collectionApi.detail as unknown as ReturnType<typeof vi.fn>).mockResolvedValue(makeDetail());
    (collectionApi.progress as unknown as ReturnType<typeof vi.fn>).mockResolvedValue({
      total: 100,
      uploaded: 1,
      percent: 0.01,
    });
    (collectionApi.submit as unknown as ReturnType<typeof vi.fn>).mockResolvedValue({
      counts: { 合同: 1, 发票: 0, 发货单: 0 },
      progress: { total: 100, uploaded: 1, percent: 0.01 },
      mergedPdfStale: false,
    });

    const store = useCollectionStore();
    store.setYear(2021);
    await store.openOrder('RX2101-22926');
    await store.stageFiles('合同', [makeFile()]);
    expect(store.stagedCount).toBe(1);

    const resp = await store.submit();
    expect(resp?.mergedPdfStale).toBe(false);
    expect(store.stagedCount).toBe(0);
    expect(collectionApi.submit).toHaveBeenCalledTimes(1);
    // Detail refetch happens after submit in addition to the openOrder call.
    expect(collectionApi.detail).toHaveBeenCalledTimes(2);
    expect(store.progress).toEqual({ total: 100, uploaded: 1, percent: 0.01 });
  });

  it('submit error keeps staged photos intact', async () => {
    (collectionApi.detail as unknown as ReturnType<typeof vi.fn>).mockResolvedValue(makeDetail());
    (collectionApi.submit as unknown as ReturnType<typeof vi.fn>).mockRejectedValue(
      new ApiError(500, 'INTERNAL', 'boom'),
    );

    const store = useCollectionStore();
    store.setYear(2021);
    await store.openOrder('RX2101-22926');
    await store.stageFiles('合同', [makeFile()]);
    const before = store.stagedCount;
    const resp = await store.submit();
    expect(resp).toBeNull();
    expect(store.stagedCount).toBe(before);
    expect(store.stagedCount).toBeGreaterThan(0);
  });

  it('mergedPdfStale=true still clears staged photos (submit treated as success)', async () => {
    (collectionApi.detail as unknown as ReturnType<typeof vi.fn>).mockResolvedValue(makeDetail());
    (collectionApi.progress as unknown as ReturnType<typeof vi.fn>).mockResolvedValue({
      total: 100,
      uploaded: 2,
      percent: 0.02,
    });
    (collectionApi.submit as unknown as ReturnType<typeof vi.fn>).mockResolvedValue({
      counts: { 合同: 2, 发票: 1, 发货单: 0 },
      progress: { total: 100, uploaded: 2, percent: 0.02 },
      mergedPdfStale: true,
    });

    const store = useCollectionStore();
    store.setYear(2021);
    await store.openOrder('RX2101-22926');
    await store.stageFiles('发票', [makeFile()]);
    const resp = await store.submit();
    expect(resp?.mergedPdfStale).toBe(true);
    expect(store.lastMergedPdfStale).toBe(true);
    expect(store.stagedCount).toBe(0);
  });

  it('runSearch below 2 chars clears results without hitting the API', async () => {
    const store = useCollectionStore();
    store.setYear(2021);
    await store.runSearch('a');
    expect(store.searchResults).toEqual([]);
    expect(collectionApi.search).not.toHaveBeenCalled();
  });

  // M-25: openOrder race — a slower earlier detail fetch must NOT clobber the
  // newer order's detail when its response resolves later.
  it('M-25: ignores stale detail response when a second openOrder is issued', async () => {
    const detailMock = collectionApi.detail as unknown as ReturnType<typeof vi.fn>;

    // First call: resolves slowly with order A.
    let resolveA: (v: OrderDetail) => void = () => {};
    const pendingA = new Promise<OrderDetail>((resolve) => {
      resolveA = resolve;
    });
    // Second call: resolves immediately with order B.
    const detailB = makeDetail({ orderNo: 'RX-B', customer: '客户B' });

    detailMock.mockImplementationOnce(() => pendingA);
    detailMock.mockResolvedValueOnce(detailB);

    const store = useCollectionStore();
    store.setYear(2021);

    // Fire both in quick succession; openOrder awaits refreshDetail so we
    // drive them as overlapping promises.
    const pA = store.openOrder('RX-A');
    const pB = store.openOrder('RX-B');

    await pB;
    expect(store.currentOrderNo).toBe('RX-B');
    expect(store.currentDetail?.orderNo).toBe('RX-B');

    // Now the late response for A lands; the store must drop it.
    resolveA(makeDetail({ orderNo: 'RX-A', customer: '客户A' }));
    await pA;

    expect(store.currentOrderNo).toBe('RX-B');
    expect(store.currentDetail?.orderNo).toBe('RX-B');
  });

  it('M-25: closeDetail aborts any in-flight detail fetch', async () => {
    const detailMock = collectionApi.detail as unknown as ReturnType<typeof vi.fn>;
    let resolveA: (v: OrderDetail) => void = () => {};
    const pendingA = new Promise<OrderDetail>((resolve) => {
      resolveA = resolve;
    });
    detailMock.mockImplementationOnce(() => pendingA);

    const store = useCollectionStore();
    store.setYear(2021);
    const pA = store.openOrder('RX-A');

    store.closeDetail();
    expect(store.currentOrderNo).toBeNull();

    resolveA(makeDetail({ orderNo: 'RX-A' }));
    await pA;

    // Stale response must not re-open the detail panel after closeDetail.
    expect(store.currentOrderNo).toBeNull();
    expect(store.currentDetail).toBeNull();
  });
});
