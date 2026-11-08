// 画面の下に短く出す知らせ（4.5秒で消える）。読み上げのため role="status" を付ける。
import { el } from './dom';

const TOAST_MS = 4500;

export function showToast(root: HTMLElement, text: string): void {
  if (!text) return;
  const toast = el('div', { className: 'toast', text, attrs: { role: 'status' } });
  root.append(toast);
  setTimeout(() => {
    toast.remove();
  }, TOAST_MS);
}
