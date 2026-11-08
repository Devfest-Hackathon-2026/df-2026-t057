// Chrome拡張だけで使う、英語の文言。キーと形は ja/extension.ts に合わせる。
import type { ExtensionMessages } from '../ja/extension.ts';
import { shared } from './shared.ts';

export const extension: ExtensionMessages = {
  // 共通
  pending: 'Working on it…',
  transportFailed: "Couldn't connect. Your input is still here. Please try again.",
  accountChanged: 'Your account changed. Reconnect from the dashboard.',
  connectFromApp: 'Connect from the dashboard.',
  connectFromPopup: 'Connect from the extension popup with the same Google account.',
  untrustedSender: 'Please check where this came from.',
  operationUnknown: 'Please check the action.',
  fromShorts: 'Do this from the Shorts you are watching.',
  claimFirst: 'To continue in this tab, take over the viewing session.',
  sessionEnded: 'This viewing session has ended.',
  autoplayBlocked: "You can resume with the video's play button.",
  close: 'Close',

  // Googleアカウントでの接続
  authConfigUnavailable:
    "Couldn't load the connection settings. We'll retry when you're back online.",
  authNotConfigured: 'Google sign-in is not set up.',
  authUnavailable: "Google sign-in for the extension isn't used in this environment.",
  authInProgress: 'Checking your sign-in.',
  authTimedOut: "Couldn't confirm your sign-in. Please connect again.",
  authFailed: "Couldn't connect to your Google account.",

  // 視聴の区切り
  choiceLabel: 'Viewing session',
  choiceOfflineLabel: 'Check the viewing time connection',
  choiceOffline: 'Check your connection before resuming',
  choiceActive: 'Take over the same session?',
  choiceEnded: 'You reached the limit you set',
  choiceNone: 'How many minutes will you watch this time?',
  choiceReminder: 'Choose how long to watch before playing.',
  choiceRemaining: (clock: string) => `${clock} left today. Only time spent playing is counted.`,
  reconnect: 'Reconnect and continue',
  continueHere: (clock: string) => `Continue in this tab (${clock} left)`,
  choose: (minutes: number, limit: string | undefined) =>
    `${minutes} min${limit ? ` (up to ${limit} left)` : ''}`,
  extend: '5 more minutes from what is left',
  exhausted: shared.problems['viewing.exhausted'],
  endHere: 'Stop here',
  notNow: 'Not now',
  nextWork: 'On to the next task',
  viewingBarLabel: 'Viewing timer',
  viewingLeft: (clock: string) => `${clock} left this time`,
  stopped: 'You stopped where you planned.',

  // 通知
  noticeLabel: 'Time to start your task',
  noticeEyebrow: "IT'S YOUR TURN",
  noticeHeadingMahjong: 'First, one move.',
  noticeHeading: "Let's do this.",
  why: (why: string) => `Why: ${why}`,
  firstStep: (step: string) => `First step: ${step}`,
  workMinutes: (minutes: number) => `${minutes} min of work`,
  startWork: 'Start working',
  snooze: (minutes: number) => `Snooze ${minutes} min · check the plan`,
  adjust: 'Adjust the plan',
  later: 'Later',

  // 変更案
  draftLabel: 'Review the plan changes',
  draftAdjustHeading: 'Adjust the plan',
  draftSnoozeHeading: 'Review the plan after snoozing',
  draftNotice: 'Nothing changes until you press OK.',
  draftShortened: 'The work time is shortened to fit the deadline.',
  moveUp: '↑ Move up',
  moveUpLabel: (title: string) => `Move ${title} up one`,
  before: (start: string, end: string, minutes: number) =>
    `Before ${start}–${end} · originally ${minutes} min`,
  start: 'Start',
  end: 'End',
  invalidTime: 'The end must be after the start.',
  instructionPlaceholder: 'Instructions for Gemini (e.g. give 5 minutes to reviewing)',
  instructionLabel: 'Tell Gemini how to adjust the plan',
  regenerate: 'Ask Gemini for a new suggestion',
  commit: 'OK · Use this plan',
  cancel: 'Cancel',
  back: 'Back',

  // 夜
  nightLabel: 'Bedtime is coming up',
  nightEyebrow: 'GOOD NIGHT',
  nightHeading: 'Time to get ready for bed.',
  nightSchedule: (bedtime: string, wake: string) =>
    `Bed at ${bedtime} · up at ${wake}. Tonight's task rewards arrive in the morning.`,
  nightReward: 'Go to sleep now for up to 15 min of viewing time in the morning',
  closeAndSleep: 'Close Shorts and sleep',
  justABit: 'Just a little longer',

  // 作業タイマー
  timerLabel: 'DOPAGAKI work timer',
  pause: 'Pause',
  resume: 'Resume',
  confirmWork: 'Log my work so far',
  complete: 'Complete task',
  progressLabel: 'Work progress',
  completeToast: (line: string, reward: string) => `${line} (${reward})`,

  // デスクトップ通知
  notificationTitle: 'DOPAGAKI · Time to start',
  notificationStart: 'Start working',
  notificationSnooze: 'Check the snooze plan',
  morningTitle: 'DOPAGAKI · Good morning',
  morningBonus: (seconds: number) => `Early-to-bed bonus +${shared.durationLabel(seconds)}`,
  morningReleased: (seconds: number) => `Last night's reward +${shared.durationLabel(seconds)}`,
  testNoticeTitle: 'DOPAGAKI connection test',
  testNoticeMessage: 'If you see this notification, confirm it on the dashboard.',

  // ポップアップ
  popupAvailable: 'Viewing time available',
  popupUnlimited: 'No limit',
  popupWorking: (title: string, clock: string) => `Working · ${title} · ${clock}`,
  popupIdle: "We'll notify you when it's time to start a task.",
  popupNext: (title: string, time: string) => `Next task · ${time} ${title}`,
  popupChecking: 'Checking the connection…',
  popupConnected: 'Connected',
  popupOpenToConnect: 'Open the dashboard to connect.',
  popupConnectFrom: (origin: string) => `Connect from ${origin}.`,
  popupOpenApp: 'Open the dashboard',
  popupWorkSite: 'Show the timer on this work site',
  popupOpenWorkPage: 'Open the page you will work on, then press this.',
  popupPermissionUnchanged: 'Permissions were not changed.',
  popupWorkSiteEnabled: 'The work timer will show on this site.',
  popupWorkSiteFailed: "Couldn't show the timer.",
  popupStartNext: 'Start the next task',
  popupSignIn: 'Connect the extension with Google',
  popupTestNotice: 'Test notification',

  workReward(rewardSeconds: number, deferred: boolean): string {
    return rewardSeconds > 0
      ? `Work logged. Viewing time +${shared.durationLabel(rewardSeconds)}${deferred ? ' (arrives in the morning)' : ''}`
      : 'Work logged';
  },

  rewardDescription(wallet): string {
    if (!wallet) return 'Log the time you put in';
    if (wallet.policy.mode !== 'rewards') return 'One step at a time until your stopping point';
    const work = shared.durationLabel(wallet.rules.workSecondsPerReward);
    const reward = shared.durationLabel(wallet.rules.rewardSecondsPerUnit);
    return `${work} of work earns ${reward} of viewing. Time grows as you log it`;
  },

  timeLabel(value: string): string {
    return new Date(value).toLocaleTimeString('en-US', {
      hour: '2-digit',
      minute: '2-digit',
      hourCycle: 'h23',
    });
  },
};
