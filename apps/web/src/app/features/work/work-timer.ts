import type { WorkSession } from '@dopagaki/contract';

/**
 * 作業タイマーに表示する経過秒数。
 * サーバーの `elapsedSeconds` は `asOf` の時点の値なので、実行中はそこからの経過を足し、`limitSeconds` で止める。
 * `asOfLocalMs` は `asOf` に当たるこの端末の時刻。端末の時計のずれを持ち込まないよう、応答を受け取った時刻を使う。
 */
export function displayedElapsedSeconds(
  session: Pick<WorkSession, 'elapsedSeconds' | 'limitSeconds' | 'running'>,
  asOfLocalMs: number,
  nowMs: number,
): number {
  const base = Math.max(0, session.elapsedSeconds);
  const ticking = session.running ? Math.max(0, Math.floor((nowMs - asOfLocalMs) / 1000)) : 0;
  return Math.min(Math.max(0, session.limitSeconds), base + ticking);
}

/** 進捗バーの値（0〜100）。 */
export function workProgress(elapsedSeconds: number, limitSeconds: number): number {
  if (limitSeconds <= 0) return 100;
  return Math.min(100, (elapsedSeconds / limitSeconds) * 100);
}
