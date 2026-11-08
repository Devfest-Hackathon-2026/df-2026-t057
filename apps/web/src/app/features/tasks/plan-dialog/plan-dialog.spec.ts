import { TestBed, type ComponentFixture } from '@angular/core/testing';
import { MAT_DIALOG_DATA, MatDialogRef } from '@angular/material/dialog';
import type { Draft } from '@dopagaki/contract';
import { FakeBackend, flush, provideFakeBackend } from '../../../../testing/fake-backend';
import { draft, plan, taskList } from '../../../../testing/fixtures';
import { changeIndexOf, editStep, moveChangeUp, retimeChange } from './draft-editing';
import { PlanDialog } from './plan-dialog';

function twoChanges(): Draft {
  return draft({
    changes: [
      {
        taskId: 'a',
        title: 'A',
        before: plan('2026-11-08T10:00:00Z', '2026-11-08T10:30:00Z'),
        after: {
          start: '2026-11-08T10:00:00Z',
          end: '2026-11-08T10:30:00Z',
          reason: 'snooze_kept',
          steps: [
            { title: '読む', start: '2026-11-08T10:00:00Z', end: '2026-11-08T10:10:00Z' },
            { title: '書く', start: '2026-11-08T10:10:00Z', end: '2026-11-08T10:30:00Z' },
          ],
        },
        originalMinutes: 30,
        plannedSeconds: 1800,
      },
      {
        taskId: 'b',
        title: 'B',
        before: plan('2026-11-08T10:30:00Z', '2026-11-08T11:30:00Z'),
        after: plan('2026-11-08T10:30:00Z', '2026-11-08T11:30:00Z'),
        originalMinutes: 60,
        plannedSeconds: 3600,
      },
    ],
  });
}

describe('draft-editing', () => {
  it('終了を変えると手順を比率どおりに並べ直し、元の案は変えない', () => {
    const original = twoChanges();
    const change = original.changes[0];
    if (!change) throw new Error();
    const edited = retimeChange(change, 'end', '2026-11-08T10:15:00Z');
    expect(edited).not.toBe(change);
    expect(edited.after.steps.map((step) => [step.start, step.end])).toEqual([
      ['2026-11-08T10:00:00.000Z', '2026-11-08T10:05:00.000Z'],
      ['2026-11-08T10:05:00.000Z', '2026-11-08T10:15:00.000Z'],
    ]);
    expect(edited.plannedSeconds).toBe(900);
    expect(edited.after.reason).toBe('manual');
    expect(change.after.end).toBe('2026-11-08T10:30:00Z');
  });

  it('手順の終了を変えると次の手順の開始も合わせる', () => {
    const change = twoChanges().changes[0];
    if (!change) throw new Error();
    const edited = editStep(change, 0, 'end', '2026-11-08T10:20:00Z');
    expect(edited.after.steps[1]?.start).toBe('2026-11-08T10:20:00.000Z');
    expect(edited.after.end).toBe('2026-11-08T10:30:00Z');
    expect(editStep(change, 1, 'title', '清書').after.steps[1]?.title).toBe('清書');
  });

  it('順番を上げると、最初の開始から長さを保って詰め直す', () => {
    const moved = moveChangeUp(twoChanges(), 1);
    expect(moved.changes.map((change) => change.taskId)).toEqual(['b', 'a']);
    expect(moved.changes[0]?.after.start).toBe('2026-11-08T10:00:00.000Z');
    expect(moved.changes[0]?.after.end).toBe('2026-11-08T11:00:00.000Z');
    expect(moved.changes[1]?.after.start).toBe('2026-11-08T11:00:00.000Z');
    expect(moved.changes[1]?.after.steps[1]?.end).toBe('2026-11-08T11:30:00.000Z');
  });

  it('エラーの項目名から予定の番号を取り出す', () => {
    expect(changeIndexOf('changes/1/after/steps')).toBe(1);
    expect(changeIndexOf('title')).toBeUndefined();
  });
});

describe('PlanDialog', () => {
  let backend: FakeBackend;
  let close: ReturnType<typeof vi.fn>;

  function setup(data: {
    taskId: string;
    mode: 'snooze' | 'adjust';
    draft?: Draft;
  }): ComponentFixture<PlanDialog> {
    close = vi.fn();
    TestBed.configureTestingModule({
      providers: [
        ...provideFakeBackend(backend),
        { provide: MAT_DIALOG_DATA, useValue: data },
        { provide: MatDialogRef, useValue: { close } },
      ],
    });
    return TestBed.createComponent(PlanDialog);
  }

  function click(fixture: ComponentFixture<PlanDialog>, text: string): void {
    const found = [...(fixture.nativeElement as HTMLElement).querySelectorAll('button')].find(
      (item) => item.textContent.trim() === text,
    );
    if (!found) throw new Error(`ボタン「${text}」がない`);
    found.click();
  }

  beforeEach(() => {
    backend = new FakeBackend().on('GET', '/wallet', { body: {} });
  });

  it('渡された案を表示し、案の元にした版で確定する', async () => {
    backend.on('POST', '/reschedule/commit', { body: taskList([], 8) });
    const fixture = setup({ taskId: 'a', mode: 'snooze', draft: twoChanges() });
    await fixture.whenStable();
    const element = fixture.nativeElement as HTMLElement;
    expect(element.textContent).toContain('スヌーズ後の予定を確認');
    expect(element.textContent).toContain('保存するまで、現在の予定は変わりません。');
    expect(element.querySelectorAll('.plan-change').length).toBe(2);
    click(fixture, 'この予定に変更');
    await flush();
    const request = backend.requests.find((item) => item.path === '/reschedule/commit');
    expect(request?.headers.get('If-Match')).toBe('"1"');
    expect(close).toHaveBeenCalledWith(true);
  });

  it('版が古いと確定を止め、作り直せる', async () => {
    backend
      .on('POST', '/reschedule/commit', {
        status: 412,
        body: { type: 'about:blank', status: 412, code: 'precondition.failed' },
      })
      .on('GET', '/tasks/a/reschedule', { body: draft({ taskId: 'a', baseRevision: 9 }) });
    const fixture = setup({ taskId: 'a', mode: 'adjust', draft: twoChanges() });
    await fixture.whenStable();
    click(fixture, 'この予定に変更');
    await flush();
    await fixture.whenStable();
    const element = fixture.nativeElement as HTMLElement;
    expect(element.querySelector('[role="alert"]')?.textContent).toContain(
      '内容が更新されています。',
    );
    const commit = [...element.querySelectorAll('button')].find(
      (item) => item.textContent.trim() === 'この予定に変更',
    );
    expect(commit?.disabled).toBe(true);
    click(fixture, '最新の予定で作り直す');
    await flush();
    await fixture.whenStable();
    expect(backend.requests.at(-1)?.path).toBe('/tasks/a/reschedule?mode=adjust');
    expect(commit?.disabled).toBe(false);
    expect(close).not.toHaveBeenCalled();
  });

  it('案がなければ取得する', async () => {
    backend.on('GET', '/tasks/t1/reschedule', { body: draft() });
    const fixture = setup({ taskId: 't1', mode: 'snooze' });
    await flush();
    await fixture.whenStable();
    expect(backend.requests.some((item) => item.path === '/tasks/t1/reschedule?mode=snooze')).toBe(
      true,
    );
    expect((fixture.nativeElement as HTMLElement).querySelectorAll('.plan-change').length).toBe(1);
  });
});
