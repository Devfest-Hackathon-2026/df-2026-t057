import { TestBed } from '@angular/core/testing';
import type { AssistantTurn } from '@dopagaki/contract';
import { messages } from '@dopagaki/messages';
import { ConfigStore } from '../../core/config/config.store';
import { FakeBackend, flush, provideFakeBackend } from '../../../testing/fake-backend';
import { taskList } from '../../../testing/fixtures';
import { AssistantPanel } from './assistant-panel';
import { AssistantStore } from './assistant.store';

function readyTurn(id: string, text: string): AssistantTurn {
  return {
    id,
    text,
    status: 'ready',
    reply: '了解です。',
    cards: [],
    createdAt: '2026-11-08T10:00:00Z',
  };
}

describe('AssistantPanel', () => {
  let backend: FakeBackend;

  async function render(): Promise<HTMLElement> {
    const fixture = TestBed.createComponent(AssistantPanel);
    fixture.detectChanges();
    await flush();
    await fixture.whenStable();
    return fixture.nativeElement as HTMLElement;
  }

  function textarea(element: HTMLElement): HTMLTextAreaElement {
    const found = element.querySelector('textarea');
    if (!found) throw new Error('textarea がない');
    return found;
  }

  function type(element: HTMLElement, value: string): void {
    const input = textarea(element);
    input.value = value;
    input.dispatchEvent(new Event('input'));
  }

  function sendButton(element: HTMLElement): HTMLButtonElement {
    const button = element.querySelector<HTMLButtonElement>('button[type="submit"]');
    if (!button) throw new Error('送信ボタンがない');
    return button;
  }

  beforeEach(() => {
    backend = new FakeBackend()
      .on('GET', '/assistant/turns', { body: { turns: [] } })
      .on('GET', '/tasks', { body: taskList([]) });
    TestBed.configureTestingModule({ providers: provideFakeBackend(backend) });
    TestBed.inject(ConfigStore).config.set({
      mode: 'demo',
      features: { ai: true, calendar: false },
    });
  });

  it('候補のボタンは入力欄を埋めるだけで送らない', async () => {
    const element = await render();
    const starter = Array.from(element.querySelectorAll('button')).find(
      (button) => button.textContent.trim() === '予定を見直す',
    );
    starter?.click();
    await flush();
    expect(textarea(element).value).toBe('予定を見直す');
    expect(backend.count('POST', '/assistant/turns')).toBe(0);
  });

  it('送信すると返答を表示し、送った内容の下書きを消す', async () => {
    backend.on('POST', '/assistant/turns', (request) => ({
      body: readyTurn(request.headers.get('Idempotency-Key') ?? 'x', 'レポートの相談'),
    }));
    const element = await render();
    type(element, 'レポートの相談');
    await flush();
    sendButton(element).click();
    await flush();
    expect(element.textContent).toContain('了解です。');
    expect(TestBed.inject(AssistantStore).draft('workspace')).toBe('');
  });

  it('失敗したら下書きを残し、同じ相談を同じ ID で再試行する', async () => {
    backend.on('POST', '/assistant/turns', {
      status: 429,
      body: { type: 'about:blank', status: 429, code: 'ai.rate_limited' },
    });
    const element = await render();
    type(element, '相談したい');
    await flush();
    sendButton(element).click();
    await flush(10);
    expect(element.querySelector('[role="alert"]')?.textContent).toContain(
      messages.ja.shared.problems['ai.rate_limited'],
    );
    expect(TestBed.inject(AssistantStore).draft('workspace')).toBe('相談したい');

    backend.on('POST', '/assistant/turns', (request) => ({
      body: readyTurn(request.headers.get('Idempotency-Key') ?? 'x', '相談したい'),
    }));
    const retry = Array.from(element.querySelectorAll('button')).find(
      (button) => button.textContent.trim() === '同じ相談を再試行',
    );
    retry?.click();
    await flush(10);
    const keys = backend.requests
      .filter((request) => request.path === '/assistant/turns' && request.method === 'POST')
      .map((request) => request.headers.get('Idempotency-Key'));
    expect(keys).toHaveLength(2);
    expect(keys[0]).toBe(keys[1]);
  });

  it('AI が使えない環境では送信できない', async () => {
    TestBed.inject(ConfigStore).config.set({
      mode: 'demo',
      features: { ai: false, calendar: false },
    });
    const element = await render();
    type(element, '相談');
    await flush();
    expect(sendButton(element).disabled).toBe(true);
    expect(element.textContent).toContain('相談は準備中です。課題は手入力できます。');
  });
  it('新規登録は前の会話を表示せず、同じ登録内の会話だけを送る', async () => {
    const store = TestBed.inject(AssistantStore);
    store.value.set([readyTurn('old', '前の課題')]);
    store.startDialog(true);
    expect(store.turnsFor('dialog')).toHaveLength(0);
    backend.on('POST', '/assistant/turns', (request) => ({
      body: readyTurn(request.headers.get('Idempotency-Key') ?? 'new', '新しい課題'),
    }));
    await store.sendMessage('新しい課題', undefined, 'dialog');
    const first = store.turnsFor('dialog')[0]?.turn.id;
    await store.sendMessage('30分です', undefined, 'dialog');
    const requests = backend.requests.filter((request) => request.method === 'POST');
    expect(requests[0]?.body).toMatchObject({ historyTurnIds: [] });
    expect(requests[1]?.body).toMatchObject({ historyTurnIds: [first] });
    store.startDialog(true);
    expect(store.turnsFor('dialog')).toHaveLength(0);
    expect(store.turnsFor('workspace').some(({ turn }) => turn.id === 'old')).toBe(true);
  });
  it('新規相談の再試行でも同じIDと履歴指定を使う', async () => {
    const store = TestBed.inject(AssistantStore);
    store.startDialog(true);
    backend.on('POST', '/assistant/turns', {
      status: 429,
      body: { type: 'about:blank', status: 429, code: 'ai.rate_limited' },
    });
    await store.sendMessage('新しい課題', undefined, 'dialog');
    const failed = store.turnsFor('dialog')[0]?.turn;
    if (!failed) throw new Error('失敗した相談がない');
    backend.on('POST', '/assistant/turns', (request) => ({
      body: readyTurn(request.headers.get('Idempotency-Key') ?? 'new', '新しい課題'),
    }));
    await store.retry(failed, 'dialog');
    const requests = backend.requests.filter((request) => request.method === 'POST');
    expect(requests[0]?.body).toEqual(requests[1]?.body);
    expect(requests[0]?.headers.get('Idempotency-Key')).toBe(
      requests[1]?.headers.get('Idempotency-Key'),
    );
  });
});
