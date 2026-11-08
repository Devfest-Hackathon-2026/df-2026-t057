import { Injectable, computed, inject, signal } from '@angular/core';
import { Router } from '@angular/router';
import { AuthService } from '../../core/auth/auth.service';

/** 使い方の案内。見たかどうかはアカウントごとに localStorage に残す。 */
@Injectable({ providedIn: 'root' })
export class Tutorial {
  private readonly auth = inject(AuthService);
  private readonly router = inject(Router);
  readonly active = signal(false);
  readonly step = signal(0);
  /** 案内が今強調している要素の `data-tour`（案内を閉じているときは undefined）。画面の側が表示を合わせるのに使う。 */
  readonly target = signal<string | undefined>(undefined);

  private readonly storageKey = computed(() => {
    const email = this.auth.account()?.email;
    return `dopagaki.tutorial.seen:${email !== undefined && email !== '' ? email : (this.auth.mode() ?? '')}`;
  });

  seen(): boolean {
    try {
      return localStorage.getItem(this.storageKey()) === '1';
    } catch {
      // 保存できない環境では、毎回出さない。
      return true;
    }
  }

  start(): void {
    this.markSeen();
    this.step.set(0);
    this.active.set(true);
    if (!this.router.url.startsWith('/calendar')) {
      void this.router.navigate(['/calendar'], { replaceUrl: true });
    }
  }

  /** 前後の段階へ移る（範囲の外へは出ない）。 */
  go(delta: number, count: number): void {
    this.step.update((step) => Math.max(0, Math.min(count - 1, step + delta)));
  }

  finish(): void {
    this.markSeen();
    this.active.set(false);
  }

  private markSeen(): void {
    try {
      localStorage.setItem(this.storageKey(), '1');
    } catch {
      // 保存できなくても案内は続ける。
    }
  }
}
