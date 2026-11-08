import { describe, expect, it } from 'vitest';
import {
  MAX_PENDING_REPORTS,
  enqueue,
  failureAction,
  forAccount,
  remove,
  type PendingReport,
} from './report-queue';

const report = (id: string, accountId = 'a'): PendingReport => ({
  id,
  seconds: 30,
  sessionId: 's1',
  accountId,
});

describe('report queue', () => {
  it('keeps one entry per receipt id, in order', () => {
    let queue = enqueue([], report('r1'));
    queue = enqueue(queue, report('r2'));
    queue = enqueue(queue, report('r1'));
    expect(queue.map((item) => item.id)).toEqual(['r1', 'r2']);
    expect(remove(queue, 'r1').map((item) => item.id)).toEqual(['r2']);
  });

  it('drops the oldest entries beyond the limit', () => {
    let queue: PendingReport[] = [];
    for (let i = 0; i < MAX_PENDING_REPORTS + 3; i += 1) queue = enqueue(queue, report(`r${i}`));
    expect(queue).toHaveLength(MAX_PENDING_REPORTS);
    expect(queue[0]?.id).toBe('r3');
  });

  it('never sends another account’s report', () => {
    const queue = [report('r1', 'a'), report('r2', 'b')];
    expect(forAccount(queue, 'b').map((item) => item.id)).toEqual(['r2']);
    expect(forAccount(queue, undefined)).toEqual([]);
  });
});

describe('failureAction', () => {
  it.each([
    [{ status: undefined, code: undefined }, 'retry'],
    [{ status: 503, code: 'internal' }, 'retry'],
    [{ status: 429, code: 'ai.rate_limited' }, 'retry'],
    [{ status: 401, code: 'auth.expired' }, 'retry'],
    [{ status: 409, code: 'idempotency.in_progress' }, 'retry'],
    [{ status: 409, code: 'viewing.session_required' }, 'drop'],
    [{ status: 404, code: 'viewing.session_unknown' }, 'drop'],
    [{ status: 409, code: 'viewing.exhausted' }, 'drop'],
    [{ status: 400, code: 'request.invalid' }, 'drop'],
  ] as const)('%o → %s', (failure, expected) => {
    expect(failureAction(failure)).toBe(expected);
  });
});
