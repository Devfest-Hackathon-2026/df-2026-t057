import { TestBed } from '@angular/core/testing';
import { MAT_DIALOG_DATA, MatDialogRef } from '@angular/material/dialog';
import type { NoticeMode } from '@dopagaki/contract';
import { FakeBackend, flush, provideFakeBackend } from '../../../testing/fake-backend';
import { plan, settings, task, taskList, wallet } from '../../../testing/fixtures';
import { SettingsStore } from '../settings/settings.store';
import { TaskActions } from '../tasks/task-actions';
import { TasksStore } from '../tasks/tasks.store';
import { WalletStore } from '../wallet/wallet.store';
import { NotificationPreview } from './notification-preview';

function button(element: HTMLElement, label: string): HTMLButtonElement {
  const found = Array.from(element.querySelectorAll<HTMLButtonElement>('button')).find(
    (item) => item.textContent.trim() === label,
  );
  if (!found) throw new Error(`ボタン ${label} がない`);
  return found;
}

describe('NotificationPreview', () => {
  let backend: FakeBackend;
  const close = vi.fn();
  const actions = { start: vi.fn(), plan: vi.fn() };

  async function render(mode: NoticeMode): Promise<{ element: HTMLElement; host: HTMLElement }> {
    backend.on('GET', '/settings', { body: settings({ mode, sound: false, snoozeMinutes: 15 }) });
    await TestBed.inject(SettingsStore).refresh();
    const fixture = TestBed.createComponent(NotificationPreview);
    fixture.detectChanges();
    await flush();
    await fixture.whenStable();
    return {
      element: fixture.nativeElement as HTMLElement,
      host: fixture.nativeElement as HTMLElement,
    };
  }

  beforeEach(async () => {
    close.mockReset();
    actions.start.mockReset();
    actions.plan.mockReset();
    const steps = plan();
    backend = new FakeBackend()
      .on('GET', '/tasks', {
        body: taskList([
          task({
            why: '単位のため',
            plan: {
              ...steps,
              steps: [{ title: '資料を読む', start: steps.start, end: steps.end }],
            },
          }),
        ]),
      })
      .on('GET', '/wallet', { body: wallet() })
      .on('POST', '/companion/lines', { body: { line: '一緒に始めよっか。', generated: false } });
    TestBed.configureTestingModule({
      providers: [
        ...provideFakeBackend(backend),
        { provide: MAT_DIALOG_DATA, useValue: { taskId: 't1' } },
        { provide: MatDialogRef, useValue: { close } },
        { provide: TaskActions, useValue: actions },
      ],
    });
    await TestBed.inject(TasksStore).refresh();
    await TestBed.inject(WalletStore).refresh();
  });

  it('パチンコでは「そろそろ、やるか。」と課題の内容、相棒の一言、報酬の説明を出す', async () => {
    const { element, host } = await render('pachinko');
    expect(host.classList).toContain('pachinko');
    expect(element.querySelector('h2')?.textContent).toContain('そろそろ、やるか。');
    expect(element.textContent).toContain('一緒に始めよっか。');
    expect(element.textContent).toContain('なぜ：単位のため');
    expect(element.textContent).toContain('最初の一手：資料を読む');
    expect(element.textContent).toContain('取り組んだ5分で視聴1分。記録した分だけ増えます');
    expect(button(element, '15分スヌーズ・予定を確認')).toBeDefined();
  });

  it('麻雀では「まず、一手。」と出す', async () => {
    const mahjong = await render('mahjong');
    expect(mahjong.host.classList).toContain('mahjong');
    expect(mahjong.element.querySelector('h2')?.textContent).toContain('まず、一手。');
  });

  it('ボタンで開始・スヌーズ・調整・閉じるを呼ぶ', async () => {
    const { element } = await render('simple');
    button(element, '作業をはじめる').click();
    expect(actions.start).toHaveBeenCalledWith(expect.objectContaining({ id: 't1' }));
    button(element, '15分スヌーズ・予定を確認').click();
    expect(actions.plan).toHaveBeenCalledWith(expect.objectContaining({ id: 't1' }), 'snooze');
    button(element, '予定を調整する').click();
    expect(actions.plan).toHaveBeenCalledWith(expect.objectContaining({ id: 't1' }), 'adjust');
    button(element, 'あとで').click();
    expect(close).toHaveBeenCalled();
  });
});
