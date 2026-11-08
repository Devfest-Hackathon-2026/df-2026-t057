import { TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { FakeBackend, flush, provideFakeBackend } from '../../../testing/fake-backend';
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
    TestBed.configureTestingModule({
      providers: [
        ...provideFakeBackend(new FakeBackend()),
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
    expect(element.textContent).toContain('1 / 8');
    button(element, '案内を見る').click();
    await detect();
    expect(element.querySelector('h2')?.textContent).toContain('まずは課題を1つ登録');
    button(element, '戻る').click();
    await detect();
    expect(element.querySelector('h2')?.textContent).toContain('DOPAGAKIへようこそ');
    for (let step = 0; step < 7; step += 1) {
      button(element, step === 0 ? '案内を見る' : '次へ').click();
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
