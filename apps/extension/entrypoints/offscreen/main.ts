import { z } from 'zod';
import { MANAGEMENT_ORIGIN } from '../../lib/config';
let frame: HTMLIFrameElement | undefined;
chrome.runtime.onMessage.addListener((raw: unknown, sender, respond) => {
  if (
    sender.id !== chrome.runtime.id ||
    !z.object({ target: z.literal('offscreen'), type: z.literal('AUTH_OPEN') }).safeParse(raw)
      .success
  )
    return false;
  frame?.remove();
  frame = document.createElement('iframe');
  frame.src = `${MANAGEMENT_ORIGIN}/extension-auth`;
  document.body.append(frame);
  frame.onload = () => frame?.contentWindow?.postMessage({ type: 'AUTH_START' }, MANAGEMENT_ORIGIN);
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
  if (parsed.success) void chrome.runtime.sendMessage(parsed.data);
});
