import { Injectable, effect, inject, untracked } from '@angular/core';
import { MatSnackBar } from '@angular/material/snack-bar';
import { durationLabel } from '@dopagaki/messages';
import { AccountScope } from '../../core/auth/account-scope';
import { CompanionService } from '../companion/companion.service';
import { SleepStore } from './sleep.store';

/**
 * 朝のあいさつ。就寝の予定があれば、ページを開いたときに一度だけ前の夜を精算する。
 * 精算するものがなければ何も出さない（一晩に一度だけかはサーバーが判断する）。
 */
@Injectable({ providedIn: 'root' })
export class MorningCheck {
  private readonly sleep = inject(SleepStore);
  private readonly companion = inject(CompanionService);
  private readonly snackBar = inject(MatSnackBar);
  private checkedEpoch: number | undefined;

  constructor() {
    const scope = inject(AccountScope);
    effect(() => {
      const hasSleep = this.sleep.sleep() !== null;
      const epoch = scope.epoch();
      if (!hasSleep || this.checkedEpoch === epoch) return;
      this.checkedEpoch = epoch;
      untracked(() => {
        void this.check();
      });
    });
  }

  private async check(): Promise<void> {
    const outcome = await this.sleep.settleMorning();
    // 失敗しても次の読み込みで精算できる。邪魔をしないよう、ここでは知らせない（エラーは store に残る）。
    if (!outcome.ok || !outcome.value) return;
    const morning = outcome.value;
    const line = await this.companion.line('morning');
    const gains: string[] = [];
    if (morning.kept && morning.bonusSeconds > 0) {
      gains.push(`早寝ボーナス ＋${durationLabel(morning.bonusSeconds)}`);
    }
    if (morning.releasedSeconds > 0)
      gains.push(`昨夜の報酬 ＋${durationLabel(morning.releasedSeconds)}`);
    this.snackBar.open([line, ...gains].join(' ・ '), '閉じる', { duration: 8000 });
  }
}
