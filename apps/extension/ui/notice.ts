// 課題の開始時刻を知らせる画面（Shorts の上に出す）。モードはパチンコ・麻雀・シンプルの3つ。
import { extensionCopy, rewardDescription } from '@dopagaki/messages';
import type { ExtensionState, ExtensionTask } from '../lib/schemas';
import { defaultSettings } from '../lib/schemas';
import { safeHttpUrl } from '../lib/urls';
import { runAction } from './action';
import { button, el } from './dom';
import { showDraft } from './draft';
import { overlayClass, type OverlayHost } from './overlay';
import { playChime } from './sound';

export function plannedMinutes(task: ExtensionTask): number {
  const seconds = Math.max(60, (Date.parse(task.plan.end) - Date.parse(task.plan.start)) / 1000);
  return Math.ceil(seconds / 60);
}

export function showNotice(
  host: OverlayHost,
  task: ExtensionTask,
  state: ExtensionState,
  line: string,
): void {
  const settings = state.settings ?? defaultSettings;
  const dialog = host.open('notice', {
    label: extensionCopy.noticeLabel,
    className: `${overlayClass(state.settings)} notice`,
  });
  const goldUrl = chrome.runtime.getURL('gold-light.png');
  dialog.element.style.setProperty('--dpg-scene-image', `url("${goldUrl}")`);
  const { panel } = dialog;
  panel.append(
    el('p', { className: 'eyebrow', text: extensionCopy.noticeEyebrow }),
    el('h1', {
      text:
        settings.mode === 'mahjong'
          ? extensionCopy.noticeHeadingMahjong
          : extensionCopy.noticeHeading,
    }),
  );
  if (line) panel.append(el('p', { className: 'companion', text: line }));
  const card = el('div', { className: 'task-card' }, el('h2', { text: task.title }));
  if (task.why) card.append(el('p', { className: 'why', text: extensionCopy.why(task.why) }));
  const first = task.plan.steps[0]?.title;
  if (first && first !== task.title) {
    card.append(el('p', { className: 'first-step', text: extensionCopy.firstStep(first) }));
  }
  card.append(
    el('p', { className: 'meta', text: extensionCopy.workMinutes(plannedMinutes(task)) }),
    el('strong', { className: 'reward', text: rewardDescription(state.wallet) }),
  );
  panel.append(card);

  const back = () => {
    showNotice(host, task, host.context.state ?? state, line);
  };
  const actions = el('div', { className: 'actions' });
  actions.append(
    button(
      extensionCopy.startWork,
      () => {
        void runAction(panel, { type: 'START_TASK', taskId: task.id }).then((reply) => {
          if (!reply?.ok) return;
          host.context.apply(reply);
          host.close();
          const url = safeHttpUrl(task.url);
          if (url) window.open(url, '_blank', 'noopener');
        });
      },
      'filled',
    ),
    button(extensionCopy.snooze(settings.snoozeMinutes), () => {
      const candidate = state.nextSnooze?.taskId === task.id ? state.nextSnooze : undefined;
      void showDraft(host, task, 'snooze', candidate, back);
    }),
    button(extensionCopy.adjust, () => {
      void showDraft(host, task, 'adjust', undefined, back);
    }),
    button(
      extensionCopy.later,
      () => {
        host.close();
      },
      'text',
    ),
  );
  panel.append(actions);
  dialog.focusFirst();
  if (settings.sound) playChime();
}
