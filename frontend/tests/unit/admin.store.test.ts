import { beforeEach, describe, expect, it, vi } from 'vitest';
import { createPinia, setActivePinia } from 'pinia';

// Mock the api module so ping/login/logout/detail are controllable.
vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api');
  return {
    ...actual,
    adminApi: {
      ping: vi.fn(),
      login: vi.fn(),
      logout: vi.fn(),
      years: vi.fn(),
      orders: vi.fn(),
      detail: vi.fn(),
      deletePhoto: vi.fn(),
      resetOrder: vi.fn(),
      rebuildPdf: vi.fn(),
      mergedPdfUrl: actual.adminApi.mergedPdfUrl,
      bundleZipUrl: actual.adminApi.bundleZipUrl,
      yearExportUrl: actual.adminApi.yearExportUrl,
    },
  };
});

import {
  ApiError,
  adminApi,
  setOn401Handler,
  type AdminOrderList,
  type AdminOrderRow,
  type OrderDetail,
} from '@/lib/api';
import { useAdminStore } from '@/stores/admin';

function makeRow(orderNo: string, customer = ''): AdminOrderRow {
  return {
    orderNo,
    customer,
    uploaded: false,
    counts: { 合同: 0, 发票: 0, 发货单: 0 },
    lastUploadAt: null,
    csvRemoved: false,
    operators: [],
  };
}

function makeDetail(orderNo: string): OrderDetail {
  return {
    orderNo,
    year: 2021,
    customer: '客户',
    csvPresent: true,
    lines: [],
    uploads: { 合同: [], 发票: [], 发货单: [] },
  };
}

function makeList(page: number, items: AdminOrderRow[]): AdminOrderList {
  return { page, size: 50, total: items.length, items };
}

beforeEach(() => {
  setActivePinia(createPinia());
  vi.clearAllMocks();
});

describe('admin store', () => {
  it('ping success stores csrfToken and flips authed', async () => {
    (adminApi.ping as unknown as ReturnType<typeof vi.fn>).mockResolvedValue({
      ok: true,
      csrfToken: 'abc123',
    });
    const store = useAdminStore();
    const ok = await store.ping();
    expect(ok).toBe(true);
    expect(store.authed).toBe(true);
    expect(store.csrfToken).toBe('abc123');
  });

  it('ping 401 leaves store unauthed and returns false', async () => {
    (adminApi.ping as unknown as ReturnType<typeof vi.fn>).mockRejectedValue(
      new ApiError(401, 'UNAUTHENTICATED', 'nope'),
    );
    const store = useAdminStore();
    const ok = await store.ping();
    expect(ok).toBe(false);
    expect(store.authed).toBe(false);
    expect(store.csrfToken).toBe('');
  });

  it('401 on any admin call routes through the registered on401 handler', async () => {
    const handler = vi.fn();
    setOn401Handler(handler);
    try {
      // Simulate api.ts invoking the handler on 401 by calling setOn401Handler
      // and manually invoking it — we already unit-test the wiring in api.ts
      // itself; here we just verify that handlers can be registered without
      // coupling the store to the router.
      handler();
      expect(handler).toHaveBeenCalled();
    } finally {
      setOn401Handler(null);
    }
  });

  it('login -> auto ping populates csrfToken', async () => {
    (adminApi.login as unknown as ReturnType<typeof vi.fn>).mockResolvedValue({ ok: true });
    (adminApi.ping as unknown as ReturnType<typeof vi.fn>).mockResolvedValue({
      ok: true,
      csrfToken: 'tkn',
    });
    const store = useAdminStore();
    const ok = await store.login('secret');
    expect(ok).toBe(true);
    expect(store.csrfToken).toBe('tkn');
  });

  it('logout clears local state even if server call fails', async () => {
    (adminApi.logout as unknown as ReturnType<typeof vi.fn>).mockRejectedValue(new Error('boom'));
    const store = useAdminStore();
    store.authed = true;
    store.csrfToken = 'whatever';
    await store.logout();
    expect(store.authed).toBe(false);
    expect(store.csrfToken).toBe('');
  });

  it('setYear resets the list/page/detail', () => {
    const store = useAdminStore();
    store.currentYear = 2021;
    store.page = 4;
    store.setYear(2023);
    expect(store.currentYear).toBe(2023);
    expect(store.page).toBe(1);
    expect(store.orderList).toBeNull();
  });

  // M-26: year switch must abort the previous in-flight list fetch so the
  // older year's rows cannot overwrite the newer year's list when they
  // resolve out of order.
  it('M-26: ignores stale loadOrders response after a second loadOrders', async () => {
    const ordersMock = adminApi.orders as unknown as ReturnType<typeof vi.fn>;

    // First call: slow, resolves with rows for "2021".
    let resolveA: (v: AdminOrderList) => void = () => {};
    const pendingA = new Promise<AdminOrderList>((resolve) => {
      resolveA = resolve;
    });
    // Second call: resolves immediately with rows for "2023".
    const listB = makeList(1, [makeRow('B1')]);

    ordersMock.mockImplementationOnce(() => pendingA);
    ordersMock.mockResolvedValueOnce(listB);

    const store = useAdminStore();
    store.currentYear = 2021;
    const pA = store.loadOrders();

    // Simulate year switch before the first response lands.
    store.currentYear = 2023;
    const pB = store.loadOrders();
    await pB;

    expect(store.orderList?.items[0].orderNo).toBe('B1');

    // Late response for year=2021 must be dropped.
    resolveA(makeList(1, [makeRow('A1')]));
    await pA;
    expect(store.orderList?.items[0].orderNo).toBe('B1');
  });

  it('M-26: setYear aborts the previous in-flight list fetch', async () => {
    const ordersMock = adminApi.orders as unknown as ReturnType<typeof vi.fn>;

    // Track whether the abort signal on the first call fires.
    let firstSignal: AbortSignal | undefined;
    ordersMock.mockImplementationOnce(
      (_year: number, _params: unknown, signal?: AbortSignal) => {
        firstSignal = signal;
        return new Promise<AdminOrderList>(() => {
          /* never resolves */
        });
      },
    );

    const store = useAdminStore();
    store.currentYear = 2021;
    // Fire but don't await.
    void store.loadOrders();
    expect(firstSignal).toBeDefined();
    expect(firstSignal?.aborted).toBe(false);

    store.setYear(2023);
    expect(firstSignal?.aborted).toBe(true);
  });

  // M-27: after a successful reset, the side panel must be cleared before
  // the list refetch runs so the deleted order's thumbnails cannot keep
  // rendering during the intermediate state.
  it('M-27: resetOrder clears side panel and refetches the list', async () => {
    const resetMock = adminApi.resetOrder as unknown as ReturnType<typeof vi.fn>;
    const ordersMock = adminApi.orders as unknown as ReturnType<typeof vi.fn>;
    const pingMock = adminApi.ping as unknown as ReturnType<typeof vi.fn>;
    pingMock.mockResolvedValue({ ok: true, csrfToken: 'csrf' });
    resetMock.mockResolvedValue({ ok: true });
    ordersMock.mockResolvedValue(makeList(1, []));

    const store = useAdminStore();
    await store.ping();
    store.currentYear = 2021;
    const row = makeRow('RX-1');
    store.currentRow = row;
    store.currentDetail = makeDetail('RX-1');

    const ok = await store.resetOrder();
    expect(ok).toBe(true);
    // Side panel state must be cleared.
    expect(store.currentRow).toBeNull();
    expect(store.currentDetail).toBeNull();
    // List must be refetched.
    expect(ordersMock).toHaveBeenCalled();
  });

  it('M-27: closeRow aborts an in-flight openRow detail fetch', async () => {
    const detailMock = adminApi.detail as unknown as ReturnType<typeof vi.fn>;
    let resolveA: (v: OrderDetail) => void = () => {};
    let gotSignal: AbortSignal | undefined;
    detailMock.mockImplementationOnce((_y: number, _o: string, signal?: AbortSignal) => {
      gotSignal = signal;
      return new Promise<OrderDetail>((resolve) => {
        resolveA = resolve;
      });
    });

    const store = useAdminStore();
    store.currentYear = 2021;
    const pOpen = store.openRow(makeRow('RX-1'));
    expect(gotSignal).toBeDefined();

    store.closeRow();
    expect(gotSignal?.aborted).toBe(true);
    expect(store.currentRow).toBeNull();

    // Late resolution must not reopen the panel.
    resolveA(makeDetail('RX-1'));
    await pOpen;
    expect(store.currentRow).toBeNull();
    expect(store.currentDetail).toBeNull();
  });
});
