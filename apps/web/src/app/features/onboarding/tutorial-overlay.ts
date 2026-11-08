import { A11yModule } from '@angular/cdk/a11y';
import {
  ChangeDetectionStrategy,
  Component,
  DestroyRef,
  type ElementRef,
  computed,
  effect,
  inject,
  signal,
  untracked,
  viewChild,
} from '@angular/core';
import { MatButtonModule } from '@angular/material/button';
import { MatProgressBarModule } from '@angular/material/progress-bar';
import { LocaleStore } from '../../core/i18n/locale.store';
import { SettingsStore } from '../settings/settings.store';
import { TaskActions } from '../tasks/task-actions';
import { WalletStore } from '../wallet/wallet.store';
import { tutorialSteps } from './tutorial-steps';
import { Tutorial } from './tutorial.service';

interface Box {
  readonly top: number;
  readonly left: number;
  readonly width: number;
  readonly height: number;
}

const margin = 16;
const gap = 14;

/** 案内のカードを、強調した要素の下・上・右・左のどこか（入らなければ画面の下）に置く。 */
export function placeCard(
  target: Box | null,
  card: { width: number; height: number },
  viewport: { width: number; height: number },
): { top: number; left: number } {
  const clampX = (x: number): number =>
    Math.max(margin, Math.min(x, viewport.width - card.width - margin));
  const clampY = (y: number): number =>
    Math.max(margin, Math.min(y, viewport.height - card.height - margin));
  if (!target) {
    return {
      top: clampY((viewport.height - card.height) / 2),
      left: clampX((viewport.width - card.width) / 2),
    };
  }
  if (target.top + target.height + gap + card.height <= viewport.height - margin) {
    return { top: target.top + target.height + gap, left: clampX(target.left) };
  }
  if (target.top - gap - card.height >= margin) {
    return { top: target.top - gap - card.height, left: clampX(target.left) };
  }
  if (target.left + target.width + gap + card.width <= viewport.width - margin) {
    return { top: clampY(target.top), left: target.left + target.width + gap };
  }
  if (target.left - gap - card.width >= margin) {
    return { top: clampY(target.top), left: target.left - gap - card.width };
  }
  return {
    top: clampY(viewport.height - card.height - 24),
    left: clampX((viewport.width - card.width) / 2),
  };
}

/** 使い方の案内。対象の要素を明るく残し、ほかを暗くする。 */
@Component({
  selector: 'app-tutorial',
  imports: [A11yModule, MatButtonModule, MatProgressBarModule],
  templateUrl: './tutorial-overlay.html',
  styleUrl: './tutorial-overlay.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class TutorialOverlay {
  protected readonly tour = inject(Tutorial);
  protected readonly settings = inject(SettingsStore);
  private readonly wallet = inject(WalletStore);
  private readonly actions = inject(TaskActions);
  private readonly card = viewChild<ElementRef<HTMLElement>>('card');
  protected readonly text = inject(LocaleStore).web;
  private readonly shared = inject(LocaleStore).shared;

  protected readonly steps = computed(() =>
    tutorialSteps(this.wallet.value()?.rules, this.text().onboarding.steps, this.shared()),
  );
  protected readonly index = this.tour.step;
  protected readonly current = computed(() => this.steps()[this.index()] ?? this.steps()[0]);
  protected readonly isFirst = computed(() => this.index() === 0);
  protected readonly isLast = computed(() => this.index() === this.steps().length - 1);
  protected readonly progress = computed(() => ((this.index() + 1) / this.steps().length) * 100);
  protected readonly rect = signal<Box | null>(null);
  private readonly cardSize = signal({ width: 360, height: 240 });
  private readonly viewport = signal({ width: window.innerWidth, height: window.innerHeight });
  protected readonly position = computed(() =>
    placeCard(this.rect(), this.cardSize(), this.viewport()),
  );

  private frame = 0;
  private timer: ReturnType<typeof setTimeout> | undefined;
  private restoreFocus: HTMLElement | null = null;

  constructor() {
    effect(() => {
      const active = this.tour.active();
      this.index();
      untracked(() => {
        if (!active) {
          this.restore();
          return;
        }
        if (!this.restoreFocus && document.activeElement instanceof HTMLElement) {
          this.restoreFocus = document.activeElement;
        }
        // 画面の移動と描画を待ってから、対象を画面に入れて測る。
        clearTimeout(this.timer);
        this.timer = setTimeout(() => {
          this.measure(true);
        }, 50);
      });
    });
    const remeasure = (): void => {
      if (!this.tour.active()) return;
      cancelAnimationFrame(this.frame);
      this.frame = requestAnimationFrame(() => {
        this.measure(false);
      });
    };
    window.addEventListener('resize', remeasure);
    window.addEventListener('scroll', remeasure, true);
    inject(DestroyRef).onDestroy(() => {
      window.removeEventListener('resize', remeasure);
      window.removeEventListener('scroll', remeasure, true);
      cancelAnimationFrame(this.frame);
      clearTimeout(this.timer);
    });
  }

  protected go(delta: number): void {
    this.tour.go(delta, this.steps().length);
  }

  protected finish(): void {
    this.tour.finish();
  }

  protected addTask(): void {
    this.tour.finish();
    void this.actions.add();
  }

  protected onKey(event: KeyboardEvent): void {
    if (event.key === 'Escape') {
      event.preventDefault();
      this.finish();
    } else if (event.key === 'ArrowRight' && !this.isLast()) {
      this.go(1);
    } else if (event.key === 'ArrowLeft' && !this.isFirst()) {
      this.go(-1);
    }
  }

  private measure(scroll: boolean): void {
    this.viewport.set({ width: window.innerWidth, height: window.innerHeight });
    const card = this.card()?.nativeElement;
    if (card) this.cardSize.set({ width: card.offsetWidth, height: card.offsetHeight });
    const target = this.current()?.target;
    const element = target ? document.querySelector<HTMLElement>(`[data-tour="${target}"]`) : null;
    let box = element?.getBoundingClientRect();
    if (!element || !box || box.width === 0 || box.height === 0) {
      this.rect.set(null);
      return;
    }
    if (scroll && (box.top < 0 || box.bottom > window.innerHeight)) {
      element.scrollIntoView({ block: box.height > window.innerHeight * 0.6 ? 'start' : 'center' });
      box = element.getBoundingClientRect();
    }
    const pad = 6;
    const top = Math.max(4, box.top - pad);
    const bottom = Math.min(window.innerHeight - 4, box.bottom + pad);
    this.rect.set({
      top,
      left: Math.max(4, box.left - pad),
      width: Math.min(window.innerWidth - 8, box.width + pad * 2),
      height: Math.max(0, bottom - top),
    });
  }

  private restore(): void {
    const element = this.restoreFocus;
    this.restoreFocus = null;
    if (element?.isConnected) element.focus();
  }
}
