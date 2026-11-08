import { TestBed, type ComponentFixture } from '@angular/core/testing';
import { provideNativeDateAdapter } from '@angular/material/core';
import { MAT_DIALOG_DATA, MatDialogRef } from '@angular/material/dialog';
import { provideRouter } from '@angular/router';
import { FakeBackend, flush, provideFakeBackend } from '../../../../testing/fake-backend';
import { draft, task, taskList, wallet } from '../../../../testing/fixtures';
import { TasksStore } from '../tasks.store';
import { TaskDialog } from './task-dialog';
import type { TaskDialogData } from './task-dialog.model';

describe('TaskDialog', () => {
  let backend: FakeBackend;
  let close: ReturnType<typeof vi.fn>;

  function setup(data: TaskDialogData): ComponentFixture<TaskDialog> {
    close = vi.fn();
    TestBed.configureTestingModule({
      providers: [
        ...provideFakeBackend(backend),
        provideRouter([]),
        provideNativeDateAdapter(),
        { provide: MAT_DIALOG_DATA, useValue: data },
        { provide: MatDialogRef, useValue: { close } },
      ],
    });
    return TestBed.createComponent(TaskDialog);
  }

  function button(fixture: ComponentFixture<TaskDialog>, text: string): HTMLButtonElement {
    const found = [...(fixture.nativeElement as HTMLElement).querySelectorAll('button')].find(
      (item) => item.textContent.trim() === text,
    );
    if (!found) throw new Error(`ボタン「${text}」がない`);
    return found;
  }

  function typeTitle(fixture: ComponentFixture<TaskDialog>, title: string): void {
    const input = (fixture.nativeElement as HTMLElement).querySelector<HTMLInputElement>(
      'input[formcontrolname="title"]',
    );
    if (!input) throw new Error('課題名の欄がない');
    input.value = title;
    input.dispatchEvent(new Event('input'));
  }

  beforeEach(() => {
    backend = new FakeBackend()
      .on('GET', '/tasks', { body: taskList([task()], 2) })
      .on('GET', '/wallet', { body: wallet() });
  });

  it('登録すると、ダイアログで決めた ID で送って閉じる', async () => {
    backend.on('POST', '/tasks', { status: 201, body: task({ id: 'new' }) });
    const fixture = setup({ mode: 'manual' });
    await fixture.whenStable();
    typeTitle(fixture, '新しいレポート');
    await fixture.whenStable();
    button(fixture, '登録する').click();
    await flush();
    const request = backend.requests.find(
      (item) => item.method === 'POST' && item.path === '/tasks',
    );
    expect(request?.body).toMatchObject({ title: '新しいレポート', minutes: 30 });
    expect((request?.body as { id: string }).id).toMatch(/^[A-Za-z0-9_-]{8,80}$/);
    expect(close).toHaveBeenCalledWith(true);
  });

  it('失敗しても入力を残し、処理中はボタンを止める', async () => {
    let release: () => void = () => undefined;
    backend.on(
      'POST',
      '/tasks',
      () =>
        new Promise((resolve) => {
          release = () => {
            resolve({
              status: 503,
              body: { type: 'about:blank', status: 503, code: 'external.failed' },
            });
          };
        }),
    );
    const fixture = setup({ mode: 'manual' });
    await fixture.whenStable();
    typeTitle(fixture, '失敗するレポート');
    await fixture.whenStable();
    button(fixture, '登録する').click();
    await flush();
    fixture.detectChanges();
    expect(button(fixture, '処理中…').disabled).toBe(true);
    release();
    await flush();
    await fixture.whenStable();
    const element = fixture.nativeElement as HTMLElement;
    expect(element.querySelector('[role="alert"]')?.textContent).toContain(
      '外部サービスとの通信に失敗しました。',
    );
    expect(element.querySelector<HTMLInputElement>('input[formcontrolname="title"]')?.value).toBe(
      '失敗するレポート',
    );
    expect(close).not.toHaveBeenCalled();
  });

  it('編集は変更案を表示する（保存しない）', async () => {
    backend.on('POST', '/tasks/t1/edit-preview', {
      body: draft({ kind: 'edit', baseRevision: 2 }),
    });
    const fixture = setup({ taskId: 't1', mode: 'manual' });
    await TestBed.inject(TasksStore).refresh();
    await fixture.whenStable();
    expect((fixture.nativeElement as HTMLElement).textContent).toContain('課題の内容を調整');
    typeTitle(fixture, '直したレポート');
    await fixture.whenStable();
    button(fixture, '変更内容を確認').click();
    await flush();
    await fixture.whenStable();
    expect((fixture.nativeElement as HTMLElement).textContent).toContain('課題の変更案');
    expect(backend.count('PUT', '/tasks/t1')).toBe(0);
  });

  it('見つからない課題は知らせる', async () => {
    const fixture = setup({ taskId: 'missing' });
    await fixture.whenStable();
    expect((fixture.nativeElement as HTMLElement).textContent).toContain(
      'この課題は見つかりません。',
    );
  });
});
