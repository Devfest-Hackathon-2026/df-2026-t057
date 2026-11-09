import { TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import type { CalendarStatus } from '@dopagaki/contract';
import { FakeBackend, flush, provideFakeBackend } from '../../../testing/fake-backend';
import { ConfigStore } from '../../core/config/config.store';
import { ExtensionBridge } from '../../core/extension/extension-bridge';
import { CalendarStore } from '../calendar/calendar.store';
import { TaskActions } from '../tasks/task-actions';
import { placeCard, TutorialOverlay } from './tutorial-overlay';
import { Tutorial } from './tutorial.service';

function button(element: HTMLElement, label: string): HTMLButtonElement {
  const found = Array.from(element.querySelectorAll<HTMLButtonElement>('button')).find(
    (item) => item.textContent.trim() === label,
  );
  if (!found) throw new Error(`ボタン ${label} がない`);
  return found;
}

describe('TutorialOverlay', () => {
  const add = vi.fn();
  let backend: FakeBackend;

  async function render(): Promise<{
    element: HTMLElement;
    tour: Tutorial;
    detect: () => Promise<void>;
  }> {
    const fixture = TestBed.createComponent(TutorialOverlay);
    const tour = TestBed.inject(Tutorial);
    tour.start();
    const detect = async (): Promise<void> => {
      fixture.detectChanges();
      await flush();
    };
    await detect();
    return { element: fixture.nativeElement as HTMLElement, tour, detect };
  }

  beforeEach(() => {
    add.mockReset();
    backend = new FakeBackend();
    TestBed.configureTestingModule({
      providers: [
        ...provideFakeBackend(backend),
        provideRouter([{ path: 'calendar', children: [] }]),
        { provide: TaskActions, useValue: { add } },
      ],
    });
  });

  it('次へ・戻るで段階を移り、最後に課題の登録を開く', async () => {
    const { element, detect } = await render();
    expect(element.querySelector('[role="dialog"] h2')?.textContent).toContain(
      'DOPAGAKIへようこそ',
    );
    expect(element.textContent).toContain('1 / 14');
    button(element, 'チュートリアルを開始').click();
    await detect();
    // 連携できない環境では、Google カレンダーの段階をそのまま進める。
    expect(element.querySelector('h2')?.textContent).toContain('Googleカレンダーを連携');
    expect(element.textContent).toContain('この環境では連携できないので');
    button(element, '次へ').click();
    await detect();
    expect(element.querySelector('h2')?.textContent).toContain('まずは課題を登録');
    button(element, '戻る').click();
    await detect();
    button(element, '戻る').click();
    await detect();
    expect(element.querySelector('h2')?.textContent).toContain('DOPAGAKIへようこそ');
    for (let step = 0; step < 13; step += 1) {
      button(element, step === 0 ? 'チュートリアルを開始' : '次へ').click();
      await detect();
    }
    expect(element.querySelector('h2')?.textContent).toContain('準備できました');
    button(element, '課題を登録する').click();
    await detect();
    expect(add).toHaveBeenCalled();
    expect(element.querySelector('[role="dialog"]')).toBeNull();
  });

  it('矢印キーで移り、Escape で閉じる', async () => {
    const { element, tour, detect } = await render();
    const layer = element.querySelector('.tour-card');
    layer?.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }));
    await detect();
    expect(tour.step()).toBe(1);
    layer?.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowLeft', bubbles: true }));
    await detect();
    expect(tour.step()).toBe(0);
    layer?.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowLeft', bubbles: true }));
    expect(tour.step()).toBe(0);
    layer?.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
    await detect();
    expect(tour.active()).toBe(false);
  });

  it('拡張の入手の段階で配布ページを示し、読み込まれていれば版を出す', async () => {
    const { element, tour, detect } = await render();
    tour.step.set(7);
    await detect();
    expect(element.querySelector('h2')?.textContent).toContain('Chrome拡張を入手');
    expect(element.textContent).toContain('配布ページからZIPを取得し');
    const link = element.querySelector<HTMLAnchorElement>('a.tour-link');
    expect(link?.href).toBe(
      'https://github.com/Devfest-Hackathon-2026/df-2026-t057/releases/latest',
    );
    expect(link?.target).toBe('_blank');

    TestBed.inject(ExtensionBridge).extensionVersion.set('0.3.0');
    await detect();
    expect(element.textContent).toContain('拡張 0.3.0 が読み込まれています。');

    tour.step.set(8);
    await detect();
    expect(element.querySelector('h2')?.textContent).toContain('Chromeに読み込む');
    expect(element.querySelector('a.tour-link')).toBeNull();
  });
});

describe('TutorialOverlay with Google Calendar available', () => {
  const status: CalendarStatus = {
    enabled: true,
    connected: false,
    sync: 'disconnected',
    failedJobs: 0,
  };

  beforeEach(() => {
    sessionStorage.clear();
    const backend = new FakeBackend().on('GET', '/calendar', () => ({ body: status }));
    TestBed.configureTestingModule({
      providers: [
        ...provideFakeBackend(backend),
        provideRouter([{ path: 'calendar', children: [] }]),
        { provide: TaskActions, useValue: { add: vi.fn() } },
      ],
    });
    TestBed.inject(ConfigStore).config.set({
      mode: 'live',
      features: { ai: false, calendar: true },
    });
  });

  it('連携するまで飛ばせず、Escape でも閉じず、連携の段階から先へ進めない', async () => {
    const fixture = TestBed.createComponent(TutorialOverlay);
    const tour = TestBed.inject(Tutorial);
    const calendar = TestBed.inject(CalendarStore);
    const connect = vi
      .spyOn(calendar, 'startConnect')
      .mockResolvedValue({ ok: true, value: { url: 'https://accounts.example/' } });
    tour.start();
    const element = fixture.nativeElement as HTMLElement;
    const detect = async (): Promise<void> => {
      fixture.detectChanges();
      await flush();
    };
    await detect();
    const labels = (): string[] =>
      Array.from(element.querySelectorAll('button')).map((item) => item.textContent.trim());
    expect(labels()).not.toContain('スキップ');
    const card = element.querySelector('.tour-card');
    card?.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
    await detect();
    expect(tour.active()).toBe(true);

    button(element, 'チュートリアルを開始').click();
    await detect();
    expect(element.querySelector('h2')?.textContent).toContain('Googleカレンダーを連携');
    expect(element.textContent).toContain('連携するまで先へ進めません。');
    expect(labels()).not.toContain('次へ');
    expect(labels()).not.toContain('終了');
    card?.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }));
    await detect();
    expect(tour.step()).toBe(1);

    button(element, 'Googleカレンダーを連携').click();
    await detect();
    expect(connect).toHaveBeenCalled();
    // 同意画面から戻ったら、この段階から続ける。
    expect(tour.takeResume()).toBe(1);

    // 連携できたら、先へ進めて終えられる。
    calendar.value.set({ ...status, connected: true, sync: 'synced' });
    await detect();
    expect(element.textContent).toContain('連携できました。');
    button(element, '次へ').click();
    await detect();
    expect(element.querySelector('h2')?.textContent).toContain('まずは課題を登録');
    button(element, '終了').click();
    await detect();
    expect(tour.active()).toBe(false);
  });
});

describe('placeCard', () => {
  const card = { width: 360, height: 240 };
  it('対象がなければ中央に置く', () => {
    expect(placeCard(null, card, { width: 1000, height: 800 })).toEqual({ top: 280, left: 320 });
  });
  it('狭い画面でも画面の内側に収める', () => {
    const position = placeCard(
      { top: 100, left: 300, width: 80, height: 40 },
      { width: 358, height: 240 },
      {
        width: 390,
        height: 800,
      },
    );
    expect(position.left).toBeGreaterThanOrEqual(16);
    expect(position.left + 358).toBeLessThanOrEqual(390 - 16 + 0.5);
    expect(position.top).toBe(154);
  });
  it('下に入らなければ上に置く', () => {
    expect(
      placeCard({ top: 600, left: 20, width: 100, height: 100 }, card, { width: 1000, height: 800 })
        .top,
    ).toBe(346);
  });
});
