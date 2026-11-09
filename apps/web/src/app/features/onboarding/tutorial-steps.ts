import type { Wallet } from '@dopagaki/contract';
import type { SharedMessages, WebMessages } from '@dopagaki/messages';

export interface TutorialStep {
  /** 強調する要素の `data-tour`。無ければ中央に出す。 */
  readonly target?: string;
  readonly title: string;
  readonly body: readonly string[];
  /** Google カレンダーを連携する段階。連携するまで先へ進めない。 */
  readonly connect?: true;
  /** 拡張の配布ページへのリンクを出す段階。 */
  readonly download?: true;
}

/** 拡張の配布ページ。ZIP と導入手順がある（Chrome Web Store では配布していない）。 */
export const extensionReleaseUrl = 'https://github.com/a-sumikawa/devfest-2026/releases/latest';

/** 案内の Google カレンダーの段階の状態。required は連携が必要で、まだ連携していない。 */
export type GoogleStepState = 'required' | 'connected' | 'unavailable';

/**
 * 使い方の案内（14段階）。1段階の説明はカードの中で3行までにする（e2e で確かめる）。
 * 報酬の数はサーバーのルールから表示する。Google カレンダーの連携は最初に案内する。
 * 拡張は ZIP を手で読み込む配布なので、入手・読み込み・接続を1段階ずつ案内する。
 * `extensionVersion` は読み込まれている拡張の版（見つからなければ undefined）。
 */
export function tutorialSteps(
  rules: Wallet['rules'] | undefined,
  text: WebMessages['onboarding']['steps'],
  shared: SharedMessages,
  google: GoogleStepState,
  extensionVersion: string | undefined,
): readonly TutorialStep[] {
  const registration = shared.durationLabel(rules?.registrationRewardSeconds ?? 300);
  const work = shared.durationLabel(rules?.workSecondsPerReward ?? 300);
  const reward = shared.durationLabel(rules?.rewardSecondsPerUnit ?? 60);
  return [
    { title: text.welcome.title, body: text.welcome.body },
    { title: text.google.title, body: text.google[google], connect: true },
    {
      target: 'add',
      title: text.add.title,
      body: [text.add.fields, text.add.reward(registration)],
    },
    { target: 'next', title: text.next.title, body: text.next.body },
    { target: 'next', title: text.notify.title, body: text.notify.body },
    { target: 'credit', title: text.credit.title, body: [text.credit.reward(work, reward)] },
    { target: 'credit', title: text.extension.title, body: text.extension.body },
    {
      title: text.extensionInstall.title,
      body:
        extensionVersion === undefined
          ? text.extensionInstall.missing
          : text.extensionInstall.detected(extensionVersion),
      download: true,
    },
    { title: text.extensionLoad.title, body: text.extensionLoad.body },
    { title: text.extensionConnect.title, body: text.extensionConnect.body },
    { target: 'calendar', title: text.calendar.title, body: text.calendar.body },
    { target: 'assistant', title: text.assistant.title, body: text.assistant.body },
    { target: 'settings', title: text.settings.title, body: text.settings.body },
    { title: text.ready.title, body: text.ready.body },
  ];
}

export const tutorialStepCount = 14;

/** Google カレンダーの段階の位置。 */
export const googleStepIndex = 1;
