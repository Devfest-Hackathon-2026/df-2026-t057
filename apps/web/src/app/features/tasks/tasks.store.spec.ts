import { TestBed } from '@angular/core/testing';
import { messages } from '@dopagaki/messages';
import { AccountScope } from '../../core/auth/account-scope';
import { RealtimeService } from '../../core/realtime/realtime.service';
import {
  FakeBackend,
  FakeEventSource,
  flush,
  provideFakeBackend,
} from '../../../testing/fake-backend';
import { draft, task, taskList, wallet, workSession } from '../../../testing/fixtures';
import { TasksStore } from './tasks.store';
import { WorkSessionStore } from '../work/work-session.store';

describe('TasksStore', () => {
  let backend: FakeBackend;
  let store: TasksStore;

  beforeEach(() => {
    backend = new FakeBackend()
      .on('GET', '/tasks', { body: taskList([task()], 3) })
      .on('GET', '/wallet', { body: wallet() })
      .on('GET', '/work-session', { body: { session: null } });
    TestBed.configureTestingModule({ providers: provideFakeBackend(backend) });
    TestBed.inject(RealtimeService);
    store = TestBed.inject(TasksStore);
    TestBed.tick();
  });

  it('読み込むと課題と計画の版を持つ', async () => {
    await store.refresh();
    expect(store.tasks().map((item) => item.id)).toEqual(['t1']);
    expect(store.revision()).toBe(3);
  });

  it('変更通知の版が新しいときだけ取り直す', async () => {
    await store.refresh();
    const source = FakeEventSource.latest();
    source.open();
    source.emit({ resource: 'tasks', revision: 3 });
    await flush();
    expect(backend.count('GET', '/tasks')).toBe(1);

    backend.on('GET', '/tasks', { body: taskList([task({ title: '更新後' })], 4) });
    source.emit({ resource: 'tasks', revision: 4 });
    await flush();
    expect(backend.count('GET', '/tasks')).toBe(2);
    expect(store.tasks()[0]?.title).toBe('更新後');

    source.emit({ resource: 'wallet', revision: 9 });
    await flush();
    expect(backend.count('GET', '/tasks')).toBe(2);
  });

  it('読み込んでいなければ変更通知では取りに行かない', async () => {
    const source = FakeEventSource.latest();
    source.open();
    source.emit({ resource: 'tasks', revision: 10 });
    await flush();
    expect(backend.count('GET', '/tasks')).toBe(0);
  });

  it('つなぎ直したら取り直す', async () => {
    await store.refresh();
    FakeEventSource.latest().open();
    vi.useFakeTimers();
    try {
      FakeEventSource.latest().fail();
      vi.advanceTimersByTime(2000);
    } finally {
      vi.useRealTimers();
    }
    FakeEventSource.latest().open();
    await flush();
    expect(backend.count('GET', '/tasks')).toBe(2);
  });

  it('操作ごとにエラーを持ち、ほかの操作に影響しない', async () => {
    backend.on('POST', '/tasks/t1/start', {
      status: 409,
      body: { type: 'about:blank', status: 409, code: 'task.another_active' },
    });
    const outcome = await store.startTask('t1');
    expect(outcome.ok).toBe(false);
    expect(store.start.error()?.message).toBe(messages.ja.shared.problems['task.another_active']);
    expect(store.start.pending()).toBe(false);
    expect(store.complete.error()).toBeUndefined();
    expect(store.create.error()).toBeUndefined();
  });

  it('開始すると作業の状態を受け取り、課題を取り直す', async () => {
    backend.on('POST', '/tasks/t1/start', { body: workSession() });
    await store.startTask('t1');
    await flush();
    expect(TestBed.inject(WorkSessionStore).session()?.id).toBe('s1');
    expect(backend.count('GET', '/tasks')).toBe(1);
  });

  it('変更案を案の元にした版で確定する', async () => {
    backend.on('POST', '/reschedule/commit', { body: taskList([task()], 5) });
    await store.commitDraft(draft({ baseRevision: 3 }));
    const request = backend.requests.find((item) => item.path === '/reschedule/commit');
    expect(request?.headers.get('If-Match')).toBe('"3"');
    expect(request?.headers.get('Idempotency-Key')).toMatch(/^[A-Za-z0-9_-]{8,80}$/);
    expect(request?.body).toEqual({
      changes: [{ taskId: 't1', after: draft().changes[0]?.after }],
    });
    expect(store.revision()).toBe(5);
  });

  it('アカウントが切り替わると空に戻り、古い応答を書き込まない', async () => {
    await store.refresh();
    let release: () => void = () => undefined;
    backend.on(
      'GET',
      '/tasks',
      () =>
        new Promise((resolve) => {
          release = () => {
            resolve({ body: taskList([task({ id: 'old' })], 7) });
          };
        }),
    );
    const pending = store.refresh();
    await flush();
    TestBed.inject(AccountScope).reset();
    expect(store.value()).toBeUndefined();
    release();
    const outcome = await pending;
    expect(outcome.ok).toBe(false);
    expect(store.value()).toBeUndefined();
  });

  it('実行中の取り直しは重ねずに、終わってからもう一度だけ行う', async () => {
    const first = store.refresh();
    void store.refresh();
    void store.refresh();
    await first;
    await flush();
    expect(backend.count('GET', '/tasks')).toBe(2);
  });
});
