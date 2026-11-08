import type { Plan, PlanChange } from '@dopagaki/contract';
import { describe, expect, it } from 'vitest';
import { fromLocalInput, moveUp, retime, toLocalInput } from './draft-edit';

const minute = 60_000;
const base = Date.parse('2026-11-08T10:00:00.000Z');
const iso = (offsetMinutes: number) => new Date(base + offsetMinutes * minute).toISOString();

function plan(start: number, minutes: number, steps = 1): Plan {
  const each = minutes / steps;
  return {
    start: iso(start),
    end: iso(start + minutes),
    steps: Array.from({ length: steps }, (_, i) => ({
      title: `step ${i + 1}`,
      start: iso(start + i * each),
      end: iso(start + (i + 1) * each),
    })),
    reason: 'snooze_kept',
  };
}

const change = (taskId: string, start: number, minutes: number): PlanChange => ({
  taskId,
  title: taskId,
  before: plan(start, minutes),
  after: plan(start, minutes, 2),
  originalMinutes: minutes,
  plannedSeconds: minutes * 60,
});

describe('moveUp', () => {
  it('swaps with the previous task and lays tasks out back to back from the first start', () => {
    const result = moveUp([change('a', 0, 30), change('b', 40, 20)], 1);
    expect(result.map((c) => c.taskId)).toEqual(['b', 'a']);
    expect(result[0]?.after.start).toBe(iso(0));
    expect(result[0]?.after.end).toBe(iso(20));
    expect(result[0]?.after.steps[1]?.end).toBe(iso(20));
    expect(result[1]?.after.start).toBe(iso(20));
    expect(result[1]?.after.end).toBe(iso(50));
  });

  it('does nothing for the first task', () => {
    const changes = [change('a', 0, 30)];
    expect(moveUp(changes, 0)).toEqual(changes);
  });
});

describe('retime', () => {
  it('scales steps to the new length and snaps the last step to the end', () => {
    const result = retime(plan(0, 30, 3), 'end', iso(60));
    expect(result?.steps.map((s) => [s.start, s.end])).toEqual([
      [iso(0), iso(20)],
      [iso(20), iso(40)],
      [iso(40), iso(60)],
    ]);
    expect(result?.reason).toBe('manual');
  });

  it('moves the start', () => {
    const result = retime(plan(0, 30), 'start', iso(10));
    expect(result?.start).toBe(iso(10));
    expect(result?.steps[0]?.start).toBe(iso(10));
    expect(result?.steps[0]?.end).toBe(iso(30));
  });

  it.each([iso(-10), 'nonsense'])('rejects an end of %s', (value) => {
    expect(retime(plan(0, 30), 'end', value)).toBeUndefined();
  });
});

describe('local input', () => {
  it('round-trips through datetime-local', () => {
    const value = '2026-11-08T10:15:00.000Z';
    expect(fromLocalInput(toLocalInput(value))).toBe(value);
    expect(fromLocalInput('')).toBeUndefined();
  });
});
