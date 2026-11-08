// 夜の時間帯に Shorts を見ていたときの、一晩に一度の静かな声かけ。何も止めない。
import { send } from '../content/messaging';
import { currentMessages } from '../lib/locale';
import type { ExtensionState } from '../lib/schemas';
import { button, el } from './dom';
import { overlayClass, type OverlayHost } from './overlay';

export function showNight(host: OverlayHost, state: ExtensionState, line: string): void {
  const copy = currentMessages().extension;
  const dialog = host.open('night', {
    label: copy.nightLabel,
    className: `${overlayClass(state.settings, 'simple')} night`,
  });
  const { panel } = dialog;
  panel.append(
    el('p', { className: 'eyebrow', text: copy.nightEyebrow }),
    el('h1', { text: copy.nightHeading }),
  );
  if (line) panel.append(el('p', { className: 'companion', text: line }));
  if (state.sleep) {
    panel.append(
      el('p', {
        className: 'muted',
        text: copy.nightSchedule(state.sleep.bedtime, state.sleep.wake),
      }),
    );
  }
  panel.append(
    el('strong', { className: 'reward', text: copy.nightReward }),
    el(
      'div',
      { className: 'actions' },
      button(
        copy.closeAndSleep,
        () => {
          void send({ type: 'CLOSE_TAB' });
        },
        'filled',
      ),
      button(
        copy.justABit,
        () => {
          host.close();
        },
        'text',
      ),
    ),
  );
  dialog.focusFirst();
}
