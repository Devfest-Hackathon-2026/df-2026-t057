import { ChangeDetectionStrategy, Component, computed, inject } from '@angular/core';
import { MatButtonModule } from '@angular/material/button';
import { durationLabel } from '@dopagaki/messages';
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

  /** 報酬の説明。ルールを読み込めていればその値で書く。 */
  private readonly rewardBody = computed(() => {
    const rules = this.wallet.value()?.rules;
    const rate = rules
      ? `取り組んだ${durationLabel(rules.workSecondsPerReward)}ごとに、Shortsを見られる時間が${durationLabel(rules.rewardSecondsPerUnit)}増えます。`
      : '取り組んだ5分ごとに、Shortsを見られる時間が1分増えます。';
    return `${rate}Shortsは、がんばった自分へのご褒美です。`;
  });

  protected readonly steps = computed<readonly Block[]>(() => [
    {
      title: '登録する',
      body: '課題と締切、必要な時間を書きます。「なぜやるのか」を一行添えると、通知でその言葉が自分に返ってきます。',
    },
    {
      title: '予定になる',
      body: '締切から逆算して、取りかかる時間を決めます。AIに頼めば、課題を小さな手順にも分けます。',
    },
    {
      title: 'Shortsの上で知らせる',
      body: '始める時間にShortsを見ていたら、画面いっぱいに「なぜ」と「最初の一手」を出します。',
    },
    {
      title: '自分で選ぶ',
      body: '「作業をはじめる」か「スヌーズ」かは本人が決めます。スヌーズしても責めずに、予定を立て直します。',
    },
    { title: 'ご褒美にする', body: this.rewardBody() },
  ]);

  protected readonly principles: readonly Block[] = [
    {
      title: 'AIは提案、決めるのは本人',
      body: '予定の案はAIが作りますが、時間と締切はプログラムが確かめ、確定するのは本人がOKを押したときだけです。',
    },
    {
      title: '責めない',
      body: '遅れても、スヌーズしても、立て直せば大丈夫。言葉も通知も、自分を責めずに次の一手へ向かえるようにしています。',
    },
    {
      title: '見ている動画は送らない',
      body: '再生している時間だけを数え、動画の内容や閲覧履歴をAIに送ることはありません。',
    },
  ];
}
