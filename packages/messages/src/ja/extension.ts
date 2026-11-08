// Chrome拡張だけで使う、日本語の文言。エラーの文言と相棒の一言は shared.ts の対応表から引く。
// 文言の値は API が返したものをそのまま使い、ここでは計算しない。英語の文言（en/extension.ts）は、このオブジェクトの型に合わせる。
import type { TimePolicyMode } from '@dopagaki/contract';
import { shared } from './shared.ts';

/** 報酬のルールの説明に必要な、Wallet の項目。値は API から受け取る。 */
export interface RewardRuleSource {
  policy: { mode: TimePolicyMode };
  rules: { workSecondsPerReward: number; rewardSecondsPerUnit: number };
}

export const extension = {
  // 共通
  pending: '処理しています…',
  transportFailed: '接続できませんでした。入力は残っています。再試行してください。',
  accountChanged: 'アカウントが切り替わりました。管理画面から再接続してください。',
  connectFromApp: '管理画面から接続してください。',
  connectFromPopup: '拡張のポップアップから同じGoogleアカウントで接続してください。',
  untrustedSender: '接続元を確認してください。',
  operationUnknown: '操作を確認してください。',
  fromShorts: '視聴中のShortsから操作してください。',
  claimFirst: 'このタブで続ける場合は、視聴の区切りを引き継いでください。',
  sessionEnded: 'この視聴の区切りは終了しています。',
  autoplayBlocked: '動画の再生ボタンから再開できます。',
  close: '閉じる',

  // Googleアカウントでの接続
  authConfigUnavailable: '接続設定を取得できませんでした。通信が戻ると再試行します。',
  authNotConfigured: 'Googleログインが設定されていません。',
  authUnavailable: 'この環境では拡張のGoogleログインを利用しません。',
  authInProgress: 'ログインを確認しています。',
  authTimedOut: 'ログインを確認できませんでした。もう一度接続してください。',
  authFailed: 'Googleアカウントへの接続に失敗しました。',

  // 視聴の区切り
  choiceLabel: '視聴の区切り',
  choiceOfflineLabel: '視聴時間の接続を確認',
  choiceOffline: '接続を確認してから再開しましょう',
  choiceActive: '同じ区切りを引き継ぎますか',
  choiceEnded: '決めた区切りになりました',
  choiceNone: '今回は、何分見ますか',
  choiceReminder: '見る長さを決めてから再生します。',
  choiceRemaining: (clock: string) => `今日の残り枠 ${clock}。再生中の時間だけを数えます。`,
  reconnect: '再接続して続ける',
  continueHere: (clock: string) => `このタブで続ける（残り${clock}）`,
  choose: (minutes: number, limit: string | undefined) =>
    `${minutes}分にする${limit ? `（残り${limit}まで）` : ''}`,
  extend: '残り枠からもう5分',
  exhausted: shared.problems['viewing.exhausted'],
  endHere: 'ここで終える',
  notNow: '今は見ない',
  nextWork: '次の作業へ',
  viewingBarLabel: '視聴タイマー',
  viewingLeft: (clock: string) => `今回あと ${clock}`,
  stopped: '決めたところで、視聴を終えました。',

  // 通知
  noticeLabel: '課題をはじめる時間です',
  noticeEyebrow: "IT'S YOUR TURN",
  noticeHeadingMahjong: 'まず、一手。',
  noticeHeading: 'そろそろ、やるか。',
  why: (why: string) => `なぜ：${why}`,
  firstStep: (step: string) => `最初の一手：${step}`,
  workMinutes: (minutes: number) => `${minutes}分の作業`,
  startWork: '作業をはじめる',
  snooze: (minutes: number) => `${minutes}分スヌーズ・予定を確認`,
  adjust: '予定を調整する',
  later: 'あとで',

  // 変更案
  draftLabel: '変更する予定を確認',
  draftAdjustHeading: '予定を調整',
  draftSnoozeHeading: 'スヌーズ後の予定を確認',
  draftNotice: 'OKを押すまで、予定は変わりません。',
  draftShortened: '締切に合わせて、作業時間を短くしています。',
  moveUp: '↑ 順序を上へ',
  moveUpLabel: (title: string) => `${title}を1つ上へ移す`,
  before: (start: string, end: string, minutes: number) =>
    `変更前 ${start}–${end} · 元の見積もり ${minutes}分`,
  start: '開始',
  end: '終了',
  invalidTime: '終了は開始より後にしてください。',
  instructionPlaceholder: 'Geminiへの指示（例：見直しに5分回して）',
  instructionLabel: 'Geminiに予定の調整を指示',
  regenerate: 'Geminiで候補を作り直す',
  commit: 'OK・この予定に変更',
  cancel: 'キャンセル',
  back: '戻る',

  // 夜
  nightLabel: '寝る時間が近づいています',
  nightEyebrow: 'GOOD NIGHT',
  nightHeading: 'そろそろ、寝る準備。',
  nightSchedule: (bedtime: string, wake: string) =>
    `就寝 ${bedtime} ・ 起床 ${wake}。今夜の課題の報酬は、朝に受け取れます。`,
  nightReward: 'このまま寝ると、朝に視聴時間 最大15分',
  closeAndSleep: 'Shortsを閉じて寝る',
  justABit: 'あと少しだけ',

  // 作業タイマー
  timerLabel: 'DOPAGAKI 作業タイマー',
  pause: '一時停止',
  resume: '再開',
  confirmWork: 'ここまで取り組んだ',
  complete: '課題を完了',
  progressLabel: '作業の進捗',
  completeToast: (line: string, reward: string) => `${line}（${reward}）`,

  // デスクトップ通知
  notificationTitle: 'DOPAGAKI · はじめる時間です',
  notificationStart: '作業をはじめる',
  notificationSnooze: 'スヌーズの予定を確認',
  morningTitle: 'DOPAGAKI · おはよう',
  morningBonus: (seconds: number) => `早寝ボーナス ＋${shared.durationLabel(seconds)}`,
  morningReleased: (seconds: number) => `昨夜の報酬 ＋${shared.durationLabel(seconds)}`,
  testNoticeTitle: 'DOPAGAKI 接続テスト',
  testNoticeMessage: '通知が表示されたら管理画面で確認してください。',

  // ポップアップ
  popupAvailable: '視聴できる時間',
  popupUnlimited: '制限なし',
  popupWorking: (title: string, clock: string) => `作業中 · ${title} · ${clock}`,
  popupIdle: '課題の開始時間になると通知します。',
  popupNext: (title: string, time: string) => `次の課題 · ${time} ${title}`,
  popupChecking: '接続を確認しています…',
  popupConnected: '接続済み',
  popupOpenToConnect: '管理画面を開くと接続されます。',
  popupConnectFrom: (origin: string) => `${origin} から接続してください。`,
  popupOpenApp: '管理画面を開く',
  popupWorkSite: 'この作業サイトにタイマーを表示',
  popupOpenWorkPage: '作業するページを開いてから押してください。',
  popupPermissionUnchanged: '許可を変更していません。',
  popupWorkSiteEnabled: 'このサイトで作業タイマーを表示します。',
  popupWorkSiteFailed: 'タイマーを表示できませんでした。',
  popupStartNext: '次の課題を開始',
  popupSignIn: 'Googleアカウントで拡張に接続',
  popupTestNotice: '通知をテスト',
  popupAddTask: '課題を登録',
  popupTaskTitle: '課題名',
  popupTaskTitleRequired: '課題名を入力してください。',
  popupTaskDeadline: '締切',
  popupTaskDeadlinePast: '締切は未来の日時を選んでください。',
  popupTaskMinutes: '必要時間（分）',
  popupTaskMinutesRange: '1〜720分で入力してください。',
  popupTaskPresets: '必要時間をすばやく選ぶ',
  popupTaskPreset: (minutes: number) => (minutes < 60 ? `${minutes}分` : `${minutes / 60}時間`),
  popupTaskDetails: '詳細を追加',
  popupTaskWhy: '目的（任意）',
  popupTaskUrl: '作業リンク（任意）',
  popupTaskNotes: 'やること・メモ',
  popupTaskRegister: '登録する',
  popupTaskCreated: (title: string) => `「${title}」を登録しました。`,

  /** 作業を記録したときの一文。報酬は API が返した値をそのまま使う。 */
  workReward(rewardSeconds: number, deferred: boolean): string {
    return rewardSeconds > 0
      ? `作業を記録しました。視聴時間 ＋${shared.durationLabel(rewardSeconds)}${deferred ? '（朝に受け取れます）' : ''}`
      : '作業を記録しました';
  },

  /** 通知に添える、報酬のルールの説明。 */
  rewardDescription(wallet: RewardRuleSource | null): string {
    if (!wallet) return '取り組んだ時間を記録しましょう';
    if (wallet.policy.mode !== 'rewards') return '区切りまで、ひとつずつ取り組みましょう';
    const work = shared.durationLabel(wallet.rules.workSecondsPerReward);
    const reward = shared.durationLabel(wallet.rules.rewardSecondsPerUnit);
    return `取り組んだ${work}で視聴${reward}。記録した分だけ増えます`;
  },

  /** 端末のタイムゾーンでの「HH:MM」。 */
  timeLabel(value: string): string {
    return new Date(value).toLocaleTimeString('ja-JP', { hour: '2-digit', minute: '2-digit' });
  },
};

export type ExtensionMessages = typeof extension;
