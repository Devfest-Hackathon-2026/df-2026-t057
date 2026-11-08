// YouTube の content script。Shorts の検出・視聴時間の計測・画面表示を組み合わせる。
import type { ContentScriptContext } from 'wxt/utils/content-script-context';
import { copy } from '../lib/copy';
import { tabCommandSchema, type TabCommand } from '../lib/protocol';
import { showNight } from '../ui/night';
import { showNotice } from '../ui/notice';
import { createOverlayHost } from '../ui/overlay';
import { TimerBar } from '../ui/timer-bar';
import { ViewingBar } from '../ui/viewing-bar';
import { ContentContext } from './context';
import { currentVideo, onShorts } from './detector';
import { ViewingController } from './viewing-controller';

export function startYouTube(ctx: ContentScriptContext, root: HTMLElement): void {
  const context = new ContentContext(root);
  const host = createOverlayHost(context, currentVideo, () => {
    context.toast(copy.autoplayBlocked);
  });
  const timer = new TimerBar(context);
  const viewing = new ViewingController(context, host);
  const bar = new ViewingBar(root, (container) => {
    void viewing.stop(container);
  });

  context.onChange(() => {
    if (!context.state) {
      host.close();
      viewing.reset();
    }
    timer.render();
    bar.update(viewing.barModel());
  });

  let lastTick = performance.now();
  ctx.setInterval(() => {
    const now = performance.now();
    const delta = (now - lastTick) / 1000;
    lastTick = now;
    timer.tick();
    viewing.tick(delta);
    bar.update(viewing.barModel());
  }, 1000);

  const show = (command: TabCommand): boolean => {
    if (!onShorts() || document.visibilityState !== 'visible') return false;
    switch (command.type) {
      case 'SHOW_NOTICE':
        showNotice(host, command.task, command.state, command.line);
        return true;
      case 'SHOW_NIGHT':
        if (host.current) return false;
        showNight(host, command.state, command.line);
        return true;
    }
  };
  const onMessage = (
    message: unknown,
    sender: chrome.runtime.MessageSender,
    respond: (value: unknown) => void,
  ) => {
    if (sender.id !== chrome.runtime.id) return false;
    const parsed = tabCommandSchema.safeParse(message);
    if (!parsed.success) return false;
    respond({ shown: show(parsed.data) });
    return false;
  };
  chrome.runtime.onMessage.addListener(onMessage);

  const disconnect = context.connect();
  void context.refresh(false);
  ctx.onInvalidated(() => {
    chrome.runtime.onMessage.removeListener(onMessage);
    disconnect();
    host.close();
  });
}
