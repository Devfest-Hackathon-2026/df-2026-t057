// 管理画面のオリジンでだけ動く content script。ページと background の間の受け渡しだけを行う。
// ページから届くメッセージは @dopagaki/bridge の parsePageMessage で検証してから background へ渡す。
// matches はビルド時に APP_URL から決める（wxt.config.ts の hooks）。
import { parsePageMessage, type PageMessage } from '@dopagaki/bridge';
import { defineContentScript } from 'wxt/utils/define-content-script';
import { keepPort, send } from '../content/messaging';
import { MANAGEMENT_ORIGIN } from '../lib/config';

export default defineContentScript({
  // ビルド時に hooks で managementMatches(APP_URL のオリジン) に差し替える。
  matches: [`${MANAGEMENT_ORIGIN}/*`],
  runAt: 'document_start',
  main(ctx) {
    if (location.origin !== MANAGEMENT_ORIGIN) return;

    const announce = () => {
      const ready: PageMessage = {
        type: 'DOPAGAKI_EXTENSION_READY',
        version: chrome.runtime.getManifest().version,
      };
      window.postMessage(ready, MANAGEMENT_ORIGIN);
    };

    ctx.addEventListener(window, 'message', (event) => {
      if (event.source !== window || event.origin !== MANAGEMENT_ORIGIN) return;
      const message = parsePageMessage(event.data);
      if (!message) return;
      switch (message.type) {
        case 'DOPAGAKI_CONNECT':
          void send({
            type: 'CONNECT',
            origin: message.origin,
            ...(message.authorization ? { authorization: message.authorization } : {}),
            accountId: message.accountId,
          });
          return;
        case 'DOPAGAKI_DISCONNECT':
          void send({
            type: 'DISCONNECT',
            ...(message.accountId ? { accountId: message.accountId } : {}),
          });
          return;
        case 'DOPAGAKI_DIAGNOSE':
          void send({ type: 'DIAGNOSE' }).then((reply) => {
            if (reply.ok)
              window.postMessage(
                {
                  type: 'DOPAGAKI_DIAGNOSTIC',
                  requestId: message.requestId,
                  connected: reply.connected,
                  ...(reply.accountId ? { accountId: reply.accountId } : {}),
                  notifications: reply.notifications ?? 'denied',
                },
                MANAGEMENT_ORIGIN,
              );
          });
          return;
        case 'DOPAGAKI_DIAGNOSTIC':
          return;
        case 'DOPAGAKI_EXTENSION_READY':
          // 自分が送ったもの。
          return;
      }
    });

    // 管理画面を開いている間は、拡張の状態も変更通知で最新に保つ。
    const disconnect = keepPort(() => undefined);
    ctx.onInvalidated(disconnect);
    if (document.readyState === 'loading') {
      ctx.addEventListener(document, 'DOMContentLoaded', announce);
    } else {
      announce();
    }
  },
});
