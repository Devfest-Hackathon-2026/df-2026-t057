import { describe, expect, it } from 'vitest';
import type { ExtensionTask } from './schemas';
import {
  alarmTime,
  inNight,
  isDue,
  nextTaskToNotify,
  pruneNotified,
  shouldPrefetchLine,
} from './schedule';

const at = (iso: string) => Date.parse(iso);
const task = (
  id: string,
  start: string,
  status: ExtensionTask['status'] = 'pending',
): ExtensionTask => ({
  id,
  title: id,
  why: '',
  minutes: 30,
  url: '',
  status,
  plan: {
    start,
    end: new Date(at(start) + 30 * 60_000).toISOString(),
    steps: [{ title: id, start, end: new Date(at(start) + 30 * 60_000).toISOString() }],
    reason: 'backward',
  },
});

describe('nextTaskToNotify', () => {
  const tasks = [
    task('late', '2026-11-08T12:00:00.000Z'),
    task('early', '2026-11-08T10:00:00.000Z'),
    task('done', '2026-11-08T09:00:00.000Z', 'completed'),
    task('active', '2026-11-08T09:30:00.000Z', 'active'),
  ];

  it('picks the earliest pending task', () => {
    expect(nextTaskToNotify(tasks, {})?.id).toBe('early');
  });

  it('skips a task already notified for the same start, but notifies again after a snooze', () => {
    expect(nextTaskToNotify(tasks, { early: '2026-11-08T10:00:00.000Z' })?.id).toBe('late');
    expect(nextTaskToNotify(tasks, { early: '2026-11-08T09:50:00.000Z' })?.id).toBe('early');
  });

  it('returns nothing when everything has been notified', () => {
    expect(
      nextTaskToNotify(tasks, {
        early: '2026-11-08T10:00:00.000Z',
        late: '2026-11-08T12:00:00.000Z',
      }),
    ).toBeUndefined();
  });
});

describe('alarm timing', () => {
  const t = task('a', '2026-11-08T10:00:00.000Z');
  it('rings at the plan start, or a second from now if it already passed', () => {
    expect(alarmTime(t, at('2026-11-08T09:00:00.000Z'))).toBe(at('2026-11-08T10:00:00.000Z'));
    expect(alarmTime(t, at('2026-11-08T11:00:00.000Z'))).toBe(at('2026-11-08T11:00:01.000Z'));
  });

  it('is due within a second of the start', () => {
    expect(isDue(t, at('2026-11-08T09:59:59.500Z'))).toBe(true);
    expect(isDue(t, at('2026-11-08T09:59:58.000Z'))).toBe(false);
  });

  it('prefetches the line within five minutes', () => {
    expect(shouldPrefetchLine(t, at('2026-11-08T09:55:01.000Z'))).toBe(true);
    expect(shouldPrefetchLine(t, at('2026-11-08T09:50:00.000Z'))).toBe(false);
  });
});

describe('pruneNotified', () => {
  it('forgets tasks that no longer exist', () => {
    expect(pruneNotified([task('a', '2026-11-08T10:00:00.000Z')], { a: 'x', gone: 'y' })).toEqual({
      a: 'x',
    });
  });
});

describe('inNight', () => {
  const night = { start: '2026-11-08T13:00:00.000Z', end: '2026-11-08T22:00:00.000Z' };
  it.each([
    ['2026-11-08T12:59:59.000Z', false],
    ['2026-11-08T13:00:00.000Z', true],
    ['2026-11-08T21:59:59.000Z', true],
    ['2026-11-08T22:00:00.000Z', false],
  ])('%s → %s', (now, expected) => {
    expect(inNight(night, at(now))).toBe(expected);
  });
  it('is never night without a sleep schedule', () => {
    expect(inNight(undefined, 0)).toBe(false);
  });
});
