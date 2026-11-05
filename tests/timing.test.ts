import { test } from 'node:test';
import assert from 'node:assert/strict';
import { elapsedSeconds, plannedSeconds, type Task } from '../shared/models.ts';

test('elapsed time is recovered from timestamps after backgrounding or reload', () => {
  const session = { taskId: 'a', startedAt: '2026-11-06T10:00:00Z', pausedSeconds: 120 };
  assert.equal(elapsedSeconds(session, Date.parse('2026-11-06T10:12:00Z')), 600);
  assert.equal(
    elapsedSeconds(
      { ...session, pausedAt: '2026-11-06T10:12:00Z' },
      Date.parse('2026-11-06T14:00:00Z'),
    ),
    600,
  );
});

test('progress uses the confirmed compressed plan rather than the original estimate', () => {
  const task = {
    minutes: 30,
    plan: { start: '2026-11-06T10:00:00Z', end: '2026-11-06T10:20:00Z' },
  } as Task;
  assert.equal(plannedSeconds(task), 1200);
});
