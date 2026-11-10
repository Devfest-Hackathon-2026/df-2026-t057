import { beforeEach, expect, it, vi } from 'vitest';

const mocks = vi.hoisted(() => ({
  register: vi.fn(),
  inject: vi.fn(),
  added: vi.fn(),
  contains: vi.fn(),
}));

beforeEach(() => {
  vi.resetModules();
  vi.clearAllMocks();
  mocks.register.mockResolvedValue(undefined);
  mocks.inject.mockResolvedValue([]);
  mocks.contains.mockResolvedValue(true);
  vi.stubGlobal('chrome', {
    permissions: { onAdded: { addListener: mocks.added }, contains: mocks.contains },
    tabs: {
      query: () => Promise.resolve([{ id: 1, url: 'https://example.com/work' }]),
      get: () => Promise.resolve({ id: 1, url: 'https://example.com/work' }),
    },
    scripting: {
      getRegisteredContentScripts: () => Promise.resolve([]),
      registerContentScripts: mocks.register,
      executeScript: mocks.inject,
    },
  });
});

it('finishes first-grant registration and injection without a popup callback', async () => {
  const { listenWorkSites } = await import('./worksites');
  listenWorkSites();
  const listener = mocks.added.mock.calls[0]?.[0] as (
    permission: chrome.permissions.Permissions,
  ) => void;
  listener({ origins: ['https://example.com/*'] });
  await vi.waitFor(() => {
    expect(mocks.inject).toHaveBeenCalledWith({
      target: { tabId: 1 },
      files: ['content-scripts/worksite.js'],
    });
  });
  expect(mocks.register).toHaveBeenCalledTimes(1);
});

it('does not register or inject a work site without its existing permission', async () => {
  mocks.contains.mockResolvedValue(false);
  const { enableWorkSite } = await import('./worksites');
  await expect(enableWorkSite(1)).rejects.toThrow();
  expect(mocks.register).not.toHaveBeenCalled();
  expect(mocks.inject).not.toHaveBeenCalled();
});
