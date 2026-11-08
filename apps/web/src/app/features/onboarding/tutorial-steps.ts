import type { Wallet } from '@dopagaki/contract';
import { durationLabel } from '@dopagaki/messages';

export interface TutorialStep {
  /** 強調する要素の `data-tour`。無ければ中央に出す。 */
  readonly target?: string;
  readonly title: string;
  readonly body: readonly string[];
}

/** 使い方の案内（8段階）。報酬の数はサーバーのルールから表示する。 */
export function tutorialSteps(rules: Wallet['rules'] | undefined): readonly TutorialStep[] {
  const registration = durationLabel(rules?.registrationRewardSeconds ?? 300);
  const work = durationLabel(rules?.workSecondsPerReward ?? 300);
  const reward = durationLabel(rules?.rewardSecondsPerUnit ?? 60);
  return [
    {
      title: 'DOPAGAKIへようこそ',
      body: [
        '締切から逆算して作業の予定を作り、取り組んだぶんだけショート動画を見られる時間が増えるアプリです。',
        '覚えることは「課題を登録する → 通知が来たら、はじめる」だけ。1分で画面を案内します。',
      ],
    },
    {
      target: 'add',
      title: 'まずは課題を1つ登録',
      body: [
        '入力するのは「課題名・締切・必要時間」の3つだけ。締切はカレンダーと時刻のリストから選べます。',
        `登録するだけで視聴＋${registration}。`,
      ],
    },
    {
      target: 'next',
      title: '取りかかる時間は自動で決まります',
      body: [
        '締切から逆算した作業予定がここに出ます。',
        '時間になると通知が届くので、「はじめる」でタイマーを開始します。',
      ],
    },
    {
      target: 'credit',
      title: '今日、見られる時間',
      body: [
        `作業を記録すると、${work}ごとに視聴${reward}増えます。`,
        'YouTubeショートを見るときは、Chrome拡張がこの時間を使います。',
      ],
    },
    {
      target: 'calendar',
      title: '予定はカレンダーで確認',
      body: ['予定を選ぶと詳細が開き、内容の編集や時間の調整ができます。'],
    },
    {
      target: 'assistant',
      title: '言葉で相談することもできます',
      body: [
        '「金曜までにレポート、2時間くらい」のように書くと、予定の案を作ります。',
        '確認して確定するまで、登録はされません。',
      ],
    },
    {
      target: 'settings',
      title: '設定はあとからで大丈夫',
      body: [
        '最初はそのままで使えます。慣れてきたら「夜の区切り（就寝時刻）」と「Google連携」だけ設定すると便利です。',
        'この案内は、右上の「使い方」からいつでも見直せます。',
      ],
    },
    {
      title: '準備できました',
      body: ['まずは、今週の課題を1つ登録してみましょう。'],
    },
  ];
}

export const tutorialStepCount = 8;
