// 拡張の画面に出す文言の言語。利用者のプロフィールの言語を使い、まだ取得していない（未接続を含む）間はブラウザーの言語にする。
import { detectLocale, messages, type Locale, type Messages } from '@dopagaki/messages';
import type { ExtensionState } from './schemas';

/** background で使う。状態を受け取った処理の中で、その状態の言語の文言を引く。 */
export function messagesFor(state: ExtensionState | null | undefined): Messages {
  return messages[state?.profile?.locale ?? detectLocale(navigator.languages)];
}

// content script とポップアップでは、画面の部品がモジュールの関数で状態を持たないので、
// 最後に受け取った状態の言語をここに覚えておき、部品はそこから文言を引く。
let shownLocale: Locale | undefined;

/** content script とポップアップで、background から状態を受け取るたびに呼ぶ。 */
export function followLocale(state: ExtensionState | null | undefined): void {
  shownLocale = state?.profile?.locale;
}

export function currentLocale(): Locale {
  return shownLocale ?? detectLocale(navigator.languages);
}

export function currentMessages(): Messages {
  return messages[currentLocale()];
}
