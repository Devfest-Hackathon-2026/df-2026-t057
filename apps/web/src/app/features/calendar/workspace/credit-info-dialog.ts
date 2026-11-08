import { ChangeDetectionStrategy, Component, computed, inject } from '@angular/core';
import { MatButtonModule } from '@angular/material/button';
import { MatDialogModule, MatDialogRef } from '@angular/material/dialog';
import { MatIconModule } from '@angular/material/icon';
import { Router } from '@angular/router';
import { LocaleStore } from '../../../core/i18n/locale.store';
import { DurationPipe } from '../../../shared/ui/format';
import { WalletStore } from '../../wallet/wallet.store';

/** 「視聴時間について」。残高とルールの説明。数はサーバーのルールから表示する。 */
@Component({
  selector: 'app-credit-info-dialog',
  imports: [DurationPipe, MatButtonModule, MatDialogModule, MatIconModule],
  templateUrl: './credit-info-dialog.html',
  styleUrl: './credit-info-dialog.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class CreditInfoDialog {
  protected readonly wallet = inject(WalletStore);
  private readonly ref = inject(MatDialogRef<CreditInfoDialog>);
  private readonly router = inject(Router);
  protected readonly text = inject(LocaleStore).web;
  private readonly shared = inject(LocaleStore).shared;

  protected readonly mode = computed(() => this.wallet.value()?.policy.mode ?? 'rewards');
  protected readonly labels = computed(() => {
    const wallet = this.wallet.value();
    const rules = wallet?.rules;
    const shared = this.shared();
    return {
      work: shared.durationLabel(rules?.workSecondsPerReward ?? 300),
      reward: shared.durationLabel(rules?.rewardSecondsPerUnit ?? 60),
      dailyRewardCap: shared.durationLabel(rules?.dailyRewardCapSeconds ?? 1200),
      registration: shared.durationLabel(rules?.registrationRewardSeconds ?? 300),
      balanceCap: shared.durationLabel(rules?.balanceCapSeconds ?? 1800),
      dailyRefill: shared.durationLabel(rules?.dailyRefillSeconds ?? 600),
      dailyLimit: shared.durationLabel(wallet?.policy.dailyLimitSeconds ?? 1800),
      choices: (rules?.viewingChoicesSeconds ?? [300, 600]).map((seconds) =>
        shared.durationLabel(seconds),
      ),
    };
  });

  protected openSettings(): void {
    this.ref.close();
    void this.router.navigateByUrl('/settings');
  }
}
