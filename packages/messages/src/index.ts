// 表示する文言はすべてここに置く。サーバーは文言を返さず、code だけを返す（AGENTS.md「APIの仕様」）。
// Record<Union, string> にしているので、仕様に code が増えたのに文言が無いと型エラーになる。
import {
  ApiError,
  NetworkError,
  type CompanionMoment,
  type FieldCode,
  type LedgerKind,
  type NoticeMode,
  type PlanReason,
  type ProblemCode,
  type TimePolicyMode,
} from '@dopagaki/contract';

export const problemMessages: Record<ProblemCode, string> = {
  internal: '問題が発生しました。少し待って再試行してください。',
  'request.malformed': '送信した内容を読み取れませんでした。画面を再読み込みしてください。',
  'request.too_large': '送信する内容が大きすぎます。',
  'request.invalid': '入力内容を確認してください。',
  'origin.forbidden': 'このページからの操作は許可されていません。',
  'auth.required': 'Googleアカウントでログインしてください。',
  'auth.expired': 'ログインの有効期限が切れました。再度ログインしてください。',
  'idempotency.key_required': '画面を再読み込みしてから、もう一度操作してください。',
  'idempotency.key_reused': '同じ操作が別の内容で送られました。画面を再読み込みしてください。',
  'idempotency.in_progress': '前の操作を処理しています。少し待ってください。',
  'precondition.required': '画面を再読み込みしてから、もう一度操作してください。',
  'precondition.failed': '内容が更新されています。最新の内容を確認してください。',
  'external.failed': '外部サービスとの通信に失敗しました。少し待って再試行してください。',
  not_found: '見つかりませんでした。',
  'task.not_found': '課題が見つかりません。',
  'task.limit_reached': '課題は200件までです。完了済みの課題を整理してください。',
  'task.already_completed': 'この課題は完了済みです。',
  'task.not_editable': '内容の編集は、開始前の課題で行ってください。',
  'task.another_active': '別の課題を進行中です。完了してから開始してください。',
  'task.duplicate': 'この候補は登録済みです。既存の予定として調整してください。',
  'plan.start_in_past': '開始時刻は現在より後にしてください。',
  'plan.deadline_passed': '締切を過ぎています。締切か対象の課題を見直してください。',
  'plan.insufficient_time': '作業時間を十分に確保できません。課題か締切を見直してください。',
  'plan.invalid_changes': '予定の内容を確認してください。',
  'plan.changes_mismatch': '調整対象の課題が変わっています。調整をやり直してください。',
  'work.no_session': '進行中の課題がありません。',
  'work.session_mismatch': '作業の状態が変わっています。最新の内容を確認してください。',
  'work.active': '作業を記録してから変更してください。',
  'viewing.policy_missing': '視聴時間の設定を確認してください。',
  'viewing.session_active': '視聴中の区切りを引き継いでください。',
  'viewing.extend_mismatch': '区切りが変わっています。もう一度選んでください。',
  'viewing.exhausted': '今日の視聴枠を使い切りました。休憩や次の作業へ移りましょう。',
  'viewing.session_required': '視聴する長さを選んでから再生してください。',
  'viewing.session_unknown': '視聴の区切りが見つかりません。',
  'memory.full': '覚えている内容が多くなっています。作業の傾向を編集して整理してください。',
  'assistant.busy': '前の相談を処理しています。少し待ってから再試行してください。',
  'assistant.turn_conflict': '同じ相談が別の内容で送られました。新しく相談してください。',
  'assistant.context_changed': '作業の傾向が更新されました。最新の内容で再試行してください。',
  'ai.unavailable': 'この環境ではAIを利用できません。手入力をご利用ください。',
  'ai.rate_limited': 'AIの利用上限に達しました。少し待ってから再試行してください。',
  'ai.failed': 'AIの処理に失敗しました。入力は残っています。もう一度お試しください。',
  'ai.unreadable': 'AIの返答を読み取れませんでした。入力を具体的にして再試行してください。',
  'ai.no_tasks': '課題名・締切・必要な分数を入力してください。',
  'shortcut.key_malformed': 'ショートカットの接続キーを確認してください。',
  'shortcut.key_invalid': 'ショートカットの接続キーが無効です。管理画面で発行し直してください。',
  'shortcut.rate_limited': '短い間に何度も呼び出されました。少し待ってから開いてください。',
  'calendar.unavailable': 'この環境ではGoogleカレンダーに接続しません。',
  'calendar.not_connected': '先にGoogleカレンダーを連携してください。',
  'calendar.reauth_required':
    'ログイン中と同じGoogleアカウントで、カレンダーを連携し直してください。',
  'demo.only': 'サンプルはデモ環境でのみ作成できます。',
  'demo.tasks_exist': 'サンプルは課題がないときだけ追加できます。',
};

export const fieldMessages: Record<FieldCode, string> = {
  required: '入力してください。',
  too_long: '長すぎます。',
  out_of_range: '範囲内の値を入力してください。',
  invalid_format: '形式を確認してください。',
  in_past: '現在より後にしてください。',
  not_allowed: 'この値は使えません。',
  'task.title_length': '課題名を1〜200文字で入力してください。',
  'task.minutes_range': '必要時間は1〜720分で入力してください。',
  'task.deadline_past': '締切は現在より後にしてください。',
  'task.notes_length': 'メモは4000文字以内にしてください。',
  'task.why_length': 'やる理由は200文字以内にしてください。',
  'task.url_scheme': '作業リンクはhttpまたはhttpsのURLにしてください。',
  'plan.empty': '変更する予定がありません。',
  'plan.duplicate_task': '変更対象の課題を確認してください。',
  'plan.invalid_time': '開始・終了時刻を確認してください。',
  'plan.exceeds_deadline': '締切を超えています。時間か締切を見直してください。',
  'plan.overlap': '課題の時間が重なっています。',
  'plan.steps_missing': '作業の内容を入力してください。',
  'plan.steps_invalid': '作業の時間割が正しくありません。',
  'plan.end_mismatch': '作業の終了時刻が一致しません。',
  'plan.overlaps_other': '別の課題の予定と重なっています。',
  'profile.timezone': 'タイムゾーンを確認してください。',
  'sleep.time_format': '就寝と起床の時刻をHH:MMで入力してください。',
  'sleep.duration_range': '睡眠時間は4〜12時間で設定してください。',
};

export const planReasonLabels: Record<PlanReason, string> = {
  backward: '締切と必要時間から逆算した予定です。',
  ai_detailed: '作業の傾向を参考に手順を具体化し、締切から逆算しました。',
  ai_limit_fallback:
    'AIの利用上限に達したため、締切と必要時間から作成した予定です。手順は手動で調整できます。',
  snooze_kept: '作業時間を保って開始時刻を後ろへ移します。',
  snooze_shortened: '締切に合わせて作業時間を短縮する案です。変更前後を確認してください。',
  edit_replanned: '課題の内容を変更し、未開始の課題を締切から組み直します。',
  ai_adjusted: '指示に合わせて調整した案です。',
  manual: '手動で調整した予定です。',
};

export const ledgerKindLabels: Record<LedgerKind, string> = {
  initial: 'はじめの視聴時間',
  registration: '課題を登録',
  daily: '毎日の補充',
  work: '作業を記録',
  sleep: '睡眠の報酬',
  viewing: '視聴',
  release: '朝に受け取り',
};

export const noticeModeLabels: Record<NoticeMode, string> = {
  pachinko: 'パチンコ',
  mahjong: '麻雀',
  simple: 'シンプル',
};

export const timePolicyModeLabels: Record<TimePolicyMode, string> = {
  rewards: '作業で増やす',
  fixed: '毎日決まった時間',
  reminder: '声かけだけ',
  self: '自分で区切る',
};

/** 相棒の一言の、AI が使えないときの決まった文。 */
export const companionFallbacks: Record<CompanionMoment, string> = {
  notice: 'そろそろ、一緒に始めよっか。',
  snooze: '了解、少し後ろにずらそう。次の時間に、また声かけるね。',
  complete: 'やりきったね。その積み重ね、ちゃんと効いてる。',
  balance_zero: '今日の視聴はここで区切ろう。次の一手で、また時間が増えるよ。',
  app_open: '開く前に一呼吸。今日やりたいこと、ひとつ思い浮かべてみよっか。',
  night: 'そろそろ寝る準備しよっか。今夜の分は、朝ちゃんと受け取れるよ。',
  morning: 'おはよう。今日の最初の一手、一緒に決めよっか。',
};

export const networkMessage = '接続できませんでした。通信状態を確認して再試行してください。';

/** 例外を、利用者に見せる一文にする。 */
export function errorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    const first = error.fields[0];
    if (error.code === 'request.invalid' && first) return fieldMessages[first.code];
    return problemMessages[error.code];
  }
  if (error instanceof NetworkError) return networkMessage;
  return problemMessages.internal;
}

/** 秒数を「N分M秒」で表す。 */
export function durationLabel(seconds: number): string {
  const total = Math.max(0, Math.round(Math.abs(seconds)));
  const minutes = Math.floor(total / 60);
  const rest = total % 60;
  if (minutes === 0) return `${rest}秒`;
  return rest === 0 ? `${minutes}分` : `${minutes}分${rest}秒`;
}

/** 秒数を「MM:SS」で表す（タイマー用）。 */
export function clockLabel(seconds: number): string {
  const total = Math.max(0, Math.floor(seconds));
  return `${String(Math.floor(total / 60)).padStart(2, '0')}:${String(total % 60).padStart(2, '0')}`;
}
