import type { LedgerEntry, Task } from './models';

export function buildActivity(tasks: Task[], ledger: LedgerEntry[], now = new Date()) {
  const start = new Date(now);
  start.setHours(0, 0, 0, 0);
  start.setDate(start.getDate() - 83);
  const days = Array.from({ length: 84 }, (_, index) => {
    const date = new Date(start);
    date.setDate(date.getDate() + index);
    return { date, key: date.toDateString(), completed: 0, earnedSeconds: 0 };
  });
  const byDate = new Map(days.map((day) => [day.key, day]));
  for (const task of tasks) {
    if (task.status !== 'completed' || !task.completedAt) continue;
    const day = byDate.get(new Date(task.completedAt).toDateString());
    if (day) day.completed++;
  }
  for (const entry of ledger) {
    if (entry.kind !== 'completion' && entry.kind !== 'work') continue;
    const day = byDate.get(new Date(entry.at).toDateString());
    if (day) day.earnedSeconds += entry.seconds;
  }
  return {
    days,
    completed: days.reduce((sum, day) => sum + day.completed, 0),
    earnedSeconds: days.reduce((sum, day) => sum + day.earnedSeconds, 0),
  };
}
