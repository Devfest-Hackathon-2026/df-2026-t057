import { test } from 'node:test';
import assert from 'node:assert/strict';
import { buildActivity } from '../shared/activity.ts';
import type { LedgerEntry, Task } from '../shared/models.ts';

test('activity uses the same 84 local dates for counts and actual completion rewards', () => {
  const now = new Date(2026, 10, 7, 12);
  const at = (month: number, day: number, hour = 12) =>
    new Date(2026, month - 1, day, hour).toISOString();
  const tasks = [
    { status: 'completed', completedAt: at(8, 15, 23) },
    { status: 'completed', completedAt: at(8, 16, 0) },
    { status: 'completed', completedAt: at(11, 7, 0) },
    { status: 'completed', completedAt: at(11, 7, 23) },
    { status: 'completed', completedAt: at(11, 8, 0) },
    { status: 'pending', completedAt: at(11, 7) },
    { status: 'completed' },
  ] as Task[];
  const ledger = [
    { kind: 'completion', seconds: 1200, at: at(8, 15, 23) },
    { kind: 'completion', seconds: 900, at: at(8, 16, 0) },
    { kind: 'work', seconds: 1200, at: at(11, 7, 0) },
    { kind: 'completion', seconds: 1200, at: at(11, 7, 23) },
    { kind: 'completion', seconds: 1200, at: at(11, 8, 0) },
    { kind: 'registration', seconds: 600, at: at(11, 7) },
    { kind: 'initial', seconds: 3600, at: at(11, 7) },
    { kind: 'viewing', seconds: -600, at: at(11, 7) },
  ] as LedgerEntry[];
  const summary = buildActivity(tasks, ledger, now);
  assert.equal(summary.completed, 3);
  assert.equal(summary.earnedSeconds, 3300);
  assert.equal(summary.days.length, 84);
  assert.equal(summary.days[0].date.toDateString(), new Date(2026, 7, 16).toDateString());
  assert.deepEqual(summary.days[83], {
    date: new Date(2026, 10, 7),
    key: new Date(2026, 10, 7).toDateString(),
    completed: 2,
    earnedSeconds: 2400,
  });
  assert.deepEqual(now, new Date(2026, 10, 7, 12));
});

test('calendar dates stay consecutive across year changes, leap days and daylight saving', () => {
  for (const now of [
    new Date(2026, 0, 1),
    new Date(2028, 2, 1),
    new Date(2026, 2, 15),
    new Date(2026, 10, 5),
  ]) {
    const { days, completed, earnedSeconds } = buildActivity([], [], now);
    assert.equal(new Set(days.map((day) => day.key)).size, 84);
    assert.equal(days[83].key, now.toDateString());
    for (let i = 1; i < days.length; i++) {
      const next = new Date(days[i - 1].date);
      next.setDate(next.getDate() + 1);
      assert.equal(days[i].key, next.toDateString());
      assert.equal(days[i].date.getHours(), 0);
    }
    assert.equal(completed, 0);
    assert.equal(earnedSeconds, 0);
  }
});
