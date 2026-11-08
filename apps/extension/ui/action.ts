// ボタンからの操作を background へ送る。送信中はボタンを止めて「処理しています…」を出し、
// 失敗したら理由を role="alert" で表示する。入力は消さない。
// 送信中にほかの操作が押されたときは何もせず undefined を返す。
import { send } from '../content/messaging';
import { copy } from '../lib/copy';
import type { Reply, Request } from '../lib/protocol';
import { el } from './dom';

let pending = false;

/** どれかの操作を送信中か（同時に2つ送らない）。 */
export function actionPending(): boolean {
  return pending;
}

export function showError(container: HTMLElement, text: string): void {
  container.querySelector(':scope > .error')?.remove();
  container.append(el('p', { className: 'error', text, attrs: { role: 'alert' } }));
}

export async function runAction(
  container: HTMLElement,
  request: Request,
): Promise<Reply | undefined> {
  if (pending) return undefined;
  pending = true;
  container.querySelector(':scope > .error')?.remove();
  const buttons = [...container.querySelectorAll('button')].filter((b) => !b.disabled);
  for (const b of buttons) b.disabled = true;
  container.setAttribute('aria-busy', 'true');
  const status = el('p', { className: 'loading', text: copy.pending, attrs: { role: 'status' } });
  container.append(status);
  try {
    const reply = await send(request);
    if (!reply.ok) showError(container, reply.error);
    return reply;
  } finally {
    pending = false;
    status.remove();
    container.removeAttribute('aria-busy');
    for (const b of buttons) b.disabled = false;
  }
}
