import type { Task } from '@dopagaki/contract';

const dayMs = 86_400_000;

/**
 * 「継続が途切れたときの演出」の濃さ（0〜0.15）。最後に課題を完了してから2日目以降、1日ごとに少し濃くする。
 * 表示だけの演出で、報酬などのルールには関わらない。
 */
export function grimeOpacity(tasks: readonly Task[], nowMs: number): number {
  let latest: number | undefined;
  for (const task of tasks) {
    if (!task.completedAt) continue;
    const completed = Date.parse(task.completedAt);
    if (latest === undefined || completed > latest) latest = completed;
  }
  if (latest === undefined) return 0;
  const idleDays = Math.floor((nowMs - latest) / dayMs) - 1;
  return Math.min(0.15, Math.max(0, idleDays) * 0.025);
}
