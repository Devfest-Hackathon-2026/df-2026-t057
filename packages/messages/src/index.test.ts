import assert from 'node:assert/strict';
import test from 'node:test';
import { ApiError, NetworkError } from '@dopagaki/contract';
import { clockLabel, durationLabel, errorMessage } from './index.ts';

test('durationLabel は分と秒で表す', () => {
  assert.equal(durationLabel(0), '0秒');
  assert.equal(durationLabel(45), '45秒');
  assert.equal(durationLabel(60), '1分');
  assert.equal(durationLabel(-90), '1分30秒');
});

test('clockLabel は MM:SS で表す', () => {
  assert.equal(clockLabel(0), '00:00');
  assert.equal(clockLabel(1500), '25:00');
  assert.equal(clockLabel(61.9), '01:01');
});

test('入力エラーは最初の項目の文言を使う', () => {
  const error = new ApiError({
    type: 'about:blank',
    status: 400,
    code: 'request.invalid',
    errors: [{ field: 'minutes', code: 'task.minutes_range' }],
  });
  assert.equal(errorMessage(error), '必要時間は1〜720分で入力してください。');
});

test('通信エラーと想定外のエラーにも文言を返す', () => {
  assert.match(errorMessage(new NetworkError(new TypeError('x'))), /接続できませんでした/);
  assert.match(errorMessage(new Error('x')), /問題が発生しました/);
});
