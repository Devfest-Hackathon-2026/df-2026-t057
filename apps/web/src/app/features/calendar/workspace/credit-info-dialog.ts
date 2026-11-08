import { ChangeDetectionStrategy, Component, computed, inject } from '@angular/core';
import { MatButtonModule } from '@angular/material/button';
import { MatDialogModule, MatDialogRef } from '@angular/material/dialog';
import { MatIconModule } from '@angular/material/icon';
import { Router } from '@angular/router';
import { durationLabel } from '@dopagaki/messages';
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

  protected readonly mode = computed(() => this.wallet.value()?.policy.mode ?? 'rewards');
  protected readonly labels = computed(() => {
    const wallet = this.wallet.value();
    const rules = wallet?.rules;
    return {
      work: durationLabel(rules?.workSecondsPerReward ?? 300),
      reward: durationLabel(rules?.rewardSecondsPerUnit ?? 60),
      dailyRewardCap: durationLabel(rules?.dailyRewardCapSeconds ?? 1200),
      registration: durationLabel(rules?.registrationRewardSeconds ?? 300),
      balanceCap: durationLabel(rules?.balanceCapSeconds ?? 1800),
      dailyRefill: durationLabel(rules?.dailyRefillSeconds ?? 600),
      dailyLimit: durationLabel(wallet?.policy.dailyLimitSeconds ?? 1800),
      choices: (rules?.viewingChoicesSeconds ?? [300, 600])
        .map((seconds) => durationLabel(seconds))
        .join('・'),
    };
  });

  protected openSettings(): void {
    this.ref.close();
    void this.router.navigateByUrl('/settings');
  }
}
