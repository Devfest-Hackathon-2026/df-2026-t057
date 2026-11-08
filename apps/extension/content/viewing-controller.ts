// 視聴時間の計測（content script 側）。数え方は lib/meter、検出は content/detector、表示は ui/* に任せる。
//
// - Shorts を開いていて、見えていて、フォーカスがあり、動画が再生中で、確認の画面を出していない間だけ数える。
// - 約30秒ごとに background へ報告する。届かなければ同じ受付の ID で送り直す。
// - 区切りの残りか今日の残りを使い切ったら止めて、区切りを `limit` で終える。
import { chargedSeconds, ViewingMeter } from '../lib/meter';
import { copy } from '../lib/copy';
import type { ExtensionState } from '../lib/schemas';
import { actionPending, runAction, showError } from '../ui/action';
import type { OverlayHost } from '../ui/overlay';
import { availableSeconds, showViewingChoice } from '../ui/viewing-choice';
import type { ContentContext } from './context';
import { currentVideo, measurable, onShorts } from './detector';
import { send } from './messaging';

const sleep = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));

export class ViewingController {
  readonly #meter = new ViewingMeter();
  #stopping: string | undefined;
  #ending = false;
  #balanceToastShown = false;

  constructor(
    private readonly context: ContentContext,
    private readonly host: OverlayHost,
  ) {}

  /** 区切りの持ち主として、残り時間のバーを出すか。 */
  barModel() {
    const state = this.context.state;
    const viewing = state?.viewing;
    const session = viewing?.session;
    const visible =
      onShorts() &&
      !!viewing?.requiresSession &&
      !!session &&
      !session.endedAt &&
      this.context.ownsViewing;
    return {
      visible,
      remainingSeconds: session
        ? session.plannedSeconds - session.consumedSeconds - this.#meter.unreportedSeconds
        : 0,
      disabled: this.#stopping !== undefined || actionPending(),
    };
  }

  /** ログアウト・アカウントの切り替え。 */
  reset(): void {
    this.#meter.reset();
    this.#stopping = undefined;
    this.#ending = false;
  }

  #pause(): void {
    currentVideo()?.pause();
    this.#meter.interrupt();
  }

  #needsChoice(state: ExtensionState): boolean {
    const viewing = state.viewing;
    if (!viewing?.requiresSession) return false;
    const session = viewing.session;
    return (
      !session || !!session.endedAt || !this.context.ownsViewing || availableSeconds(this.host) <= 0
    );
  }

  #limit(state: ExtensionState): number {
    const session = state.viewing?.session;
    const sessionLeft = session ? session.plannedSeconds - session.consumedSeconds : Infinity;
    return Math.min(availableSeconds(this.host), sessionLeft);
  }

  showChoice(offline: boolean): void {
    showViewingChoice(this.host, offline, {
      stop: (container) => {
        void this.stop(container);
      },
      resumed: () => {
        this.#meter.interrupt();
        this.host.resumePlayback();
      },
    });
  }

  /** 1秒ごとに呼ぶ。 */
  tick(deltaSeconds: number): void {
    const state = this.context.state;
    const video = currentVideo();
    let playing = measurable(video, this.host.current !== undefined);
    if (playing && (this.#stopping || this.#ending)) {
      this.#pause();
      playing = false;
    }
    if (playing && state && this.#needsChoice(state)) {
      this.#pause();
      playing = false;
      this.showChoice(false);
    }
    this.#meter.tick(playing, deltaSeconds);
    if (
      playing &&
      state?.viewing?.requiresSession &&
      this.#meter.remaining(this.#limit(state)) <= 0
    ) {
      this.#pause();
      playing = false;
      void this.#exhausted();
    }
    if (!this.#stopping && !this.#ending && this.#meter.shouldReport(Date.now(), playing)) {
      void this.report();
    }
  }

  /** 報告を1回送る。送り直しが必要な失敗なら false。 */
  async report(): Promise<boolean> {
    const session = this.context.state?.viewing?.session ?? undefined;
    const receipt = this.#meter.begin(
      () => crypto.randomUUID(),
      session && !session.endedAt ? session.id : undefined,
      session?.consumedSeconds,
    );
    const reply = await send(
      receipt.sessionId
        ? {
            type: 'REPORT_VIEWING',
            receiptId: receipt.id,
            seconds: receipt.seconds,
            sessionId: receipt.sessionId,
          }
        : { type: 'REPORT_VIEWING', receiptId: receipt.id, seconds: receipt.seconds },
    );
    if (reply.ok) {
      this.context.apply(reply);
      const viewing = reply.state?.viewing;
      const after = viewing?.session ?? undefined;
      this.#meter.succeed(
        chargedSeconds(
          receipt,
          { sessionId: after?.id, consumedSeconds: after?.consumedSeconds },
          !!viewing?.requiresSession,
        ),
      );
      if (viewing?.requiresSession && after?.endedAt && !this.#stopping && !this.#ending) {
        this.#meter.reset();
        if (!this.host.current) this.showChoice(false);
      }
      return true;
    }
    if (reply.retryable) {
      this.#meter.fail(Date.now());
      this.#pause();
      if (!this.#stopping && !this.host.current) this.showChoice(true);
      return false;
    }
    // 区切りが終わっていた・持ち主が変わったなど。送り直さずに数え直し、状態を取り直す。
    this.#meter.discard();
    void this.context.refresh(true);
    return true;
  }

  /** たまっている分を残さず報告する。届かなければ false。 */
  async #flush(): Promise<boolean> {
    for (let attempts = 0; attempts < 10; attempts += 1) {
      if (!this.#meter.receipt && this.#meter.unreportedSeconds < 1) return true;
      if (this.#meter.inFlight) {
        await sleep(200);
        continue;
      }
      if (Date.now() < this.#meter.retryAt) return false;
      if (!(await this.report())) return false;
    }
    return !this.#meter.receipt && this.#meter.unreportedSeconds < 1;
  }

  /** 区切りの残りか今日の残りを使い切った。 */
  async #exhausted(): Promise<void> {
    if (this.#ending) return;
    this.#ending = true;
    try {
      await this.#flush();
      const session = this.context.state?.viewing?.session;
      if (session && !session.endedAt) {
        const reply = await send({ type: 'END_VIEWING', sessionId: session.id, reason: 'limit' });
        if (reply.ok) this.context.apply(reply);
      }
      this.#meter.reset();
      if (availableSeconds(this.host) <= 0 && !this.#balanceToastShown) {
        this.#balanceToastShown = true;
        this.context.toast(await this.context.line('balance_zero'));
      }
    } finally {
      this.#ending = false;
    }
    if (!this.host.current) this.showChoice(false);
  }

  /** 利用者が「ここで終える」を選んだ。残りを報告してから区切りを `stop` で終える。 */
  async stop(container: HTMLElement): Promise<void> {
    const session = this.context.state?.viewing?.session;
    if (this.#stopping || !session || session.endedAt || actionPending()) return;
    this.#stopping = session.id;
    this.#pause();
    try {
      if (!(await this.#flush())) {
        showError(container, copy.transportFailed);
        return;
      }
      const reply = await runAction(container, {
        type: 'END_VIEWING',
        sessionId: session.id,
        reason: 'stop',
      });
      if (!reply?.ok) return;
      this.context.apply(reply);
      this.#meter.reset();
      this.host.close();
      this.context.toast(copy.stopped);
    } finally {
      this.#stopping = undefined;
    }
  }
}
