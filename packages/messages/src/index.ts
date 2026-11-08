// 表示する文言はすべてこのパッケージに置く（AGENTS.md「APIの仕様」）。言語ごとに ja/ と en/ に分け、英語の型は日本語に合わせる。
// shared は web と拡張で共有する code → 文言の対応表、extension は拡張だけ、web は管理画面だけで使う文言。
import { ApiError, NetworkError, type Locale } from '@dopagaki/contract';
import { extension as enExtension } from './en/extension.ts';
import { shared as enShared } from './en/shared.ts';
import { web as enWeb } from './en/web.ts';
import { extension as jaExtension, type ExtensionMessages } from './ja/extension.ts';
import { shared as jaShared, type SharedMessages } from './ja/shared.ts';
import { web as jaWeb, type WebMessages } from './ja/web.ts';

export * from './locale.ts';
export type { RewardRuleSource } from './ja/extension.ts';
export type { ExtensionMessages, SharedMessages, WebMessages };

export interface Messages {
  shared: SharedMessages;
  extension: ExtensionMessages;
  web: WebMessages;
}

export const messages: Record<Locale, Messages> = {
  ja: { shared: jaShared, extension: jaExtension, web: jaWeb },
  en: { shared: enShared, extension: enExtension, web: enWeb },
};

/** 例外を、利用者に見せる一文にする。 */
export function errorMessage(error: unknown, shared: SharedMessages): string {
  if (error instanceof ApiError) {
    // 項目ごとのエラーがあれば、そのほうが具体的（例: plan.invalid_changes の内訳）。
    const first = error.fields[0];
    return first ? shared.fields[first.code] : shared.problems[error.code];
  }
  if (error instanceof NetworkError) return shared.network;
  return shared.problems.internal;
}

/** 秒数を「MM:SS」で表す（タイマー用）。言語によらない。 */
export function clockLabel(seconds: number): string {
  const total = Math.max(0, Math.floor(seconds));
  return `${String(Math.floor(total / 60)).padStart(2, '0')}:${String(total % 60).padStart(2, '0')}`;
}
