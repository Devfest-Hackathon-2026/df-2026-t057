// content script から background への送受信。受け取った値は必ず zod で検証する。
import { copy } from '../lib/copy';
import {
  PORT_NAME,
  portPushSchema,
  replySchema,
  type PortPush,
  type Reply,
  type Request,
} from '../lib/protocol';

/** 拡張の更新・無効化で、このページの content script が切り離されたか。 */
export function extensionAlive(): boolean {
  try {
    // 切り離されると chrome.runtime.id が undefined になる（型には現れない）。
    return !!chrome.runtime.id;
  } catch {
    return false;
  }
}

const transportFailure: Reply = { ok: false, error: copy.transportFailed, retryable: true };

export async function send(request: Request): Promise<Reply> {
  if (!extensionAlive()) return transportFailure;
  try {
    const reply: unknown = await chrome.runtime.sendMessage(request);
    const parsed = replySchema.safeParse(reply);
    return parsed.success ? parsed.data : transportFailure;
  } catch {
    return transportFailure;
  }
}

/** service worker の待機時間（30秒）より短い間隔で PING を送り、つながっている間は止めさせない。 */
const PING_INTERVAL_MS = 20_000;
const RECONNECT_MS = 2_000;

/**
 * background とのポートを開き続ける。切れたらつなぎ直す（service worker が再起動した場合など）。
 * 戻り値で閉じる。
 */
export function keepPort(onPush: (push: PortPush) => void): () => void {
  let port: chrome.runtime.Port | undefined;
  let ping: ReturnType<typeof setInterval> | undefined;
  let retry: ReturnType<typeof setTimeout> | undefined;
  let closed = false;

  const open = () => {
    if (closed || !extensionAlive()) return;
    try {
      port = chrome.runtime.connect({ name: PORT_NAME });
    } catch {
      return;
    }
    port.onMessage.addListener((message: unknown) => {
      const parsed = portPushSchema.safeParse(message);
      if (parsed.success) onPush(parsed.data);
    });
    port.onDisconnect.addListener(() => {
      clearInterval(ping);
      port = undefined;
      if (!closed) retry = setTimeout(open, RECONNECT_MS);
    });
    ping = setInterval(() => {
      try {
        port?.postMessage({ type: 'PING' });
      } catch {
        port?.disconnect();
      }
    }, PING_INTERVAL_MS);
  };

  // 表示されたときに切れていれば、待たずにつなぎ直す。
  const onVisibility = () => {
    if (document.visibilityState === 'visible' && !port && !closed) {
      clearTimeout(retry);
      open();
    }
  };
  document.addEventListener('visibilitychange', onVisibility);
  open();

  return () => {
    closed = true;
    clearInterval(ping);
    clearTimeout(retry);
    document.removeEventListener('visibilitychange', onVisibility);
    port?.disconnect();
  };
}
