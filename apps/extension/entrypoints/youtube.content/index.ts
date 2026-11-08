// YouTube の content script（Shorts の検出・視聴時間の計測・画面表示）。
// 画面は Shadow DOM の中に作り、ページのスタイルと混ざらないようにする。
import css from '../../ui/ui.css?inline';
import { createShadowRootUi } from 'wxt/utils/content-script-ui/shadow-root';
import { defineContentScript } from 'wxt/utils/define-content-script';
import { startYouTube } from '../../content/youtube-app';
import { YOUTUBE_MATCH } from '../../buildconfig/manifest';

export default defineContentScript({
  matches: [YOUTUBE_MATCH],
  // CSS は文字列として Shadow root に入れる（ページから読める場所に CSS を公開しない）。
  cssInjectionMode: 'manual',
  async main(ctx) {
    const ui = await createShadowRootUi(ctx, {
      name: 'dopagaki-extension',
      css,
      position: 'inline',
      anchor: 'html',
      append: 'last',
      // YouTube のキーボード操作（スペースで再生など）が、入力欄への入力で動かないようにする。
      isolateEvents: true,
      onMount(container, _shadow, shadowHost) {
        shadowHost.id = 'dopagaki-extension';
        container.className = 'dpg-root';
        return container;
      },
    });
    ui.mount();
    startYouTube(ctx, ui.uiContainer);
  },
});
