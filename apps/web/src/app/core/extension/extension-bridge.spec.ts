import { signal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import type { PageMessage } from '@dopagaki/bridge';
import { createApiClient } from '@dopagaki/contract';
import { FakeBackend, flush } from '../../../testing/fake-backend';
import { AccountScope } from '../auth/account-scope';
import { AuthService, type Account } from '../auth/auth.service';
import { API_CLIENT } from '../http/api-client';
import { ExtensionBridge } from './extension-bridge';

describe('ExtensionBridge', () => {
  const signedIn = signal(true);
  const mode = signal<'demo' | 'live'>('live');
  const idToken = signal<string | undefined>('t1');
  const account = signal<Account | undefined>({ id: 'uid-1', email: '', name: '', photoUrl: '' });
  let posted: PageMessage[];
  let backend: FakeBackend;

  beforeEach(() => {
    posted = [];
    signedIn.set(true);
    mode.set('live');
    idToken.set('t1');
    account.set({ id: 'uid-1', email: '', name: '', photoUrl: '' });
    vi.spyOn(window, 'postMessage').mockImplementation((message: unknown) => {
      posted.push(message as PageMessage);
    });
    backend = new FakeBackend().on('POST', '/extension/token', {
      body: { authorization: 'Demo abc' },
    });
    TestBed.configureTestingModule({
      providers: [
        { provide: AuthService, useValue: { signedIn, mode, idToken, account } },
        {
          provide: API_CLIENT,
          useValue: createApiClient({
            origin: 'http://localhost',
            authorization: () => undefined,
            fetch: backend.fetch,
          }),
        },
      ],
    });
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('live はアカウントIDだけを渡し、Webのトークンは送らない', async () => {
    TestBed.inject(ExtensionBridge);
    TestBed.tick();
    await flush();
    expect(posted.at(-1)).toEqual({
      type: 'DOPAGAKI_CONNECT',
      origin: location.origin,
      accountId: 'uid-1',
    });
    idToken.set('t2');
    TestBed.tick();
    await flush();
    expect(posted.at(-1)).toMatchObject({ type: 'DOPAGAKI_CONNECT', accountId: 'uid-1' });
  });

  it('アカウントの切り替えとログアウトで切断を送る', async () => {
    TestBed.inject(ExtensionBridge);
    TestBed.tick();
    await flush();
    TestBed.inject(AccountScope).reset();
    expect(posted.at(-1)).toEqual({ type: 'DOPAGAKI_DISCONNECT', accountId: 'uid-1' });
    signedIn.set(false);
    account.set(undefined);
    TestBed.tick();
    await flush();
    expect(posted.at(-1)).toEqual({ type: 'DOPAGAKI_DISCONNECT', accountId: 'uid-1' });
  });

  it('デモは拡張用のトークンを一度だけ発行して渡す', async () => {
    mode.set('demo');
    account.set({ id: 'demo', email: '', name: '', photoUrl: '' });
    TestBed.inject(ExtensionBridge);
    TestBed.tick();
    await flush(10);
    const connect = posted.at(-1);
    expect(connect).toMatchObject({ type: 'DOPAGAKI_CONNECT', authorization: 'Demo abc' });
    expect(connect?.type === 'DOPAGAKI_CONNECT' && connect.accountId).toMatch(
      /^demo-[0-9a-f]{64}$/,
    );
    expect(backend.count('POST', '/extension/token')).toBe(1);
  });

  it('拡張の READY を受けたら接続の情報を送り直す', async () => {
    const bridge = TestBed.inject(ExtensionBridge);
    TestBed.tick();
    await flush();
    posted.length = 0;
    window.dispatchEvent(
      new MessageEvent('message', {
        data: { type: 'DOPAGAKI_EXTENSION_READY', version: '1.2.3' },
        origin: location.origin,
        source: window,
      }),
    );
    await flush();
    expect(bridge.extensionVersion()).toBe('1.2.3');
    expect(posted.at(-1)).toMatchObject({ type: 'DOPAGAKI_CONNECT', accountId: 'uid-1' });
  });

  it('ほかのオリジンや形の違うメッセージは無視する', async () => {
    const bridge = TestBed.inject(ExtensionBridge);
    TestBed.tick();
    await flush();
    window.dispatchEvent(
      new MessageEvent('message', {
        data: { type: 'DOPAGAKI_EXTENSION_READY', version: '1' },
        origin: 'https://evil.example',
        source: window,
      }),
    );
    window.dispatchEvent(
      new MessageEvent('message', {
        data: { type: 'DOPAGAKI_EXTENSION_READY' },
        origin: location.origin,
        source: window,
      }),
    );
    await flush();
    expect(bridge.extensionVersion()).toBeUndefined();
  });
});
