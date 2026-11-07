import { test } from 'node:test';
import assert from 'node:assert/strict';
import { nightWindow } from '../shared/models.ts';

test('the night starts an hour before bedtime and ends at wake', () => {
  const sleep = { bedtime: '23:30', wake: '07:00' };
  assert.equal(nightWindow(sleep, new Date(2026, 10, 7, 22, 0).getTime()), undefined);
  assert.equal(nightWindow(sleep, new Date(2026, 10, 7, 22, 40).getTime())?.key, '2026-11-07');
  assert.equal(nightWindow(sleep, new Date(2026, 10, 8, 6, 59).getTime())?.key, '2026-11-07');
  assert.equal(nightWindow(sleep, new Date(2026, 10, 8, 7, 0).getTime()), undefined);
  const late = { bedtime: '01:00', wake: '08:30' };
  assert.equal(nightWindow(late, new Date(2026, 10, 7, 23, 30).getTime()), undefined);
  assert.equal(nightWindow(late, new Date(2026, 10, 8, 0, 10).getTime())?.key, '2026-11-07');
});
