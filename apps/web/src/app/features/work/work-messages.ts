import type { WorkRecord } from '@dopagaki/contract';
import { durationLabel } from '@dopagaki/messages';

/** 作業を記録したときのお知らせ。報酬の秒数と、朝に受け取るかはサーバーが決める。 */
export function workRecordedMessage(work: WorkRecord | undefined): string {
  if (!work || work.rewardSeconds <= 0) return '作業を記録しました';
  return `作業を記録しました。視聴時間 ＋${durationLabel(work.rewardSeconds)}${work.deferred ? '（朝に受け取れます）' : ''}`;
}
