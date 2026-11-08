import type { Locale } from '@dopagaki/contract';

export type { Locale };

/** 対応している言語。設定画面の選択肢の順。 */
export const locales: readonly Locale[] = ['ja', 'en'];

/** 言語の名前。どの言語の画面でも、その言語自身の書き方で表示する。 */
export const localeNames: Record<Locale, string> = { ja: '日本語', en: 'English' };

/** ブラウザーの言語の一覧（navigator.languages）から、対応している最初の言語を選ぶ。どれにも当たらなければ英語にする。 */
export function detectLocale(languages: readonly string[]): Locale {
  for (const language of languages) {
    const primary = language.toLowerCase().split('-')[0];
    if (primary === 'ja' || primary === 'en') return primary;
  }
  return 'en';
}
