import { TestBed } from '@angular/core/testing';
import type { AssistantCard } from '@dopagaki/contract';
import { ConfigStore } from '../../core/config/config.store';
import { FakeBackend, flush, provideFakeBackend } from '../../../testing/fake-backend';
import { plan, task, taskList, wallet } from '../../../testing/fixtures';
import { TasksStore } from '../tasks/tasks.store';
import { WalletStore } from '../wallet/wallet.store';
import { AssistantCardView } from './assistant-card';

const createCard: AssistantCard = {
  kind: 'create',
  id: 'card1',
  input: {
    id: 'new-task',
    title: '新しいレポート',
    notes: '',
    why: '',
    deadline: '2026-11-10T14:59:00Z',
    minutes: 60,
    url: '',
  },
  preview: plan('2026-11-09T10:00:00Z', '2026-11-09T11:00:00Z'),
  revision: 2,
};

function button(element: HTMLElement, label: string): HTMLButtonElement | undefined {
  return Array.from(element.querySelectorAll<HTMLButtonElement>('button')).find((item) =>
    item.textContent.trim().startsWith(label),
  );
}

describe('AssistantCardView（登録のカード）', () => {
  let backend: FakeBackend;

  async function render(): Promise<HTMLElement> {
    const fixture = TestBed.createComponent(AssistantCardView);
    fixture.componentRef.setInput('card', createCard);
    fixture.detectChanges();
    await flush();
    await fixture.whenStable();
    return fixture.nativeElement as HTMLElement;
  }

  beforeEach(async () => {
    backend = new FakeBackend()
      .on('GET', '/tasks', { body: taskList([task()], 2) })
      .on('GET', '/wallet', { body: wallet() });
    TestBed.configureTestingModule({ providers: provideFakeBackend(backend) });
    TestBed.inject(ConfigStore).config.set({
      mode: 'demo',
      features: { ai: true, calendar: false },
    });
    await TestBed.inject(TasksStore).refresh();
    await TestBed.inject(WalletStore).refresh();
  });

  it('カードの版を If-Match にして、カードの ID で登録する', async () => {
    backend.on('POST', '/tasks', () => {
      backend.on('GET', '/tasks', {
        body: taskList([task(), task({ id: 'new-task', title: '新しいレポート' })], 3),
      });
      return { status: 201, body: task({ id: 'new-task' }), headers: { ETag: '"3"' } };
    });
    const element = await render();
    expect(element.textContent).toContain('登録前');
    button(element, 'カレンダーに登録 ＋5分')?.click();
    await flush(10);
    const request = backend.requests.find(
      (item) => item.method === 'POST' && item.path === '/tasks',
    );
    expect(request?.headers.get('If-Match')).toBe('"2"');
    expect(request?.body).toMatchObject({ id: 'new-task', title: '新しいレポート' });
    expect(element.textContent).toContain('現在の予定');
    expect(button(element, '予定・手順を調整')).toBeDefined();
  });

  it('計画の版が変わったら、再確認してから登録する', async () => {
    backend.on('GET', '/tasks', { body: taskList([task()], 5) });
    await TestBed.inject(TasksStore).refresh();
    backend.on('POST', '/tasks/preview', {
      body: { plan: plan('2026-11-09T12:00:00Z', '2026-11-09T13:00:00Z'), revision: 5 },
    });
    const element = await render();
    expect(button(element, 'カレンダーに登録')).toBeUndefined();
    button(element, '最新の予定で再確認')?.click();
    await flush(10);
    expect(backend.count('POST', '/tasks/preview')).toBe(1);
    expect(button(element, 'カレンダーに登録 ＋5分')).toBeDefined();
  });

  it('登録に失敗したらカードにエラーを出す', async () => {
    backend.on('POST', '/tasks', {
      status: 412,
      body: { type: 'about:blank', status: 412, code: 'precondition.failed' },
    });
    const element = await render();
    button(element, 'カレンダーに登録 ＋5分')?.click();
    await flush(10);
    expect(element.querySelector('[role="alert"]')?.textContent).toContain(
      '内容が更新されています',
    );
  });
});
