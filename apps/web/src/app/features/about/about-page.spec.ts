import { TestBed } from '@angular/core/testing';
import { FakeBackend, flush, provideFakeBackend } from '../../../testing/fake-backend';
import { wallet } from '../../../testing/fixtures';
import { TaskActions } from '../tasks/task-actions';
import { WalletStore } from '../wallet/wallet.store';
import { AboutPage } from './about-page';

describe('AboutPage', () => {
  const add = vi.fn();
  let backend: FakeBackend;

  async function render(): Promise<HTMLElement> {
    const fixture = TestBed.createComponent(AboutPage);
    await flush();
    await fixture.whenStable();
    return fixture.nativeElement as HTMLElement;
  }

  function headings(element: HTMLElement, level: 'h1' | 'h2' | 'h3'): string[] {
    return Array.from(element.querySelectorAll(level), (heading) =>
      heading.textContent.replace(/\s+/g, ' ').trim(),
    );
  }

  beforeEach(() => {
    add.mockReset();
    backend = new FakeBackend().on('GET', '/wallet', { body: wallet() });
    TestBed.configureTestingModule({
      providers: [...provideFakeBackend(backend), { provide: TaskActions, useValue: { add } }],
    });
  });

  it('見出しの階層で、目的・対象・しくみ・大切にしていることを説明する', async () => {
    const element = await render();
    expect(headings(element, 'h1')).toEqual(['このアプリについて']);
    expect(headings(element, 'h2')).toEqual([
      '禁止ではなく、距離感を。',
      '目的',
      '誰のためのアプリか',
      'しくみ',
      '大切にしていること',
    ]);
    expect(headings(element, 'h3')).toEqual([
      'やめられないのは、意志のせいじゃない',
      '取り上げるのは、優しさじゃない',
      '最後は、このアプリがいらなくなる',
      '登録する',
      '予定になる',
      'Shortsの上で知らせる',
      '自分で選ぶ',
      'ご褒美にする',
      'AIは提案、決めるのは本人',
      '責めない',
      '見ている動画は送らない',
    ]);
    // 各節は見出しで名前が付く。
    for (const section of Array.from(element.querySelectorAll('section'))) {
      const id = section.getAttribute('aria-labelledby') ?? '';
      expect(section.querySelector(`#${id}`)).not.toBeNull();
    }
  });

  it('報酬の説明はサーバーのルールの値で書く', async () => {
    backend.on('GET', '/wallet', {
      body: wallet({ rules: { ...wallet().rules, workSecondsPerReward: 600 } }),
    });
    await TestBed.inject(WalletStore).refresh();
    const element = await render();
    expect(element.textContent).toContain(
      '取り組んだ10分ごとに、Shortsを見られる時間が1分増えます。',
    );
  });

  it('「課題を登録してみる」で登録を開く', async () => {
    const element = await render();
    const button = Array.from(element.querySelectorAll('button')).find(
      (item) => item.textContent.trim() === '課題を登録してみる',
    );
    button?.click();
    expect(add).toHaveBeenCalledTimes(1);
  });
});
