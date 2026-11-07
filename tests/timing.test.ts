import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  elapsedSeconds,
  plannedSeconds,
  viewingAvailable,
  workRewardText,
  type State,
  type Task,
} from '../shared/models.ts';

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

test('viewing availability honors both the carried balance and remaining daily budget', () => {
  const state = {
    balanceSeconds: 7200,
    timePolicy: { mode: 'rewards', dailyLimitSeconds: 1800, viewedSeconds: 1740 },
  } as State;
  assert.equal(viewingAvailable(state), 60);
  state.balanceSeconds = 20;
  assert.equal(viewingAvailable(state), 20);
  state.timePolicy!.viewedSeconds = 1900;
  assert.equal(viewingAvailable(state), 0);
  state.timePolicy!.mode = 'reminder';
  assert.equal(viewingAvailable(state), 600);
});

test('reward messages use the saved grant, including capped seconds and zero rewards', () => {
  const before = {
    balanceSeconds: 0,
    ledger: [],
    timePolicy: { mode: 'rewards' },
  } as unknown as State;
  const after = {
    ...before,
    balanceSeconds: 610,
    ledger: [
      { id: 'daily', kind: 'daily', seconds: 600 },
      { id: 'work:one', kind: 'work', seconds: 10 },
    ],
  } as State;
  assert.equal(workRewardText(before, after), '作業を記録しました。視聴時間 ＋10秒');
  after.ledger[1].deferred = true;
  assert.equal(
    workRewardText(before, after),
    '作業を記録しました。視聴時間 ＋10秒（朝に受け取れます）',
  );
  after.ledger[1].seconds = 0;
  assert.equal(workRewardText(before, after), '作業を記録しました');
  const session = {
    taskId: 'a',
    startedAt: '2026-11-07T00:00:00Z',
    pausedSeconds: 0,
    limitSeconds: 1500,
  };
  assert.equal(elapsedSeconds(session, Date.parse('2026-11-07T10:00:00Z')), 1500);
});
