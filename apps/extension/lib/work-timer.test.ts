import { describe, expect, it } from 'vitest';
import { workElapsedSeconds } from './work-timer';

describe('workElapsedSeconds', () => {
  const session = { elapsedSeconds: 100, limitSeconds: 600, running: true };
  it.each([
    [session, 0, 100],
    [session, 30_500, 130],
    [session, -5_000, 100],
    [session, 10_000_000, 600],
    [{ ...session, running: false }, 30_000, 100],
  ])('%o after %i ms → %i', (value, elapsedMs, expected) => {
    expect(workElapsedSeconds(value, 1_000, 1_000 + elapsedMs)).toBe(expected);
  });
});
