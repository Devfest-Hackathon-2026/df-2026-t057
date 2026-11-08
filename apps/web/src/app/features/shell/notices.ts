import { Injectable, inject, signal } from '@angular/core';
import { AccountScope } from '../../core/auth/account-scope';

export interface Notice {
  readonly id: number;
  readonly text: string;
}

/** 画面の上に出し続けるお知らせ（閉じるまで残す）。一時的な結果はスナックバーで知らせる。 */
@Injectable({ providedIn: 'root' })
export class Notices {
  private nextId = 1;
  readonly items = signal<readonly Notice[]>([]);

  constructor() {
    inject(AccountScope).onReset(() => {
      this.items.set([]);
    });
  }

  push(text: string): void {
    if (this.items().some((item) => item.text === text)) return;
    const id = this.nextId++;
    this.items.update((items) => [...items, { id, text }]);
  }

  dismiss(id: number): void {
    this.items.update((items) => items.filter((item) => item.id !== id));
  }
}
