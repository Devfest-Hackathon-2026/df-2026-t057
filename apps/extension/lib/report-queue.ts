// 送れていない視聴の報告の待ち行列（background が chrome.storage.local に書き出す）。
// 先に書いてから送る（write-ahead）。service worker が止まっても、次に起きたときに同じ受付の ID で送り直す。

export interface PendingReport {
  /** 受付の ID。Idempotency-Key に使う。再送でも変えない。 */
  readonly id: string;
  readonly seconds: number;
  readonly sessionId: string | undefined;
  /** 報告したアカウント。アカウントが変わったら送らずに捨てる。 */
  readonly accountId: string;
}

/** 待ち行列の上限。超えた古い報告から捨てる（数え過ぎを防ぐより、溜め込み続けないことを優先する）。 */
export const MAX_PENDING_REPORTS = 20;

/** 同じ受付の ID は一度だけ並べる。 */
export function enqueue(queue: readonly PendingReport[], report: PendingReport): PendingReport[] {
  if (queue.some((item) => item.id === report.id)) return [...queue];
  return [...queue, report].slice(-MAX_PENDING_REPORTS);
}

export function remove(queue: readonly PendingReport[], id: string): PendingReport[] {
  return queue.filter((item) => item.id !== id);
}

/** 今のアカウントの報告だけを残す。 */
export function forAccount(
  queue: readonly PendingReport[],
  accountId: string | undefined,
): PendingReport[] {
  return queue.filter((item) => item.accountId === accountId);
}

export interface FailureLike {
  /** HTTP のステータス。通信できなかったときは undefined。 */
  readonly status: number | undefined;
  readonly code: string | undefined;
}

/**
 * 送れなかった報告を、あとで送り直すか（retry）、捨てるか（drop）。
 * 通信の失敗・サーバーの失敗・利用上限・処理中は送り直す。
 * それ以外の 4xx（区切りが終わった、残りがない、入力が不正など）は、送り直しても結果が変わらないので捨てる。
 */
export function failureAction(failure: FailureLike): 'retry' | 'drop' {
  if (failure.status === undefined) return 'retry';
  if (failure.status >= 500 || failure.status === 429 || failure.status === 401) return 'retry';
  if (failure.code === 'idempotency.in_progress' || failure.code === 'auth.expired') return 'retry';
  return 'drop';
}
