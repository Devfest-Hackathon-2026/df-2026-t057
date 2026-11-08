import type { WorkRecord } from '@dopagaki/contract';
import type { SharedMessages, WebMessages } from '@dopagaki/messages';

/** 作業を記録したときのお知らせ。報酬の秒数と、朝に受け取るかはサーバーが決める。 */
export function workRecordedMessage(
  work: WorkRecord | undefined,
  shared: SharedMessages,
  text: WebMessages['work'],
): string {
  if (!work || work.rewardSeconds <= 0) return text.recorded.plain;
  return text.recorded.withReward(shared.durationLabel(work.rewardSeconds), work.deferred);
}
