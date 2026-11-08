// 夜の時間帯に Shorts を見ていたときの、一晩に一度の静かな声かけ。何も止めない。
import { send } from '../content/messaging';
import { extensionCopy } from '@dopagaki/messages';
import type { ExtensionState } from '../lib/schemas';
import { button, el } from './dom';
import { overlayClass, type OverlayHost } from './overlay';

export function showNight(host: OverlayHost, state: ExtensionState, line: string): void {
  const dialog = host.open('night', {
    label: extensionCopy.nightLabel,
    className: `${overlayClass(state.settings, 'simple')} night`,
  });
  const { panel } = dialog;
  panel.append(
    el('p', { className: 'eyebrow', text: extensionCopy.nightEyebrow }),
    el('h1', { text: extensionCopy.nightHeading }),
  );
  if (line) panel.append(el('p', { className: 'companion', text: line }));
  if (state.sleep) {
    panel.append(
      el('p', {
        className: 'muted',
        text: extensionCopy.nightSchedule(state.sleep.bedtime, state.sleep.wake),
      }),
    );
  }
  panel.append(
    el('strong', { className: 'reward', text: extensionCopy.nightReward }),
    el(
      'div',
      { className: 'actions' },
      button(
        extensionCopy.closeAndSleep,
        () => {
          void send({ type: 'CLOSE_TAB' });
        },
        'filled',
      ),
      button(
        extensionCopy.justABit,
        () => {
          host.close();
        },
        'text',
      ),
    ),
  );
  dialog.focusFirst();
}
