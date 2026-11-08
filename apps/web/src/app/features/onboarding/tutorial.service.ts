import { Injectable, computed, inject, signal } from '@angular/core';
import { Router } from '@angular/router';
import { AuthService } from '../../core/auth/auth.service';

const resumeKey = 'dopagaki.tutorial.resume';

/** 使い方の案内。見たかどうかはアカウントごとに localStorage に残す。 */
@Injectable({ providedIn: 'root' })
export class Tutorial {
  private readonly auth = inject(AuthService);
  private readonly router = inject(Router);
  readonly active = signal(false);
  readonly step = signal(0);

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

  /**
   * 案内を始める。見たことは終えたときに残す（Google カレンダーを連携するまで終えられないので、
   * 途中で閉じたり再読み込みしたりしたら、次に開いたときもう一度出す）。
   */
  start(step = 0): void {
    this.step.set(step);
    this.active.set(true);
    if (!this.router.url.startsWith('/calendar')) {
      void this.router.navigate(['/calendar'], { replaceUrl: true });
    }
  }

  /** 前後の段階へ移る（範囲の外へは出ない）。 */
  go(delta: number, count: number): void {
    this.step.update((step) => Math.max(0, Math.min(count - 1, step + delta)));
  }

  /** Google の同意画面へ移る前に、戻ってきたら続ける段階を残す。 */
  rememberResume(step: number): void {
    try {
      sessionStorage.setItem(resumeKey, String(step));
    } catch {
      // 残せなければ、戻ったあとは最初から案内する。
    }
  }

  /** 同意画面から戻ったときに続ける段階。一度読んだら消す。 */
  takeResume(): number | undefined {
    try {
      const value = sessionStorage.getItem(resumeKey);
      sessionStorage.removeItem(resumeKey);
      const step = value === null ? Number.NaN : Number(value);
      return Number.isInteger(step) && step >= 0 ? step : undefined;
    } catch {
      return undefined;
    }
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
