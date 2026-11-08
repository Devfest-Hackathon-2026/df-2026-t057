import { TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { FakeBackend, flush, provideFakeBackend } from '../../../../testing/fake-backend';
import { task, taskList, wallet, workSession } from '../../../../testing/fixtures';
import { TasksStore } from '../../tasks/tasks.store';
import { WalletStore } from '../../wallet/wallet.store';
import { WorkSessionStore } from '../../work/work-session.store';
import { CalendarWorkspace, validDate } from './calendar-workspace';

describe('CalendarWorkspace', () => {
  let backend: FakeBackend;

  async function render(): Promise<HTMLElement> {
    const fixture = TestBed.createComponent(CalendarWorkspace);
    fixture.detectChanges();
    await flush();
    await fixture.whenStable();
    return fixture.nativeElement as HTMLElement;
  }

  function startButton(element: HTMLElement): HTMLButtonElement {
    const found = element.querySelector<HTMLButtonElement>(
      'button[aria-label="次の課題をはじめる"]',
    );
    if (!found) throw new Error('開始ボタンがない');
    return found;
  }

  beforeEach(async () => {
    backend = new FakeBackend()
      .on('GET', '/tasks', {
        body: taskList([
          task({
            id: 'later',
            title: 'あとの課題',
            plan: { ...task().plan, start: '2026-11-09T10:00:00Z', end: '2026-11-09T10:30:00Z' },
          }),
          task({ id: 'first', title: '最初の課題' }),
        ]),
      })
      .on('GET', '/wallet', { body: wallet({ availableSeconds: 90 }) })
      .on('GET', '/assistant/turns', { body: { turns: [] } })
      .on('GET', '/calendar', {
        body: { enabled: false, connected: false, sync: 'disconnected', failedJobs: 0 },
      });
    TestBed.configureTestingModule({
      providers: [...provideFakeBackend(backend), provideRouter([])],
    });
    await TestBed.inject(TasksStore).refresh();
    await TestBed.inject(WalletStore).refresh();
  });

  it('視聴できる時間と、予定の開始が一番早い課題を出す', async () => {
    const element = await render();
    expect(element.querySelector('[data-tour="credit"]')?.textContent).toContain('1分30秒');
    expect(element.querySelector('.next-task')?.textContent).toContain('最初の課題');
  });

  it('制限なしのルールでは「制限なし」と出す', async () => {
    backend.on('GET', '/wallet', {
      body: wallet({ policy: { ...wallet().policy, mode: 'self' } }),
    });
    await TestBed.inject(WalletStore).refresh();
    const element = await render();
    const credit = element.querySelector('[data-tour="credit"]')?.textContent ?? '';
    expect(credit).toContain('制限なし');
    expect(credit).not.toContain('今日、視聴できる時間');
  });

  it('作業中は次の課題を開始できない', async () => {
    const element = await render();
    expect(startButton(element).disabled).toBe(false);
    TestBed.inject(WorkSessionStore).accept(workSession({ taskId: 'later' }));
    await flush();
    TestBed.tick();
    expect(startButton(element).disabled).toBe(true);
  });

  it('課題がなければ空の表示を出す', async () => {
    backend.on('GET', '/tasks', { body: taskList([]) });
    await TestBed.inject(TasksStore).refresh();
    const element = await render();
    expect(element.textContent).toContain('予定はまだありません');
    expect(element.textContent).toContain('次の課題はまだありません');
    expect(element.textContent).toContain('サンプル3件で試す');
  });
});

describe('validDate', () => {
  it('YYYY-MM-DD の正しい日付だけ受け付ける', () => {
    expect(validDate('2026-11-08')).toBe('2026-11-08');
    expect(validDate('2026-02-30')).toBeUndefined();
    expect(validDate('2026-1-8')).toBeUndefined();
    expect(validDate(undefined)).toBeUndefined();
  });
});
