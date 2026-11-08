// 作業サイトの content script（作業タイマーだけを出す）。
// manifest には載せず、ポップアップで利用者がそのサイトを許可したときに scripting.registerContentScripts で登録する。
import css from '../../ui/ui.css?inline';
import { createShadowRootUi } from 'wxt/utils/content-script-ui/shadow-root';
import { defineContentScript } from 'wxt/utils/define-content-script';
import { ContentContext } from '../../content/context';
import { TimerBar } from '../../ui/timer-bar';

declare global {
  interface Window {
    dopagakiWorkSite?: boolean;
  }
}

export default defineContentScript({
  registration: 'runtime',
  // CSS は文字列として Shadow root に入れる（ページから読める場所に CSS を公開しない）。
  cssInjectionMode: 'manual',
  async main(ctx) {
    // 登録と、許可した直後の executeScript の両方で読み込まれることがあるので、1回だけ動かす。
    if (window.dopagakiWorkSite) return;
    window.dopagakiWorkSite = true;
    const ui = await createShadowRootUi(ctx, {
      name: 'dopagaki-extension',
      css,
      position: 'inline',
      anchor: 'html',
      append: 'last',
      isolateEvents: true,
      onMount(container, _shadow, shadowHost) {
        shadowHost.id = 'dopagaki-extension';
        container.className = 'dpg-root';
        return container;
      },
    });
    ui.mount();
    const context = new ContentContext(ui.uiContainer);
    const timer = new TimerBar(context);
    context.onChange(() => {
      timer.render();
    });
    ctx.setInterval(() => {
      timer.tick();
    }, 1000);
    const disconnect = context.connect();
    void context.refresh(false);
    ctx.onInvalidated(() => {
      disconnect();
      window.dopagakiWorkSite = false;
    });
  },
});
