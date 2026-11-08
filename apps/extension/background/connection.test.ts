import { beforeEach, describe, expect, it, vi } from 'vitest';

function fakeStorage() {
  const data = new Map<string, unknown>();
  return {
    data,
    get: vi.fn((key: string) => Promise.resolve({ [key]: data.get(key) })),
    set: vi.fn((items: Record<string, unknown>) => {
      for (const [key, value] of Object.entries(items)) data.set(key, value);
      return Promise.resolve();
    }),
    remove: vi.fn((key: string) => {
      data.delete(key);
      return Promise.resolve();
    }),
  };
}

let session: ReturnType<typeof fakeStorage>;
let local: ReturnType<typeof fakeStorage>;

beforeEach(() => {
  vi.resetModules();
  session = fakeStorage();
  local = fakeStorage();
  vi.stubGlobal('chrome', { storage: { session, local } });
});

describe('replaceConnection', () => {
  it('replaces a refreshed token without clearing the account', async () => {
    const { connectionVersion, getConnection, replaceConnection } = await import('./connection');
    const clear = vi.fn(() => Promise.resolve());
    expect(await replaceConnection({ authorization: 'Bearer a', accountId: 'u1' }, clear)).toBe(
      'switched',
    );
    const version = connectionVersion();
    expect(await replaceConnection({ authorization: 'Bearer b', accountId: 'u1' }, clear)).toBe(
      'refreshed',
    );
    expect(clear).toHaveBeenCalledTimes(1);
    expect(connectionVersion()).toBe(version);
    expect((await getConnection())?.authorization).toBe('Bearer b');
    expect(await replaceConnection({ authorization: 'Bearer b', accountId: 'u1' }, clear)).toBe(
      'unchanged',
    );
  });

  it('clears everything and invalidates in-flight work when the account changes', async () => {
    const { connectionVersion, getConnection, replaceConnection } = await import('./connection');
    const clear = vi.fn(() => Promise.resolve());
    await replaceConnection({ authorization: 'Bearer a', accountId: 'u1' }, clear);
    const version = connectionVersion();
    expect(await replaceConnection({ authorization: 'Bearer c', accountId: 'u2' }, clear)).toBe(
      'switched',
    );
    expect(clear).toHaveBeenCalledTimes(2);
    expect(connectionVersion()).toBeGreaterThan(version);
    expect((await getConnection())?.accountId).toBe('u2');
  });

  it('forgets the connection on disconnect', async () => {
    const { getConnection, replaceConnection } = await import('./connection');
    const clear = vi.fn(() => Promise.resolve());
    await replaceConnection({ authorization: 'Bearer a', accountId: 'u1' }, clear);
    await replaceConnection(undefined, clear);
    expect(await getConnection()).toBeUndefined();
    expect(session.data.has('connection')).toBe(false);
    expect(local.data.has('accountId')).toBe(false);
  });

  it('keeps the data when the same account reconnects after the browser restarts', async () => {
    const { replaceConnection } = await import('./connection');
    const clear = vi.fn(() => Promise.resolve());
    await replaceConnection({ authorization: 'Bearer a', accountId: 'u1' }, clear);
    session.data.clear(); // chrome.storage.session はブラウザーを閉じると消える
    expect(await replaceConnection({ authorization: 'Bearer b', accountId: 'u1' }, clear)).toBe(
      'refreshed',
    );
    expect(clear).toHaveBeenCalledTimes(1);
    expect(local.data.get('accountId')).toBe('u1');
  });

  it('ignores a malformed stored value', async () => {
    session.data.set('connection', { authorization: 42 });
    const { getConnection } = await import('./connection');
    expect(await getConnection()).toBeUndefined();
  });
});
