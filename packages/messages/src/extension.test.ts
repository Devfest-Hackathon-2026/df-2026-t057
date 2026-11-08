import assert from 'node:assert/strict';
import test from 'node:test';
import { messages, type RewardRuleSource } from './index.ts';

const extensionCopy = messages.ja.extension;

const wallet: RewardRuleSource = {
  policy: { mode: 'rewards' },
  rules: { workSecondsPerReward: 300, rewardSecondsPerUnit: 60 },
};

test('拡張の文言は、文か、文を返す関数', () => {
  for (const [key, value] of Object.entries(extensionCopy)) {
    assert.ok(typeof value === 'string' || typeof value === 'function', key);
  }
  assert.equal(extensionCopy.continueHere('05:00'), 'このタブで続ける（残り05:00）');
  assert.equal(extensionCopy.choose(10, undefined), '10分にする');
  assert.equal(extensionCopy.choose(10, '03:00'), '10分にする（残り03:00まで）');
  assert.equal(extensionCopy.morningBonus(90), '早寝ボーナス ＋1分30秒');
});

test('視聴枠を使い切ったときの文言は、エラーコードの文言と同じ', () => {
  assert.equal(extensionCopy.exhausted, messages.ja.shared.problems['viewing.exhausted']);
  assert.equal(messages.en.extension.exhausted, messages.en.shared.problems['viewing.exhausted']);
});

test('workReward は報酬の有無と受け取り時期で文を変える', () => {
  assert.equal(extensionCopy.workReward(0, false), '作業を記録しました');
  assert.equal(extensionCopy.workReward(75, false), '作業を記録しました。視聴時間 ＋1分15秒');
  assert.equal(
    extensionCopy.workReward(60, true),
    '作業を記録しました。視聴時間 ＋1分（朝に受け取れます）',
  );
});

test('rewardDescription は API のルールの値で説明する', () => {
  assert.equal(
    extensionCopy.rewardDescription(wallet),
    '取り組んだ5分で視聴1分。記録した分だけ増えます',
  );
});

test('rewardDescription は報酬モード以外と未取得のときに決まった文を返す', () => {
  assert.equal(
    extensionCopy.rewardDescription({ ...wallet, policy: { mode: 'fixed' } }),
    '区切りまで、ひとつずつ取り組みましょう',
  );
  assert.equal(extensionCopy.rewardDescription(null), '取り組んだ時間を記録しましょう');
});

test('timeLabel は HH:MM で表す', () => {
  assert.match(extensionCopy.timeLabel('2026-01-01T09:05:00Z'), /^\d{2}:\d{2}$/);
});

test('英語の文言も、API の値を使って組み立てる', () => {
  assert.equal(messages.en.extension.morningBonus(90), 'Early-to-bed bonus +1 min 30 sec');
  assert.equal(
    messages.en.extension.rewardDescription(wallet),
    '5 min of work earns 1 min of viewing. Time grows as you log it',
  );
  assert.match(messages.en.extension.timeLabel('2026-01-01T09:05:00Z'), /^\d{2}:\d{2}$/);
});
