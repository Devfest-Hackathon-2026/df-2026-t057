import assert from 'node:assert/strict';
import test from 'node:test';
import {
  extensionCopy,
  problemMessages,
  rewardDescription,
  timeLabel,
  workRewardMessage,
} from './index.ts';
import type { RewardRuleSource } from './extension.ts';

const wallet: RewardRuleSource = {
  policy: { mode: 'rewards' },
  rules: { workSecondsPerReward: 300, rewardSecondsPerUnit: 60 },
};

test('拡張の文言は、空でない文か、文を返す関数', () => {
  for (const [key, value] of Object.entries(extensionCopy)) {
    if (typeof value === 'function') continue;
    assert.equal(typeof value, 'string', key);
    assert.ok(value.length > 0, `${key} が空`);
  }
  assert.equal(extensionCopy.continueHere('05:00'), 'このタブで続ける（残り05:00）');
  assert.equal(extensionCopy.choose(10, undefined), '10分にする');
  assert.equal(extensionCopy.choose(10, '03:00'), '10分にする（残り03:00まで）');
  assert.equal(extensionCopy.morningBonus(90), '早寝ボーナス ＋1分30秒');
});

test('視聴枠を使い切ったときの文言は、エラーコードの文言と同じ', () => {
  assert.equal(extensionCopy.exhausted, problemMessages['viewing.exhausted']);
});

test('workRewardMessage は報酬の有無と受け取り時期で文を変える', () => {
  assert.equal(workRewardMessage(0, false), '作業を記録しました');
  assert.equal(workRewardMessage(75, false), '作業を記録しました。視聴時間 ＋1分15秒');
  assert.equal(
    workRewardMessage(60, true),
    '作業を記録しました。視聴時間 ＋1分（朝に受け取れます）',
  );
});

test('rewardDescription は API のルールの値で説明する', () => {
  assert.equal(rewardDescription(wallet), '取り組んだ5分で視聴1分。記録した分だけ増えます');
});

test('rewardDescription は報酬モード以外と未取得のときに決まった文を返す', () => {
  assert.equal(
    rewardDescription({ ...wallet, policy: { mode: 'fixed' } }),
    '区切りまで、ひとつずつ取り組みましょう',
  );
  assert.equal(rewardDescription(null), '取り組んだ時間を記録しましょう');
});

test('timeLabel は HH:MM で表す', () => {
  assert.match(timeLabel('2026-01-01T09:05:00Z'), /^\d{2}:\d{2}$/);
});
