import { TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { FakeBackend, provideFakeBackend } from '../../../../testing/fake-backend';
import { plan, task, taskList } from '../../../../testing/fixtures';
import { TasksStore } from '../tasks.store';
import { TasksPage, filterTasks } from './tasks-page';

describe('filterTasks', () => {
  const items = [
    task({ id: 'late', deadline: '2026-11-12T00:00:00Z' }),
    task({ id: 'done', status: 'completed', deadline: '2026-11-09T00:00:00Z' }),
    task({ id: 'active', status: 'active', deadline: '2026-11-10T00:00:00Z' }),
    task({ id: 'early', deadline: '2026-11-09T00:00:00Z' }),
  ];

  it('未完了は作業中を含め、締切の早い順に並べる', () => {
    expect(filterTasks(items, 'pending').map((item) => item.id)).toEqual([
      'early',
      'active',
      'late',
    ]);
  });

  it('完了だけを出す', () => {
    expect(filterTasks(items, 'completed').map((item) => item.id)).toEqual(['done']);
  });
});

describe('TasksPage', () => {
  it('予定と見積もりが違えば両方を出す', async () => {
    const backend = new FakeBackend().on('GET', '/tasks', {
      body: taskList([
        task({ minutes: 30, plan: plan('2026-11-08T10:00:00Z', '2026-11-08T10:10:00Z') }),
      ]),
    });
    TestBed.configureTestingModule({
      providers: [...provideFakeBackend(backend), provideRouter([])],
    });
    await TestBed.inject(TasksStore).refresh();
    const fixture = TestBed.createComponent(TasksPage);
    await fixture.whenStable();
    const text = (fixture.nativeElement as HTMLElement).textContent;
    expect(text).toContain('予定 10分');
    expect(text).toContain('（見積もり 30分）');
  });

  it('課題がなければ空の表示を出す', async () => {
    const backend = new FakeBackend().on('GET', '/tasks', { body: taskList([]) });
    TestBed.configureTestingModule({
      providers: [...provideFakeBackend(backend), provideRouter([])],
    });
    await TestBed.inject(TasksStore).refresh();
    const fixture = TestBed.createComponent(TasksPage);
    await fixture.whenStable();
    expect((fixture.nativeElement as HTMLElement).textContent).toContain('まだ課題がありません');
  });

  it('ほかの画面で失敗した「はじめる」のエラーは出さない', async () => {
    const backend = new FakeBackend().on('GET', '/tasks', { body: taskList([task()]) });
    TestBed.configureTestingModule({
      providers: [...provideFakeBackend(backend), provideRouter([])],
    });
    const store = TestBed.inject(TasksStore);
    await store.refresh();
    await store.start.run(() => Promise.reject(new Error('カレンダーで失敗')));
    expect(store.start.error()).toBeDefined();
    const fixture = TestBed.createComponent(TasksPage);
    await fixture.whenStable();
    expect((fixture.nativeElement as HTMLElement).querySelector('.error[role="alert"]')).toBeNull();
  });

  it('アーカイブしたら、開いているアーカイブの一覧も読み直す', async () => {
    const done = task({ id: 'done', title: '終わった課題', status: 'completed' });
    let archived = false;
    const backend = new FakeBackend()
      .on('GET', '/tasks', () => ({ body: taskList(archived ? [] : [done]) }))
      .on('GET', '/tasks/archived', () => ({ body: { items: archived ? [done] : [] } }))
      .on('POST', '/tasks/done/archive', () => {
        archived = true;
        return { status: 204 };
      });
    TestBed.configureTestingModule({
      providers: [...provideFakeBackend(backend), provideRouter([])],
    });
    await TestBed.inject(TasksStore).refresh();
    const fixture = TestBed.createComponent(TasksPage);
    await fixture.whenStable();
    const root = fixture.nativeElement as HTMLElement;
    const buttons = (): HTMLButtonElement[] => [...root.querySelectorAll('button')];
    buttons()
      .find((b) => b.textContent.trim() === '一覧を表示')
      ?.click();
    await fixture.whenStable();
    expect(root.textContent).toContain('アーカイブした課題はありません。');

    buttons()
      .find((b) => b.textContent.trim() === '完了')
      ?.click();
    await fixture.whenStable();
    buttons()
      .find((b) => b.textContent.trim() === 'アーカイブ')
      ?.click();
    await fixture.whenStable();
    await fixture.whenStable();
    expect(backend.count('POST', '/tasks/done/archive')).toBe(1);
    expect(root.querySelector('app-archives')?.textContent).toContain('終わった課題');
  });
});
