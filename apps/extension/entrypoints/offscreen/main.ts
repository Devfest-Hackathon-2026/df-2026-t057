import { z } from 'zod';
import { MANAGEMENT_ORIGIN } from '../../lib/config';
let frame: HTMLIFrameElement | undefined;
let retry: ReturnType<typeof setInterval> | undefined;
chrome.runtime.onMessage.addListener((raw: unknown, sender, respond) => {
  if (
    sender.id !== chrome.runtime.id ||
    !z.object({ target: z.literal('offscreen'), type: z.literal('AUTH_OPEN') }).safeParse(raw)
      .success
  )
    return false;
  clearInterval(retry);
  frame?.remove();
  frame = document.createElement('iframe');
  frame.src = `${MANAGEMENT_ORIGIN}/extension-auth`;
  document.body.append(frame);
  // ページの受信処理は読み込み完了より後に準備されることがあるので、結果が来るまで送り直す。
  frame.onload = () => {
    const start = () =>
      frame?.contentWindow?.postMessage({ type: 'AUTH_START' }, MANAGEMENT_ORIGIN);
    start();
    clearInterval(retry);
    retry = setInterval(start, 500);
  };
  respond({ started: true });
  return false;
});
window.addEventListener('message', (event: MessageEvent<unknown>) => {
  if (event.origin !== MANAGEMENT_ORIGIN || event.source !== frame?.contentWindow) return;
  const parsed = z
    .object({
      type: z.literal('AUTH_RESULT'),
      credential: z.object({
        idToken: z.string().optional(),
        accessToken: z.string().optional(),
        error: z.string().optional(),
      }),
    })
    .safeParse(event.data);
  if (parsed.success) {
    clearInterval(retry);
    void chrome.runtime.sendMessage(parsed.data);
  }
});
