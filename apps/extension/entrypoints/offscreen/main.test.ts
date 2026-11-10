import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const ORIGIN = 'http://127.0.0.1:4200';
const postMessage = vi.fn();
const sendMessage = vi.fn();
const contentWindow = { postMessage };
const frame = {
  src: '',
  contentWindow,
  remove: vi.fn(),
  onload: undefined as (() => void) | undefined,
};

let open: (raw: unknown, sender: { id: string }, respond: (reply: unknown) => void) => boolean;
let receive: (event: { origin: string; source: unknown; data: unknown }) => void;

beforeEach(async () => {
  vi.resetModules();
  vi.useFakeTimers();
  postMessage.mockClear();
  sendMessage.mockClear();
  vi.stubGlobal('chrome', {
    runtime: {
      id: 'own',
      sendMessage,
      onMessage: { addListener: (listener: typeof open) => (open = listener) },
    },
  });
  vi.stubGlobal('document', { createElement: () => frame, body: { append: vi.fn() } });
  vi.stubGlobal('window', {
    addEventListener: (_type: string, listener: typeof receive) => (receive = listener),
  });
  await import('./main');
  open({ target: 'offscreen', type: 'AUTH_OPEN' }, { id: 'own' }, vi.fn());
  frame.onload?.();
});

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe('ログイン開始の依頼', () => {
  it('ページの受信処理が遅れても届くよう、結果が来るまで送り直す', () => {
    expect(postMessage).toHaveBeenCalledTimes(1);
    vi.advanceTimersByTime(1500);
    expect(postMessage).toHaveBeenCalledTimes(4);
    expect(postMessage).toHaveBeenLastCalledWith({ type: 'AUTH_START' }, ORIGIN);
  });

  it('結果を受け取ったら送り直しを止め、結果を拡張へ渡す', () => {
    const data = { type: 'AUTH_RESULT', credential: { error: 'auth_failed' } };
    receive({ origin: ORIGIN, source: contentWindow, data });
    expect(sendMessage).toHaveBeenCalledWith(data);
    postMessage.mockClear();
    vi.advanceTimersByTime(1500);
    expect(postMessage).not.toHaveBeenCalled();
  });

  it('管理画面以外からの結果では送り直しを止めない', () => {
    const data = { type: 'AUTH_RESULT', credential: {} };
    receive({ origin: 'https://example.com', source: contentWindow, data });
    expect(sendMessage).not.toHaveBeenCalled();
    postMessage.mockClear();
    vi.advanceTimersByTime(500);
    expect(postMessage).toHaveBeenCalledTimes(1);
  });
});
