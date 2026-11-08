import { task } from '../../../testing/fixtures';
import { grimeOpacity } from './grime';

describe('grimeOpacity', () => {
  const day = 86_400_000;
  const completedAt = '2026-11-01T00:00:00Z';
  const base = Date.parse(completedAt);

  it.each([
    [0, 0],
    [1.5 * day, 0],
    [2 * day, 0.025],
    [4 * day, 0.075],
    [30 * day, 0.15],
  ])('完了から %d ms 後は %d', (elapsed, expected) => {
    expect(grimeOpacity([task({ status: 'completed', completedAt })], base + elapsed)).toBeCloseTo(
      expected,
    );
  });

  it('完了した課題がなければ汚さない', () => {
    expect(grimeOpacity([task()], base + 10 * day)).toBe(0);
  });
});
