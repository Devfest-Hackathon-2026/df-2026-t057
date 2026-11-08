import { Injectable, effect, inject, signal } from '@angular/core';
import { Title } from '@angular/platform-browser';
import { TitleStrategy, type RouterStateSnapshot } from '@angular/router';
import type { WebMessages } from '@dopagaki/messages';
import { LocaleStore } from './locale.store';

/** ルートの `title` に書くキー。 */
export type PageTitleKey = keyof WebMessages['app']['titles'];

/** ルートの `title` に書いたキーを、画面の言語のタイトルにする。言語を切り替えたら付け直す。 */
@Injectable()
export class LocalizedTitleStrategy extends TitleStrategy {
  private readonly title = inject(Title);
  private readonly text = inject(LocaleStore).web;
  private readonly key = signal<PageTitleKey | undefined>(undefined);

  constructor() {
    super();
    effect(() => {
      const key = this.key();
      if (key) this.title.setTitle(this.text().app.titles[key]);
    });
  }

  override updateTitle(snapshot: RouterStateSnapshot): void {
    // title の無いルートでは、前のタイトルを残す（Angular の既定と同じ）。
    const key = this.buildTitle(snapshot) as PageTitleKey | undefined;
    if (key) this.key.set(key);
  }
}
