import { Injectable, signal } from '@angular/core';

/**
 * ログイン中のアカウントの「世代」。アカウントが切り替わると世代が進み、
 * 登録された store はすべて空に戻る。古い世代で始めた通信の結果は store に書き込まない。
 */
@Injectable({ providedIn: 'root' })
export class AccountScope {
  readonly epoch = signal(0);
  private readonly resetters: (() => void)[] = [];

  onReset(reset: () => void): void {
    this.resetters.push(reset);
  }

  reset(): void {
    this.epoch.update((epoch) => epoch + 1);
    for (const reset of this.resetters) reset();
  }
}
