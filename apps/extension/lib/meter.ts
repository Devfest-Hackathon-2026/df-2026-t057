// 視聴時間の計測と報告の組み立て。DOM・chrome API・時計に依存しない（時刻は引数で受け取る）。
//
// - 1秒ごとの tick で、再生が続いている間だけ秒数を積み上げる（一度止まったら次の tick から数え直す）。
// - 約30秒たまるごとに報告する。止まったときは、たまっている分をすぐに報告する。
// - 報告には受付の ID（receipt）を付け、受理されるまで同じ ID で再送する（サーバーで二重に減らさない）。

/** 報告する間隔の目安。 */
export const REPORT_EVERY_SECONDS = 30;
/** 1回の報告の上限。API は 65 秒まで受け付ける。 */
export const MAX_REPORT_SECONDS = 60;
/** タブが裏に回って tick が遅れても、1回の tick で数えるのはこの秒数まで。 */
export const MAX_TICK_SECONDS = 2;
const RETRY_BASE_MS = 10_000;
const RETRY_MAX_MS = 60_000;

export interface Receipt {
  readonly id: string;
  readonly seconds: number;
  readonly sessionId: string | undefined;
  /** 報告する直前の、区切りで消費済みの秒数（サーバーが実際に減らした秒数を求めるのに使う）。 */
  readonly consumedBefore: number | undefined;
}

export interface ReportResult {
  readonly sessionId: string | undefined;
  readonly consumedSeconds: number | undefined;
}

/**
 * サーバーが実際に数えた秒数。区切りを使うルールでは、区切りの消費秒数の増分（報告した秒数まで）。
 * 区切りが無いルールや、区切りが変わった場合は、報告した秒数をそのまま使う。
 */
export function chargedSeconds(
  receipt: Receipt,
  result: ReportResult,
  requiresSession: boolean,
): number {
  if (
    !requiresSession ||
    receipt.consumedBefore === undefined ||
    result.consumedSeconds === undefined ||
    result.sessionId !== receipt.sessionId
  ) {
    return receipt.seconds;
  }
  return Math.max(0, Math.min(receipt.seconds, result.consumedSeconds - receipt.consumedBefore));
}

export function retryDelayMs(failures: number): number {
  return Math.min(RETRY_MAX_MS, RETRY_BASE_MS * 2 ** Math.max(0, failures - 1));
}

export class ViewingMeter {
  #unreported = 0;
  #wasPlaying = false;
  #receipt: Receipt | undefined;
  #inFlight = false;
  #failures = 0;
  #retryAt = 0;

  /** まだ受理されていない視聴の秒数（小数を含む）。 */
  get unreportedSeconds(): number {
    return this.#unreported;
  }

  get receipt(): Receipt | undefined {
    return this.#receipt;
  }

  get inFlight(): boolean {
    return this.#inFlight;
  }

  get retryAt(): number {
    return this.#retryAt;
  }

  /** 前回の tick から `deltaSeconds` 秒たった。2回続けて再生中だったときだけ数える。 */
  tick(playing: boolean, deltaSeconds: number): void {
    if (playing && this.#wasPlaying) {
      this.#unreported += Math.min(MAX_TICK_SECONDS, Math.max(0, deltaSeconds));
    }
    this.#wasPlaying = playing;
  }

  /** 拡張が動画を止めたときなど、次の tick を数え直しにする。 */
  interrupt(): void {
    this.#wasPlaying = false;
  }

  /** 区切りの残りなど `limitSeconds` から、未報告の分を引いた残り。 */
  remaining(limitSeconds: number): number {
    return limitSeconds - Math.floor(this.#unreported);
  }

  /**
   * 今、報告を送るべきか。受付中の報告があれば必ず再送する。
   * `flush` は、止めた・区切りを終えるなどで、たまっている分を残さず送りたいとき。
   */
  shouldReport(now: number, playing: boolean, flush = false): boolean {
    if (this.#inFlight || now < this.#retryAt) return false;
    if (this.#receipt) return true;
    if (this.#unreported >= REPORT_EVERY_SECONDS) return true;
    return (!playing || flush) && this.#unreported >= 1;
  }

  /** 送る報告を決める。受付中の報告があれば、同じ ID・同じ秒数のものを返す。 */
  begin(
    newId: () => string,
    sessionId: string | undefined,
    consumedBefore: number | undefined,
  ): Receipt {
    this.#inFlight = true;
    this.#receipt ??= {
      id: newId(),
      seconds: Math.max(1, Math.min(MAX_REPORT_SECONDS, Math.floor(this.#unreported))),
      sessionId,
      consumedBefore,
    };
    return this.#receipt;
  }

  /**
   * 報告が受理された。数えられた分を未報告から引く。
   * 一部しか数えられなかったときは残りを次に回すが、まったく数えられなかったときは捨てる
   * （同じ秒数を何度も送り続けないため）。
   */
  succeed(charged: number): void {
    const receipt = this.#receipt;
    this.#inFlight = false;
    this.#failures = 0;
    this.#retryAt = 0;
    this.#receipt = undefined;
    if (!receipt) return;
    const subtract = charged > 0 ? charged : receipt.seconds;
    this.#unreported = Math.max(0, this.#unreported - subtract);
  }

  /** 報告が届かなかった。同じ receipt を、間隔を空けて再送する。 */
  fail(now: number): void {
    this.#inFlight = false;
    this.#failures += 1;
    this.#retryAt = now + retryDelayMs(this.#failures);
  }

  /**
   * 再試行までの待ち時間をなくす。接続が戻ったときや、利用者が操作したときに使う。
   * 再送は同じ receipt（同じ Idempotency-Key）なので、サーバーで二重に数えられることはない。
   */
  retryNow(): void {
    this.#retryAt = 0;
  }

  /** サーバーが受け付けない報告だった（区切りが終わったなど）。捨てて数え直す。 */
  discard(): void {
    this.#inFlight = false;
    this.#receipt = undefined;
    this.#failures = 0;
    this.#retryAt = 0;
    this.#unreported = 0;
  }

  /** アカウントの切り替えや、区切りが終わったとき。 */
  reset(): void {
    this.discard();
    this.#wasPlaying = false;
  }
}
