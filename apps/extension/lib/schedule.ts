// 通知の予定を決める純粋な関数。
import type { ExtensionTask } from './schemas';

/** 課題ごとに、通知済みの計画の開始時刻（`plan.start`）。スヌーズで開始時刻が変われば、また通知する。 */
export type NotifiedPlans = Readonly<Record<string, string>>;

/** まだ通知していない、いちばん早く始まる未着手の課題。 */
export function nextTaskToNotify(
  tasks: readonly ExtensionTask[],
  notified: NotifiedPlans,
): ExtensionTask | undefined {
  return tasks
    .filter((task) => task.status === 'pending' && notified[task.id] !== task.plan.start)
    .sort((a, b) => Date.parse(a.plan.start) - Date.parse(b.plan.start))[0];
}

/** 通知のアラームを鳴らす時刻。過ぎていれば1秒後。 */
export function alarmTime(task: ExtensionTask, now: number): number {
  return Math.max(now + 1000, Date.parse(task.plan.start));
}

/** 開始時刻を迎えたか（1秒の誤差を許す）。 */
export function isDue(task: ExtensionTask, now: number): boolean {
  return Date.parse(task.plan.start) <= now + 1000;
}

/** 通知の一言を先に用意しておく時間（Gemini を待たずに通知を出すため）。 */
export const PREFETCH_BEFORE_MS = 5 * 60_000;

export function shouldPrefetchLine(task: ExtensionTask, now: number): boolean {
  return Date.parse(task.plan.start) - now < PREFETCH_BEFORE_MS;
}

/** 通知済みの記録から、もう存在しない課題を取り除く（ストレージに溜め込まない）。 */
export function pruneNotified(
  tasks: readonly ExtensionTask[],
  notified: NotifiedPlans,
): Record<string, string> {
  const ids = new Set(tasks.map((task) => task.id));
  return Object.fromEntries(Object.entries(notified).filter(([id]) => ids.has(id)));
}

/** 夜の時間帯（就寝1時間前から起床まで）の中か。時間帯そのものはサーバーが決める。 */
export function inNight(night: { start: string; end: string } | undefined, now: number): boolean {
  if (!night) return false;
  return Date.parse(night.start) <= now && now < Date.parse(night.end);
}
