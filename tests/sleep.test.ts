import { test } from 'node:test';
import assert from 'node:assert/strict';
import { nightWindow } from '../shared/models.ts';

// Instants are written in UTC so the result does not depend on the machine's own time zone.
const tokyo = (d: number, h: number, mi = 0) => Date.UTC(2026, 10, d, h - 9, mi);

test('the night starts an hour before bedtime and ends at wake, in the profile time zone', () => {
  const sleep = { bedtime: '23:30', wake: '07:00' };
  assert.equal(nightWindow(sleep, 'Asia/Tokyo', tokyo(7, 22, 0)), undefined);
  assert.equal(nightWindow(sleep, 'Asia/Tokyo', tokyo(7, 22, 40))?.key, '2026-11-07');
  assert.equal(nightWindow(sleep, 'Asia/Tokyo', tokyo(8, 6, 59))?.key, '2026-11-07');
  assert.equal(nightWindow(sleep, 'Asia/Tokyo', tokyo(8, 7, 0)), undefined);
  const late = { bedtime: '01:00', wake: '08:30' };
  assert.equal(nightWindow(late, 'Asia/Tokyo', tokyo(7, 23, 30)), undefined);
  assert.equal(nightWindow(late, 'Asia/Tokyo', tokyo(8, 0, 10))?.key, '2026-11-07');
  const window = nightWindow(sleep, 'Asia/Tokyo', tokyo(7, 23, 0))!;
  assert.equal(window.start, tokyo(7, 22, 30));
  assert.equal(window.end, tokyo(8, 7, 0));
});

test('another profile time zone moves the window with it', () => {
  const sleep = { bedtime: '23:30', wake: '07:00' };
  // 22:40 in New York (EST, UTC-5) is 12:40 the next day in Tokyo.
  const newYork = Date.UTC(2026, 10, 8, 3, 40);
  assert.equal(nightWindow(sleep, 'America/New_York', newYork)?.key, '2026-11-07');
  assert.equal(nightWindow(sleep, 'Asia/Tokyo', newYork), undefined);
});
