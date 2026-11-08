// 視聴の区切りを選ぶ画面。区切りを使うルールでは、長さを選ぶまで動画を再生しない。
import { clockLabel, extensionCopy } from '@dopagaki/messages';
import type { Request } from '../lib/protocol';
import { send } from '../content/messaging';
import { runAction } from './action';
import { button, el } from './dom';
import { overlayClass, type OverlayHost } from './overlay';

export interface ChoiceHandlers {
  /** 持ち主のタブで、区切りの途中で「今は見ない」を選んだ。 */
  readonly stop: (container: HTMLElement) => void;
  /** 区切りを始めた・引き継いだ・再接続できた。 */
  readonly resumed: () => void;
}

/** 視聴できる秒数（区切りの残りではなく、今日の残り）。 */
export function availableSeconds(host: OverlayHost): number {
  const state = host.context.state;
  return state?.viewing?.availableSeconds ?? state?.wallet?.availableSeconds ?? 0;
}

export function showViewingChoice(
  host: OverlayHost,
  offline: boolean,
  handlers: ChoiceHandlers,
): void {
  const state = host.context.state;
  if (!state) return;
  const session = state.viewing?.session ?? null;
  const active = session !== null && !session.endedAt;
  const available = availableSeconds(host);
  const dialog = host.open('choice', {
    label: offline ? extensionCopy.choiceOfflineLabel : extensionCopy.choiceLabel,
    className: `${overlayClass(state.settings, 'simple')} choice`,
  });
  const { panel } = dialog;
  panel.append(
    el('h2', {
      text: offline
        ? extensionCopy.choiceOffline
        : active
          ? extensionCopy.choiceActive
          : session
            ? extensionCopy.choiceEnded
            : extensionCopy.choiceNone,
    }),
    el('p', {
      text:
        state.wallet?.policy.mode === 'reminder'
          ? extensionCopy.choiceReminder
          : extensionCopy.choiceRemaining(clockLabel(available)),
    }),
  );

  const resume = (request: Request) => {
    void runAction(panel, request).then(async (reply) => {
      if (!reply) return;
      if (!reply.ok) {
        // ほかのタブが先に区切りを始めていたら、引き継ぐかどうかを聞き直す。
        if (reply.code === 'viewing.session_active' && (await host.context.refresh(true))) {
          showViewingChoice(host, false, handlers);
        }
        return;
      }
      host.context.apply(reply);
      host.close();
      handlers.resumed();
    });
  };

  const actions = el('div', { className: 'actions' });
  if (offline) {
    actions.append(
      button(
        extensionCopy.reconnect,
        () => {
          resume({ type: 'GET_STATE', refresh: true });
        },
        'filled',
      ),
    );
  } else if (active && available > 0) {
    actions.append(
      button(
        extensionCopy.continueHere(clockLabel(session.plannedSeconds - session.consumedSeconds)),
        () => {
          resume({ type: 'CLAIM_VIEWING', sessionId: session.id });
        },
        'filled',
      ),
    );
  } else if (available > 0) {
    const choices: readonly (300 | 600)[] = session ? [300] : [300, 600];
    for (const seconds of choices) {
      // 押し直しても同じ区切りになるよう、ボタンごとに区切りの ID を決めておく。
      const sessionId = crypto.randomUUID();
      const label = session
        ? extensionCopy.extend
        : extensionCopy.choose(
            seconds / 60,
            available < seconds ? clockLabel(available) : undefined,
          );
      actions.append(
        button(
          label,
          () => {
            resume(
              session
                ? { type: 'START_VIEWING', sessionId, seconds, extendFrom: session.id }
                : { type: 'START_VIEWING', sessionId, seconds },
            );
          },
          seconds === 300 ? 'filled' : 'outlined',
        ),
      );
    }
  } else {
    panel.append(el('p', { className: 'exhausted', text: extensionCopy.exhausted }));
  }
  actions.append(
    button(session && !active ? extensionCopy.endHere : extensionCopy.notNow, () => {
      if (active && host.context.ownsViewing) handlers.stop(panel);
      else host.close();
    }),
    button(
      extensionCopy.nextWork,
      () => {
        void send({ type: 'OPEN_APP' });
        host.close();
      },
      'text',
    ),
  );
  panel.append(actions);
  dialog.focusFirst();
}
