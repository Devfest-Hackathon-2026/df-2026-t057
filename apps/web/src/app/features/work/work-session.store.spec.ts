import { TestBed } from '@angular/core/testing';
import { FakeBackend, provideFakeBackend } from '../../../testing/fake-backend';
import { workSession } from '../../../testing/fixtures';
import { WorkSessionStore } from './work-session.store';

describe('WorkSessionStore', () => {
  let backend: FakeBackend;

  beforeEach(() => {
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval', 'Date'] });
    vi.setSystemTime(new Date('2026-11-08T10:00:00Z'));
    backend = new FakeBackend();
    TestBed.configureTestingModule({ providers: provideFakeBackend(backend) });
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('実行中は1秒ごとに経過時間を進め、上限で止める', () => {
    const store = TestBed.inject(WorkSessionStore);
    store.accept(workSession({ elapsedSeconds: 1795, limitSeconds: 1800 }));
    TestBed.tick();
    expect(store.elapsedSeconds()).toBe(1795);
    vi.advanceTimersByTime(3000);
    expect(store.elapsedSeconds()).toBe(1798);
    vi.advanceTimersByTime(10_000);
    expect(store.elapsedSeconds()).toBe(1800);
    expect(store.progress()).toBe(100);
    expect(store.limitMinutes()).toBe(30);
  });

  it('一時停止中は進めない', () => {
    const store = TestBed.inject(WorkSessionStore);
    store.accept(workSession({ elapsedSeconds: 60, running: false }));
    TestBed.tick();
    vi.advanceTimersByTime(5000);
    expect(store.elapsedSeconds()).toBe(60);
  });

  it('記録すると作業の状態を消す', async () => {
    backend.on('POST', '/work-session/confirm', {
      body: { sessionId: 's1', taskId: 't1', workSeconds: 60, rewardSeconds: 0, deferred: false },
    });
    const store = TestBed.inject(WorkSessionStore);
    store.accept(workSession());
    vi.useRealTimers();
    const outcome = await store.confirmWork();
    expect(outcome.ok).toBe(true);
    expect(store.session()).toBeNull();
    expect(backend.requests[0]?.body).toEqual({ sessionId: 's1' });
  });
});
