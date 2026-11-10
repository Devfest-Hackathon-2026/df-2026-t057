import { beforeEach, expect, it, vi } from 'vitest';
import { emptyState } from '../lib/schemas';
import type { Viewing } from '@dopagaki/contract';

const mocks = vi.hoisted(() => ({
  report: vi.fn(),
  state: vi.fn(),
  patch: vi.fn(),
  version: 0,
  account: 'u1',
}));
vi.mock('./api', () => ({ api: { reportViewing: mocks.report } }));
vi.mock('./connection', () => ({
  connectionVersion: () => mocks.version,
  getConnection: () => Promise.resolve({ accountId: mocks.account }),
}));
vi.mock('./store', () => ({ getState: mocks.state, patchState: mocks.patch }));

const sender: chrome.runtime.MessageSender = {
  url: 'https://www.youtube.com/shorts/synthetic',
  tab: { id: 1, active: true } as chrome.tabs.Tab,
};
const sample = (totalSeconds: number, flush = false) => ({
  type: 'CHECKPOINT_VIEWING' as const,
  accountId: 'u1',
  meterId: 'meter-0001',
  sessionId: 'view-1',
  totalSeconds,
  flush,
  closed: false,
});
let local: Map<string, unknown>;
let session: Map<string, unknown>;
let viewing: Viewing;
function sessionWith(consumedSeconds: number): NonNullable<Viewing['session']> {
  return { id: 'view-1', startedAt: '2026-11-10T00:00:00Z', plannedSeconds: 300, consumedSeconds };
}
function storage(data: Map<string, unknown>) {
  return {
    get: (keys: string | string[]) =>
      Promise.resolve(
        Object.fromEntries(
          (Array.isArray(keys) ? keys : [keys]).map((key) => [key, data.get(key)]),
        ),
      ),
    set: (items: Record<string, unknown>) => {
      for (const [key, value] of Object.entries(items)) data.set(key, structuredClone(value));
      return Promise.resolve();
    },
    remove: (key: string) => {
      data.delete(key);
      return Promise.resolve();
    },
  };
}
beforeEach(() => {
  vi.resetModules();
  vi.clearAllMocks();
  mocks.version = 0;
  mocks.account = 'u1';
  local = new Map();
  session = new Map([['viewingOwner', { tabId: 1, sessionId: 'view-1' }]]);
  viewing = {
    session: {
      id: 'view-1',
      startedAt: '2026-11-10T00:00:00Z',
      plannedSeconds: 300,
      consumedSeconds: 0,
    },
    reportCursor: 0,
    requiresSession: true,
    availableSeconds: 300,
    balanceSeconds: 300,
  };
  mocks.state.mockImplementation(() => Promise.resolve({ ...emptyState, viewing }));
  mocks.patch.mockImplementation((_version: number, patch: { viewing: Viewing }) => {
    viewing = patch.viewing;
    return Promise.resolve();
  });
  mocks.report.mockImplementation(
    (_id: string, seconds: number, _sessionId: string, from: number) =>
      Promise.resolve({
        ...viewing,
        reportCursor: from + seconds,
        acceptedSeconds: seconds,
        session: sessionWith(from + seconds),
      }),
  );
  vi.stubGlobal('chrome', { storage: { local: storage(local), session: storage(session) } });
});

it('recovers a three-second checkpoint after both the document and worker are gone', async () => {
  const first = await import('./viewing');
  await first.handleViewing(sample(3), sender);
  expect(mocks.report).not.toHaveBeenCalled();
  vi.resetModules();
  const restarted = await import('./viewing');
  await restarted.forgetTab(1);
  expect(mocks.report).toHaveBeenCalledWith(expect.any(String), 3, 'view-1', 0);
  expect(viewing.reportCursor).toBe(3);
  // BFCacheなどで同じ累積値が再送されても、新しい報告にしない。
  session.set('viewingOwner', { tabId: 1, sessionId: 'view-1' });
  await restarted.handleViewing(sample(3, true), sender);
  expect(mocks.report).toHaveBeenCalledTimes(1);
});

it('stores new samples while an earlier HTTP response is held', async () => {
  let finish!: (result: Viewing) => void;
  mocks.report.mockReturnValueOnce(
    new Promise<Viewing>((resolve) => {
      finish = resolve;
    }),
  );
  const { handleViewing, flushPendingReports } = await import('./viewing');
  const first = handleViewing(sample(3, true), sender);
  await vi.waitFor(() => {
    expect(mocks.report).toHaveBeenCalledTimes(1);
  });
  await handleViewing(sample(6), sender);
  const stored = local.get('viewingJournal') as { checkpoints: { total: number }[] };
  expect(stored.checkpoints[0]?.total).toBe(6);
  finish({
    ...viewing,
    reportCursor: 3,
    acceptedSeconds: 3,
    session: sessionWith(3),
  });
  await first;
  await flushPendingReports();
  expect(viewing.reportCursor).toBe(6);
});

it('retains an unaccepted remainder and uses a new receipt for that remainder', async () => {
  mocks.report.mockResolvedValueOnce({
    ...viewing,
    reportCursor: 1,
    acceptedSeconds: 1,
    session: sessionWith(1),
  });
  const { handleViewing, flushPendingReports } = await import('./viewing');
  await handleViewing(sample(3, true), sender);
  expect(viewing.reportCursor).toBe(1);
  await flushPendingReports();
  expect(mocks.report.mock.calls[1]?.slice(1)).toEqual([2, 'view-1', 1]);
  expect(mocks.report.mock.calls[1]?.[0]).not.toBe(mocks.report.mock.calls[0]?.[0]);
  const repeated = await handleViewing(sample(3, true), sender);
  expect(repeated.checkpoint?.acknowledgedSeconds).toBe(3);
  expect(mocks.report).toHaveBeenCalledTimes(2);
});

it('retries a lost response with the same receipt and never sends it under another account', async () => {
  const { NetworkError } = await import('@dopagaki/contract');
  mocks.report.mockRejectedValueOnce(new NetworkError(new Error('connection lost')));
  const { handleViewing, flushPendingReports, clearViewing } = await import('./viewing');
  await expect(handleViewing(sample(3, true), sender)).rejects.toThrow();
  await flushPendingReports();
  expect(mocks.report.mock.calls[1]?.[0]).toBe(mocks.report.mock.calls[0]?.[0]);
  mocks.version++;
  mocks.account = 'u2';
  await clearViewing();
  await flushPendingReports();
  viewing = { ...viewing, requiresSession: false, session: null, reportCursor: 0 };
  await expect(handleViewing(sample(3), sender)).rejects.toThrow();
  expect(mocks.report).toHaveBeenCalledTimes(2);
});

it('keeps the original receipt if saving its successful acknowledgement fails', async () => {
  const { handleViewing, flushPendingReports } = await import('./viewing');
  const area = storage(local);
  let fail = true;
  const set = area.set;
  area.set = (items: Record<string, unknown>) => {
    const journal = items['viewingJournal'] as { checkpoints: { acknowledged: number }[] };
    if (fail && journal.checkpoints.some((item) => item.acknowledged > 0)) {
      fail = false;
      return Promise.reject(new Error('storage write failed'));
    }
    return set(items);
  };
  vi.stubGlobal('chrome', { storage: { local: area, session: storage(session) } });
  await expect(handleViewing(sample(3, true), sender)).rejects.toThrow('storage write failed');
  await flushPendingReports();
  expect(mocks.report.mock.calls[1]?.[0]).toBe(mocks.report.mock.calls[0]?.[0]);
  expect(viewing.reportCursor).toBe(3);
});
