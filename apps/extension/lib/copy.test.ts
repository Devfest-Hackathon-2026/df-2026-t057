import { describe, expect, it } from 'vitest';
import { rewardDescription, workRewardMessage } from './copy';
import type { ExtensionWallet } from './schemas';

const wallet: ExtensionWallet = {
  revision: 1,
  balanceSeconds: 600,
  availableSeconds: 600,
  deferredSeconds: 0,
  policy: { mode: 'rewards', dailyLimitSeconds: 1800, viewedSeconds: 0 },
  rules: { workSecondsPerReward: 300, rewardSecondsPerUnit: 60 },
};

describe('workRewardMessage', () => {
  it.each([
    [0, false, '作業を記録しました'],
    [75, false, '作業を記録しました。視聴時間 ＋1分15秒'],
    [60, true, '作業を記録しました。視聴時間 ＋1分（朝に受け取れます）'],
  ])('%i seconds (deferred %s)', (seconds, deferred, expected) => {
    expect(workRewardMessage(seconds, deferred)).toBe(expected);
  });
});

describe('rewardDescription', () => {
  it('describes the rule with the values from the API', () => {
    expect(rewardDescription(wallet)).toBe('取り組んだ5分で視聴1分。記録した分だけ増えます');
  });
  it('falls back for other modes and missing data', () => {
    expect(rewardDescription({ ...wallet, policy: { ...wallet.policy, mode: 'fixed' } })).toBe(
      '区切りまで、ひとつずつ取り組みましょう',
    );
    expect(rewardDescription(null)).toBe('取り組んだ時間を記録しましょう');
  });
});
