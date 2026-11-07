import { Component, inject } from '@angular/core';
import { MatButtonModule } from '@angular/material/button';
import { TaskActions } from './task-actions';

@Component({
  selector: 'app-about',
  imports: [MatButtonModule],
  templateUrl: './about.html',
  styleUrl: './about.scss',
})
export class About {
  readonly actions = inject(TaskActions);
  readonly steps = [
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
    {
      title: 'ご褒美にする',
      body: '取り組んだ5分ごとに、Shortsを見られる時間が1分増えます。Shortsは、がんばった自分へのご褒美です。',
    },
  ];
  readonly principles = [
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
