import { signal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { AccountScope } from '../auth/account-scope';
import { AuthService } from '../auth/auth.service';
import { FakeEventSource } from '../../../testing/fake-backend';
import {
  EVENT_SOURCE_FACTORY,
  RealtimeService,
  parseChangeEvent,
  retryDelayMs,
} from './realtime.service';

describe('parseChangeEvent', () => {
  it('知っているリソースと整数の版だけを受け付ける', () => {
    expect(parseChangeEvent('{"resource":"wallet","revision":42}')).toEqual({
      resource: 'wallet',
      revision: 42,
    });
    expect(parseChangeEvent('{"resource":"unknown","revision":1}')).toBeUndefined();
    expect(parseChangeEvent('{"resource":"wallet","revision":"1"}')).toBeUndefined();
    expect(parseChangeEvent('{"resource":"wallet","revision":1.5}')).toBeUndefined();
    expect(parseChangeEvent('not json')).toBeUndefined();
    expect(parseChangeEvent(42)).toBeUndefined();
  });
});

describe('retryDelayMs', () => {
  it('失敗するたびに倍にし、60秒で止める', () => {
    expect(retryDelayMs(0, 1)).toBe(1000);
    expect(retryDelayMs(1, 1)).toBe(2000);
    expect(retryDelayMs(3, 1)).toBe(8000);
    expect(retryDelayMs(10, 1)).toBe(60_000);
    expect(retryDelayMs(0, 0)).toBe(800);
  });
});

describe('RealtimeService', () => {
  const signedIn = signal(true);
  const mode = signal<'demo' | 'live'>('live');
  const idToken = signal<string | undefined>('token-1');

  beforeEach(() => {
    FakeEventSource.instances = [];
    signedIn.set(true);
    mode.set('live');
    idToken.set('token-1');
    TestBed.configureTestingModule({
      providers: [
        { provide: AuthService, useValue: { signedIn, mode, idToken } },
        { provide: EVENT_SOURCE_FACTORY, useValue: (url: string) => new FakeEventSource(url) },
      ],
    });
  });

  it('live は ID トークンを付けて接続し、トークンが更新されたらつなぎ直す', () => {
    TestBed.inject(RealtimeService);
    TestBed.tick();
    expect(FakeEventSource.latest().url).toBe('/api/events?token=token-1');
    idToken.set('token-2');
    TestBed.tick();
    expect(FakeEventSource.instances[0]?.closed).toBe(true);
    expect(FakeEventSource.latest().url).toBe('/api/events?token=token-2');
  });

  it('デモは Cookie で接続し、ログアウトしたら切る', () => {
    mode.set('demo');
    TestBed.inject(RealtimeService);
    TestBed.tick();
    expect(FakeEventSource.latest().url).toBe('/api/events');
    signedIn.set(false);
    TestBed.tick();
    expect(FakeEventSource.latest().closed).toBe(true);
  });

  it('通知を流し、一番新しい版を覚える', () => {
    const service = TestBed.inject(RealtimeService);
    TestBed.tick();
    const received: unknown[] = [];
    service.changes$.subscribe((event) => received.push(event));
    FakeEventSource.latest().emit({ resource: 'tasks', revision: 3 });
    FakeEventSource.latest().emit({ resource: 'tasks', revision: 2 });
    expect(received).toHaveLength(2);
    expect(service.latestRevision('tasks')).toBe(3);
    TestBed.inject(AccountScope).reset();
    expect(service.latestRevision('tasks')).toBeUndefined();
  });

  it('失敗したら間隔を空けてつなぎ直し、つなぎ直したことを知らせる', () => {
    vi.useFakeTimers();
    try {
      const service = TestBed.inject(RealtimeService);
      TestBed.tick();
      let reconnects = 0;
      service.reconnected$.subscribe(() => (reconnects += 1));
      FakeEventSource.latest().open();
      expect(reconnects).toBe(0);
      FakeEventSource.latest().fail();
      expect(service.state()).toBe('retrying');
      expect(FakeEventSource.instances).toHaveLength(1);
      vi.advanceTimersByTime(1000);
      expect(FakeEventSource.instances).toHaveLength(2);
      FakeEventSource.latest().fail();
      vi.advanceTimersByTime(1000);
      // 2回目は約2秒空ける
      expect(FakeEventSource.instances).toHaveLength(2);
      vi.advanceTimersByTime(1000);
      expect(FakeEventSource.instances).toHaveLength(3);
      FakeEventSource.latest().open();
      expect(reconnects).toBe(1);
      expect(service.state()).toBe('open');
    } finally {
      vi.useRealTimers();
    }
  });
});
