import { beforeEach, expect, it, vi } from 'vitest';
import { emptyState } from '../lib/schemas';

const mocks = vi.hoisted(() => ({ start: vi.fn(), tab: vi.fn(), patch: vi.fn() }));
vi.mock('./api', () => ({ api: { startViewing: mocks.start } }));
vi.mock('./connection', () => ({
  connectionVersion: () => 0,
  getConnection: () => Promise.resolve({ accountId: 'u1' }),
}));
vi.mock('./store', () => ({
  getState: () => Promise.resolve(emptyState),
  patchState: mocks.patch,
}));

beforeEach(() => {
  vi.resetModules();
  vi.clearAllMocks();
  mocks.start.mockResolvedValue({ session: { id: 'view-1' } });
  mocks.tab.mockResolvedValue({ id: 1, active: true, url: 'https://www.youtube.com/shorts/test' });
  vi.stubGlobal('chrome', {
    tabs: { get: mocks.tab },
    storage: {
      local: { get: () => Promise.resolve({}), remove: () => Promise.resolve() },
      session: { set: () => Promise.resolve() },
    },
  });
});

it('starts after an in-page navigation from a normal YouTube URL to Shorts', async () => {
  const { handleViewing } = await import('./viewing');
  await handleViewing(
    { type: 'START_VIEWING', sessionId: 'view-1', seconds: 300 },
    {
      url: 'https://www.youtube.com/watch?v=synthetic',
      tab: { id: 1, active: true } as chrome.tabs.Tab,
    },
  );
  expect(mocks.start).toHaveBeenCalledWith('view-1', 300, undefined);
});

it.each([
  { id: 1, active: true, url: 'https://www.youtube.com/watch?v=synthetic' },
  { id: 1, active: false, url: 'https://www.youtube.com/shorts/test' },
  { id: 1, active: true, url: 'https://example.com/shorts/test' },
])('rejects choosing from a tab that is no longer a foreground Shorts page: %o', async (tab) => {
  mocks.tab.mockResolvedValue(tab);
  const { handleViewing } = await import('./viewing');
  await expect(
    handleViewing(
      { type: 'START_VIEWING', sessionId: 'view-1', seconds: 300 },
      {
        url: 'https://www.youtube.com/shorts/test',
        tab: { id: 1, active: true } as chrome.tabs.Tab,
      },
    ),
  ).rejects.toThrow();
  expect(mocks.start).not.toHaveBeenCalled();
});
