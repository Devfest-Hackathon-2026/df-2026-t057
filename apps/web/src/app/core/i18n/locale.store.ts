import { DOCUMENT, Injectable, computed, effect, inject, signal } from '@angular/core';
import { DateAdapter } from '@angular/material/core';
import { detectLocale, messages, type Locale } from '@dopagaki/messages';
import { AccountScope } from '../auth/account-scope';

/** Intl と DateAdapter に渡す、地域まで含めた言語の名前。 */
const intlLocales: Record<Locale, string> = { ja: 'ja-JP', en: 'en-US' };

/**
 * 画面の言語と、その言語の文言。
 * ログイン前はブラウザーの言語、ログイン後は本人のプロフィールの言語を使う（サーバーが作る文と言語をそろえるため）。
 */
@Injectable({ providedIn: 'root' })
export class LocaleStore {
  /** ブラウザーの言語。プロフィールを初めて保存するときの値にもする。 */
  readonly browserLocale = detectLocale(navigator.languages);
  private readonly profileLocale = signal<Locale | undefined>(undefined);

  readonly locale = computed(() => this.profileLocale() ?? this.browserLocale);
  /** Intl.DateTimeFormat などに渡す名前（例：`ja-JP`）。 */
  readonly intlLocale = computed(() => intlLocales[this.locale()]);
  /** web と拡張で共有する文言（エラーの code の対応表など）。 */
  readonly shared = computed(() => messages[this.locale()].shared);
  /** 管理画面の文言。 */
  readonly web = computed(() => messages[this.locale()].web);

  constructor() {
    const document = inject(DOCUMENT);
    const dateAdapter = inject(DateAdapter);
    effect(() => {
      document.documentElement.lang = this.locale();
      dateAdapter.setLocale(this.intlLocale());
    });
    inject(AccountScope).onReset(() => {
      this.profileLocale.set(undefined);
    });
  }

  /** 本人のプロフィールの言語に切り替える。 */
  useProfileLocale(locale: Locale): void {
    this.profileLocale.set(locale);
  }
}
