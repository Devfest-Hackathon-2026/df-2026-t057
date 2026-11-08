// 作業タイマーの表示に使う経過時間。サーバーの `elapsedSeconds` を、受け取った時点からの経過で数え上げる。
// サーバーとの時計のずれの影響を受けないよう、基準には受け取ったときの端末の時刻を使う。
import type { WorkSession } from '@dopagaki/contract';

export function workElapsedSeconds(
  session: Pick<WorkSession, 'elapsedSeconds' | 'limitSeconds' | 'running'>,
  syncedAt: number,
  now: number,
): number {
  const extra = session.running ? Math.max(0, (now - syncedAt) / 1000) : 0;
  return Math.max(0, Math.min(session.limitSeconds, Math.floor(session.elapsedSeconds + extra)));
}
