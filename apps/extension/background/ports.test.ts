import { beforeEach, describe, expect, it, vi } from 'vitest';
import { emptyState } from '../lib/schemas';
const mocks = vi.hoisted(() => ({ connection: vi.fn(), state: vi.fn(), owner: vi.fn() }));
vi.mock('./connection', () => ({ getConnection: mocks.connection, connectionVersion: () => 0 }));
vi.mock('./store', () => ({ getState: mocks.state }));
vi.mock('./viewing', () => ({ getOwner: mocks.owner }));
function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => {
    resolve = done;
  });
  return { promise, resolve };
}
async function flush() {
  for (let i = 0; i < 10; i++) await Promise.resolve();
}
beforeEach(() => {
  vi.resetModules();
  mocks.connection.mockReset();
  mocks.state.mockResolvedValue(emptyState);
  mocks.owner.mockResolvedValue(undefined);
});
describe('port delivery', () => {
  it('reads state after auth restoration and drops an older queued delivery', async () => {
    const first = deferred<{ accountId: string; authorization: string }>();
    const second = deferred<{ accountId: string; authorization: string }>();
    mocks.connection.mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise);
    let connect: (port: chrome.runtime.Port) => void = () => undefined;
    vi.stubGlobal('chrome', {
      runtime: {
        id: 'test-id',
        onConnect: {
          addListener: (listener: typeof connect) => {
            connect = listener;
          },
        },
      },
    });
    const post = vi.fn();
    const port = {
      name: 'dopagaki',
      sender: { id: 'test-id', url: 'https://www.youtube.com/shorts/test', tab: { id: 1 } },
      postMessage: post,
      onMessage: { addListener: vi.fn() },
      onDisconnect: { addListener: vi.fn() },
      disconnect: vi.fn(),
    } as unknown as chrome.runtime.Port;
    let current: typeof emptyState | undefined = undefined;
    mocks.state.mockImplementation(() => Promise.resolve(current));
    const { listenPorts, broadcast } = await import('./ports');
    listenPorts(() => undefined);
    connect(port);
    current = emptyState;
    const latest = broadcast();
    second.resolve({ accountId: 'u1', authorization: 'Demo synthetic' });
    await latest;
    first.resolve({ accountId: 'u1', authorization: 'Demo synthetic' });
    await flush();
    expect(post).toHaveBeenCalledTimes(1);
    expect(post.mock.calls[0]?.[0]).toMatchObject({ type: 'STATE', state: emptyState });
  });
  it('does not deliver cached user state after authentication is lost', async () => {
    mocks.connection.mockResolvedValue(undefined);
    let connect: (port: chrome.runtime.Port) => void = () => undefined;
    vi.stubGlobal('chrome', {
      runtime: {
        id: 'test-id',
        onConnect: {
          addListener: (listener: typeof connect) => {
            connect = listener;
          },
        },
      },
    });
    const post = vi.fn();
    const port = {
      name: 'dopagaki',
      sender: { id: 'test-id', url: 'https://www.youtube.com/shorts/test', tab: { id: 1 } },
      postMessage: post,
      onMessage: { addListener: vi.fn() },
      onDisconnect: { addListener: vi.fn() },
      disconnect: vi.fn(),
    } as unknown as chrome.runtime.Port;
    const { listenPorts } = await import('./ports');
    listenPorts(() => undefined);
    connect(port);
    await flush();
    expect(post).toHaveBeenCalledWith({ type: 'STATE', state: null, ownsViewing: false });
  });
});
