import { TestBed } from '@angular/core/testing';
import { FakeBackend, flush, provideFakeBackend } from '../../../testing/fake-backend';
import { task, taskList, workSession } from '../../../testing/fixtures';
import { TasksStore } from '../tasks/tasks.store';
import { FocusBar } from './focus-bar';
import { WorkSessionStore } from './work-session.store';

describe('FocusBar', () => {
  let backend: FakeBackend;

  beforeEach(() => {
    backend = new FakeBackend();
    TestBed.configureTestingModule({ providers: provideFakeBackend(backend) });
    TestBed.inject(TasksStore).accept(taskList([task({ status: 'active' })]));
  });

  function render(): HTMLElement {
    const fixture = TestBed.createComponent(FocusBar);
    fixture.detectChanges();
    return fixture.nativeElement as HTMLElement;
  }

  function button(root: HTMLElement, name: string): HTMLButtonElement {
    const found = [...root.querySelectorAll('button')].find(
      (item) => item.textContent.trim() === name,
    );
    if (!found) throw new Error(`${name} が見つからない`);
    return found;
  }

  it('課題名・経過時間・上限を表示する', () => {
    TestBed.inject(WorkSessionStore).accept(
      workSession({ elapsedSeconds: 125, limitSeconds: 600, running: false }),
    );
    const root = render();
    const region = root.querySelector('[aria-label="作業タイマー"]');
    expect(region?.textContent).toContain('レポート');
    expect(region?.textContent).toContain('02:05');
    expect(region?.textContent).toContain('/ 10分');
    expect(button(root, '再開')).toBeTruthy();
  });

  it('一時停止に失敗したら、その操作のエラーを出す', async () => {
    backend.on('POST', '/work-session/pause', {
      status: 409,
      body: { type: 'about:blank', status: 409, code: 'work.no_session' },
    });
    TestBed.inject(WorkSessionStore).accept(workSession());
    const fixture = TestBed.createComponent(FocusBar);
    fixture.detectChanges();
    const root = fixture.nativeElement as HTMLElement;
    button(root, '一時停止').click();
    await flush();
    fixture.detectChanges();
    expect(root.querySelector('[role="alert"]')?.textContent).toContain(
      '進行中の課題がありません。',
    );
  });

  it('前の作業で失敗した操作のエラーを、新しい作業のタイマーに持ち越さない', async () => {
    const tasks = TestBed.inject(TasksStore);
    await tasks.complete.run(() => Promise.reject(new Error('完了に失敗')));
    TestBed.inject(WorkSessionStore).accept(workSession());
    const root = render();
    expect(root.querySelector('[role="alert"]')).toBeNull();
  });
});
