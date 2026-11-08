// 作業タイマーのバー（YouTube と、許可した作業サイトで表示する）。
// 経過時間は1秒ごとに書き換えるだけにし、作業や課題や言語が変わったときだけ組み立て直す（フォーカスを失わせない）。
import { clockLabel } from '@dopagaki/messages';
import type { ContentContext } from '../content/context';
import { currentLocale, currentMessages } from '../lib/locale';
import { workElapsedSeconds } from '../lib/work-timer';
import { runAction } from './action';
import { button, el } from './dom';

export class TimerBar {
  #section: HTMLElement | undefined;
  #key = '';
  #elapsed: HTMLElement | undefined;
  #progress: HTMLProgressElement | undefined;
  #pause: HTMLButtonElement | undefined;

  constructor(private readonly context: ContentContext) {}

  /** 状態が変わったとき。 */
  render(): void {
    const state = this.context.state;
    const session = state?.workSession;
    const task = session ? state.tasks.find((item) => item.id === session.taskId) : undefined;
    if (!state || !session || !task) {
      this.#remove();
      return;
    }
    const key = [
      session.id,
      session.limitSeconds,
      task.title,
      !!state.wallet,
      currentLocale(),
    ].join('|');
    if (key !== this.#key || !this.#section) {
      this.#remove();
      this.#key = key;
      this.#build(task.title, session.id, task.id, !!state.wallet);
    }
    // 一時停止と再開は同じボタンの表示を変えるだけにする（押したボタンからフォーカスが消えないように）。
    const copy = currentMessages().extension;
    if (this.#pause) this.#pause.textContent = session.running ? copy.pause : copy.resume;
    this.tick();
  }

  #build(title: string, sessionId: string, taskId: string, canConfirm: boolean): void {
    const copy = currentMessages().extension;
    const section = el('section', {
      className: 'timer',
      attrs: { 'aria-label': copy.timerLabel },
    });
    const elapsed = el('span', { className: 'elapsed', attrs: { 'aria-live': 'off' } });
    const progress = el('progress', { attrs: { 'aria-label': copy.progressLabel } });
    const actions = el('div', { className: 'timer-actions' });
    const pause = button(copy.pause, () => {
      const running = this.context.state?.workSession?.running ?? true;
      void runAction(section, { type: running ? 'PAUSE_WORK' : 'RESUME_WORK' }).then((reply) => {
        if (reply?.ok) this.context.apply(reply);
      });
    });
    this.#pause = pause;
    actions.append(pause);
    if (canConfirm) {
      actions.append(
        button(copy.confirmWork, () => {
          void runAction(section, { type: 'CONFIRM_WORK', sessionId }).then((reply) => {
            if (!reply?.ok) return;
            this.context.apply(reply);
            if (reply.work)
              this.context.toast(copy.workReward(reply.work.rewardSeconds, reply.work.deferred));
          });
        }),
      );
    }
    actions.append(
      button(
        copy.complete,
        () => {
          void runAction(section, { type: 'COMPLETE_TASK', taskId }).then(async (reply) => {
            if (!reply?.ok) return;
            this.context.apply(reply);
            const reward = copy.workReward(
              reply.work?.rewardSeconds ?? 0,
              reply.work?.deferred ?? false,
            );
            const line = await this.context.line('complete', taskId);
            this.context.toast(line ? copy.completeToast(line, reward) : reward);
          });
        },
        'tonal',
      ),
    );
    section.append(
      el('strong', { className: 'timer-title', text: title }),
      elapsed,
      actions,
      progress,
    );
    this.context.root.append(section);
    this.#section = section;
    this.#elapsed = elapsed;
    this.#progress = progress;
  }

  /** 1秒ごと。 */
  tick(): void {
    const state = this.context.state;
    const session = state?.workSession;
    if (!state || !session || !this.#elapsed || !this.#progress) return;
    const elapsed = workElapsedSeconds(session, state.workSyncedAt, Date.now());
    this.#elapsed.textContent = `${clockLabel(elapsed)} / ${clockLabel(session.limitSeconds)}`;
    this.#progress.max = Math.max(1, session.limitSeconds);
    this.#progress.value = elapsed;
  }

  #remove(): void {
    this.#section?.remove();
    this.#section = undefined;
    this.#elapsed = undefined;
    this.#progress = undefined;
    this.#pause = undefined;
    this.#key = '';
  }
}
