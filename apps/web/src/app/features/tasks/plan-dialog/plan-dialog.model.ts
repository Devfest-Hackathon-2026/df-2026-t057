import type { Draft } from '@dopagaki/contract';

/**
 * PlanDialog に渡す値。
 * - `snooze`：スヌーズ後の予定を確認する（`draft` があればそれを使い、なければ取得する）
 * - `adjust`：予定を調整する
 */
export interface PlanDialogData {
  readonly taskId: string;
  readonly mode: 'snooze' | 'adjust';
  readonly draft?: Draft;
}
