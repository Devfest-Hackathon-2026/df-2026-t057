import { TestBed } from '@angular/core/testing';
import { FakeBackend, flush, provideFakeBackend } from '../../../../testing/fake-backend';
import { draft, task, taskList } from '../../../../testing/fixtures';
import { TasksStore } from '../tasks.store';
import { TaskChange } from './task-change';

describe('TaskChange', () => {
  const input = {
    title: '直したレポート',
    notes: '',
    why: '',
    deadline: '2026-11-10T14:59:00Z',
    minutes: 45,
    url: '',
  };

  it('版が古ければ再確認し、新しい案の版で保存する', async () => {
    const backend = new FakeBackend()
      .on('GET', '/tasks', { body: taskList([task()], 5) })
      .on('GET', '/calendar', {
        body: { enabled: false, connected: false, sync: 'disconnected', failedJobs: 0 },
      })
      .on('POST', '/tasks/t1/edit-preview', { body: draft({ kind: 'edit', baseRevision: 5 }) })
      .on('PUT', '/tasks/t1', { body: task({ title: '直したレポート' }) });
    TestBed.configureTestingModule({ providers: provideFakeBackend(backend) });
    await TestBed.inject(TasksStore).refresh();
    const fixture = TestBed.createComponent(TaskChange);
    fixture.componentRef.setInput('taskId', 't1');
    fixture.componentRef.setInput('input', input);
    fixture.componentRef.setInput('draft', draft({ kind: 'edit', baseRevision: 4 }));
    await fixture.whenStable();
    const element = fixture.nativeElement as HTMLElement;
    const find = (text: string): HTMLButtonElement | undefined =>
      [...element.querySelectorAll('button')].find((item) => item.textContent.trim() === text);

    expect(element.textContent).toContain('予定が更新されています。');
    expect(find('この内容に変更する')).toBeUndefined();
    find('最新の予定で再確認')?.click();
    await flush();
    await fixture.whenStable();

    find('この内容に変更する')?.click();
    await flush();
    await fixture.whenStable();
    const put = backend.requests.find((item) => item.method === 'PUT');
    expect(put?.headers.get('If-Match')).toBe('"5"');
    expect(put?.body).toEqual(input);
    expect(element.textContent).toContain('課題の変更を保存しました。');
  });

  it('保存に失敗したら知らせて、案を残す', async () => {
    const backend = new FakeBackend()
      .on('GET', '/tasks', { body: taskList([task()], 4) })
      .on('GET', '/calendar', {
        body: { enabled: true, connected: true, sync: 'pending', failedJobs: 0 },
      })
      .on('PUT', '/tasks/t1', {
        status: 412,
        body: { type: 'about:blank', status: 412, code: 'precondition.failed' },
      });
    TestBed.configureTestingModule({ providers: provideFakeBackend(backend) });
    await TestBed.inject(TasksStore).refresh();
    const fixture = TestBed.createComponent(TaskChange);
    fixture.componentRef.setInput('taskId', 't1');
    fixture.componentRef.setInput('input', input);
    fixture.componentRef.setInput('draft', draft({ kind: 'edit', baseRevision: 4 }));
    await fixture.whenStable();
    const element = fixture.nativeElement as HTMLElement;
    [...element.querySelectorAll('button')]
      .find((item) => item.textContent.trim() === 'この内容に変更する')
      ?.click();
    await flush();
    await fixture.whenStable();
    const alert = element.querySelector('[role="alert"]')?.textContent ?? '';
    expect(alert).toContain('Googleへの反映を確認できていません');
    expect(alert).toContain('内容が更新されています。');
    expect(element.textContent).toContain('課題の変更案');
  });
});
