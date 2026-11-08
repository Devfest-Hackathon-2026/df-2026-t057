import { ChangeDetectionStrategy, Component, computed, inject } from '@angular/core';
import { MatButtonModule } from '@angular/material/button';
import { LocaleStore } from '../../core/i18n/locale.store';
import { PageHeading } from '../../shared/ui/page-heading';
import { TaskActions } from '../tasks/task-actions';
import { WalletStore } from '../wallet/wallet.store';

interface Block {
  readonly title: string;
  readonly body: string;
}

/** このアプリについて（静的な説明）。 */
@Component({
  selector: 'app-about-page',
  imports: [MatButtonModule, PageHeading],
  templateUrl: './about-page.html',
  styleUrl: './about-page.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class AboutPage {
  protected readonly actions = inject(TaskActions);
  private readonly wallet = inject(WalletStore);
  private readonly shared = inject(LocaleStore).shared;
  protected readonly text = inject(LocaleStore).web;

  protected readonly steps = computed<readonly Block[]>(() => {
    const { register, schedule, notify, choose, reward } = this.text().about.page.steps;
    // 報酬の説明は、ルールを読み込めていればその値で書く。
    const rules = this.wallet.value()?.rules;
    const work = this.shared().durationLabel(rules?.workSecondsPerReward ?? 300);
    const rewarded = this.shared().durationLabel(rules?.rewardSecondsPerUnit ?? 60);
    return [
      register,
      schedule,
      notify,
      choose,
      { title: reward.title, body: reward.body(work, rewarded) },
    ];
  });

  protected readonly principles = computed<readonly Block[]>(() => {
    const { youDecide, noBlame, privacy } = this.text().about.page.principles;
    return [youDecide, noBlame, privacy];
  });
}
