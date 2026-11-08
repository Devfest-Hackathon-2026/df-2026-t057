import type { Wallet } from '@dopagaki/contract';
import type { SharedMessages, WebMessages } from '@dopagaki/messages';

export interface TutorialStep {
  /** 強調する要素の `data-tour`。無ければ中央に出す。 */
  readonly target?: string;
  readonly title: string;
  readonly body: readonly string[];
}

/** 使い方の案内（8段階）。報酬の数はサーバーのルールから表示する。 */
export function tutorialSteps(
  rules: Wallet['rules'] | undefined,
  text: WebMessages['onboarding']['steps'],
  shared: SharedMessages,
): readonly TutorialStep[] {
  const registration = shared.durationLabel(rules?.registrationRewardSeconds ?? 300);
  const work = shared.durationLabel(rules?.workSecondsPerReward ?? 300);
  const reward = shared.durationLabel(rules?.rewardSecondsPerUnit ?? 60);
  return [
    { title: text.welcome.title, body: text.welcome.body },
    {
      target: 'add',
      title: text.add.title,
      body: [text.add.fields, text.add.reward(registration)],
    },
    { target: 'next', title: text.next.title, body: text.next.body },
    {
      target: 'credit',
      title: text.credit.title,
      body: [text.credit.reward(work, reward), text.credit.extension],
    },
    { target: 'calendar', title: text.calendar.title, body: text.calendar.body },
    { target: 'assistant', title: text.assistant.title, body: text.assistant.body },
    { target: 'settings', title: text.settings.title, body: text.settings.body },
    { title: text.ready.title, body: text.ready.body },
  ];
}

export const tutorialStepCount = 8;
