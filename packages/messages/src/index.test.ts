import assert from 'node:assert/strict';
import test from 'node:test';
import { ApiError, NetworkError } from '@dopagaki/contract';
import { clockLabel, detectLocale, errorMessage, messages } from './index.ts';

const ja = messages.ja.shared;
const en = messages.en.shared;

test('durationLabel は分と秒で表す', () => {
  assert.equal(ja.durationLabel(0), '0秒');
  assert.equal(ja.durationLabel(45), '45秒');
  assert.equal(ja.durationLabel(60), '1分');
  assert.equal(ja.durationLabel(-90), '1分30秒');
  assert.equal(en.durationLabel(45), '45 sec');
  assert.equal(en.durationLabel(60), '1 min');
  assert.equal(en.durationLabel(-90), '1 min 30 sec');
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
  assert.equal(errorMessage(error, ja), '必要時間は1〜720分で入力してください。');
  assert.equal(errorMessage(error, en), 'Enter a time needed of 1–720 minutes.');
});

test('通信エラーと想定外のエラーにも文言を返す', () => {
  assert.match(errorMessage(new NetworkError(new TypeError('x')), ja), /接続できませんでした/);
  assert.match(errorMessage(new Error('x'), ja), /問題が発生しました/);
  assert.match(errorMessage(new Error('x'), en), /Something went wrong/);
});

test('予定の検証エラーも内訳の文言を使う', () => {
  const error = new ApiError({
    type: 'about:blank',
    status: 400,
    code: 'plan.invalid_changes',
    errors: [{ field: 'changes/0/after', code: 'plan.overlaps_other' }],
  });
  assert.equal(errorMessage(error, ja), '別の課題の予定と重なっています。');
});

test('detectLocale は対応している最初の言語を選び、なければ英語にする', () => {
  assert.equal(detectLocale(['ja-JP', 'en-US']), 'ja');
  assert.equal(detectLocale(['fr-FR', 'en-GB', 'ja']), 'en');
  assert.equal(detectLocale(['EN']), 'en');
  assert.equal(detectLocale(['fr-FR', 'de']), 'en');
  assert.equal(detectLocale([]), 'en');
});

// 型は日本語に合わせているが、空文字の訳し忘れは型で見つからないので確かめる。
test('どの言語の文言も空でない', () => {
  function walk(value: unknown, path: string): void {
    if (typeof value === 'string') {
      assert.ok(value.length > 0, `${path} が空`);
    } else if (typeof value === 'object' && value !== null) {
      for (const [key, child] of Object.entries(value)) walk(child, `${path}.${key}`);
    }
  }
  for (const [locale, set] of Object.entries(messages)) walk(set, locale);
});
