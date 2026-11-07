import {
  Component,
  computed,
  effect,
  ElementRef,
  inject,
  Injectable,
  OnDestroy,
  signal,
  viewChild,
} from '@angular/core';
import { Router } from '@angular/router';
import { A11yModule } from '@angular/cdk/a11y';
import { MatButtonModule } from '@angular/material/button';
import { Api } from './api';
import { TaskActions } from './task-actions';

interface Step {
  /** `data-tour` value of the element to highlight; none shows the card in the centre. */
  target?: string;
  title: string;
  body: string[];
}

const STEPS: Step[] = [
  {
    title: 'DOPAGAKIへようこそ',
    body: [
      '締切から逆算して作業の予定を作り、取り組んだぶんだけショート動画を見られる時間が増えるアプリです。',
      '覚えることは「課題を登録する → 通知が来たら、はじめる」だけ。1分で画面を案内します。',
    ],
  },
  {
    target: 'add',
    title: 'まずは課題を1つ登録',
    body: [
      '入力するのは「課題名・締切・必要時間」の3つだけ。締切はカレンダーと時刻のリストから選べます。',
      '登録するだけで視聴＋5分。',
    ],
  },
  {
    target: 'next',
    title: '取りかかる時間は自動で決まります',
    body: [
      '締切から逆算した作業予定がここに出ます。',
      '時間になると通知が届くので、「はじめる」でタイマーを開始します。',
    ],
  },
  {
    target: 'credit',
    title: '今日、見られる時間',
    body: [
      '作業を記録すると、5分ごとに視聴1分増えます。',
      'YouTubeショートを見るときは、Chrome拡張がこの時間を使います。',
    ],
  },
  {
    target: 'calendar',
    title: '予定はカレンダーで確認',
    body: ['予定を選ぶと詳細が開き、内容の編集や時間の調整ができます。'],
  },
  {
    target: 'assistant',
    title: '言葉で相談することもできます',
    body: [
      '「金曜までにレポート、2時間くらい」のように書くと、予定の案を作ります。',
      '確認して確定するまで、登録はされません。',
    ],
  },
  {
    target: 'settings',
    title: '設定はあとからで大丈夫',
    body: [
      '最初はそのままで使えます。慣れてきたら「夜の区切り（就寝時刻）」と「Google連携」だけ設定すると便利です。',
      'この案内は、右上の「使い方」からいつでも見直せます。',
    ],
  },
  {
    title: '準備できました',
    body: ['まずは、今週の課題を1つ登録してみましょう。'],
  },
];

@Injectable({ providedIn: 'root' })
export class TutorialService {
  private readonly api = inject(Api);
  private readonly router = inject(Router);
  readonly active = signal(false);
  readonly step = signal(0);

  private key() {
    return `dopagaki.tutorial.seen:${this.api.accountEmail() || this.api.config()?.mode || ''}`;
  }
  seen() {
    try {
      return localStorage.getItem(this.key()) === '1';
    } catch {
      return true; // Storage blocked: never nag on every load.
    }
  }
  private markSeen() {
    try {
      localStorage.setItem(this.key(), '1');
    } catch {
      /* Storage is optional. */
    }
  }
  start() {
    this.markSeen();
    this.step.set(0);
    this.active.set(true);
    if (!this.router.url.startsWith('/calendar'))
      void this.router.navigateByUrl('/calendar', { replaceUrl: true });
  }
  finish() {
    this.markSeen();
    this.active.set(false);
  }
}

@Component({
  selector: 'app-tutorial',
  imports: [A11yModule, MatButtonModule],
  styles: `
    .tour-layer {
      position: fixed;
      inset: 0;
      z-index: 1100;
    }
    .tour-dim {
      position: absolute;
      inset: 0;
      background: rgb(15 20 12 / 55%);
    }
    .tour-spot {
      position: absolute;
      border-radius: 14px;
      box-shadow:
        0 0 0 4px var(--lime),
        0 0 0 9999px rgb(15 20 12 / 55%);
      transition: all 0.25s ease;
      pointer-events: none;
    }
    .tour-layer.reduced .tour-spot,
    .tour-layer.reduced .tour-card {
      transition: none;
    }
    .tour-card {
      position: absolute;
      width: min(360px, calc(100vw - 32px));
      background: #fff;
      color: var(--ink);
      border-radius: 16px;
      padding: 20px 20px 14px;
      box-shadow: 0 12px 40px rgb(0 0 0 / 25%);
      transition:
        top 0.25s ease,
        left 0.25s ease;
    }
    .tour-progress {
      display: flex;
      gap: 4px;
      margin-bottom: 12px;
    }
    .tour-progress span {
      height: 4px;
      flex: 1;
      border-radius: 2px;
      background: var(--line);
    }
    .tour-progress span.done {
      background: var(--blue);
    }
    h2 {
      font-size: 18px;
      margin: 0 0 8px;
    }
    p {
      margin: 0 0 8px;
      line-height: 1.7;
    }
    .tour-count {
      font-size: 12px;
      color: var(--muted);
      margin-right: auto;
    }
    .tour-actions {
      display: flex;
      align-items: center;
      gap: 4px;
      margin-top: 12px;
      flex-wrap: wrap;
    }
  `,
  template: `
    @if (tour.active()) {
      <div
        class="tour-layer"
        [class.reduced]="api.state()?.settings?.reducedMotion"
        (keydown)="onKey($event)"
      >
        @if (rect(); as r) {
          <div
            class="tour-spot"
            [style.top.px]="r.top"
            [style.left.px]="r.left"
            [style.width.px]="r.width"
            [style.height.px]="r.height"
          ></div>
        } @else {
          <div class="tour-dim"></div>
        }
        <section
          #card
          class="tour-card"
          role="dialog"
          aria-modal="true"
          aria-labelledby="tour-title"
          aria-describedby="tour-body"
          cdkTrapFocus
          [cdkTrapFocusAutoCapture]="true"
          [style.top.px]="cardPos().top"
          [style.left.px]="cardPos().left"
        >
          <div class="tour-progress" aria-hidden="true">
            @for (s of steps; track $index) {
              <span [class.done]="$index <= index()"></span>
            }
          </div>
          <h2 id="tour-title">{{ current().title }}</h2>
          <div id="tour-body">
            @for (line of current().body; track $index) {
              <p>{{ line }}</p>
            }
          </div>
          <div class="tour-actions">
            <span class="tour-count">{{ index() + 1 }} / {{ steps.length }}</span>
            @if (index() === 0) {
              <button mat-button (click)="tour.finish()">スキップ</button>
              <button mat-flat-button cdkFocusInitial (click)="go(1)">案内を見る</button>
            } @else if (index() === steps.length - 1) {
              <button mat-button (click)="tour.finish()">閉じる</button>
              <button mat-flat-button cdkFocusInitial (click)="addTask()">課題を登録する</button>
            } @else {
              <button mat-button (click)="tour.finish()">終了</button>
              <button mat-button (click)="go(-1)">戻る</button>
              <button mat-flat-button cdkFocusInitial (click)="go(1)">次へ</button>
            }
          </div>
        </section>
      </div>
    }
  `,
})
export class Tutorial implements OnDestroy {
  readonly tour = inject(TutorialService);
  readonly api = inject(Api);
  private readonly actions = inject(TaskActions);
  readonly steps = STEPS;
  readonly index = this.tour.step;
  readonly current = computed(() => STEPS[this.index()]);
  readonly rect = signal<{ top: number; left: number; width: number; height: number } | null>(null);
  private readonly card = viewChild<ElementRef<HTMLElement>>('card');
  private readonly cardSize = signal({ width: 360, height: 240 });
  readonly cardPos = computed(() => {
    const vw = window.innerWidth;
    const vh = window.innerHeight;
    const { width, height } = this.cardSize();
    const r = this.rect();
    const clampX = (x: number) => Math.max(16, Math.min(x, vw - width - 16));
    const clampY = (y: number) => Math.max(16, Math.min(y, vh - height - 16));
    if (!r) return { top: clampY((vh - height) / 2), left: clampX((vw - width) / 2) };
    const gap = 14;
    if (r.top + r.height + gap + height <= vh - 16)
      return { top: r.top + r.height + gap, left: clampX(r.left) };
    if (r.top - gap - height >= 16) return { top: r.top - gap - height, left: clampX(r.left) };
    // Large targets (the calendar): beside it, or overlapping its lower part.
    if (r.left + r.width + gap + width <= vw - 16)
      return { top: clampY(r.top), left: r.left + r.width + gap };
    if (r.left - gap - width >= 16) return { top: clampY(r.top), left: r.left - gap - width };
    return { top: clampY(vh - height - 24), left: clampX((vw - width) / 2) };
  });
  private frame = 0;
  private readonly measure = () => {
    cancelAnimationFrame(this.frame);
    this.frame = requestAnimationFrame(() => this.update(false));
  };

  constructor() {
    effect(() => {
      if (!this.tour.active()) return;
      this.index();
      // Wait for navigation/render, then bring the target into view.
      setTimeout(() => this.update(true), 50);
    });
    window.addEventListener('resize', this.measure);
    window.addEventListener('scroll', this.measure, true);
  }
  ngOnDestroy() {
    window.removeEventListener('resize', this.measure);
    window.removeEventListener('scroll', this.measure, true);
    cancelAnimationFrame(this.frame);
  }
  private update(scroll: boolean) {
    const card = this.card()?.nativeElement;
    if (card) this.cardSize.set({ width: card.offsetWidth, height: card.offsetHeight });
    const target = this.current().target;
    const el = target ? document.querySelector<HTMLElement>(`[data-tour="${target}"]`) : null;
    let box = el?.getBoundingClientRect();
    if (!el || !box || !box.width || !box.height) {
      this.rect.set(null);
      return;
    }
    if (scroll && (box.top < 0 || box.bottom > window.innerHeight)) {
      el.scrollIntoView({
        block: box.height > window.innerHeight * 0.6 ? 'start' : 'center',
        behavior: 'instant',
      });
      box = el.getBoundingClientRect();
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
  go(delta: number) {
    this.index.set(Math.max(0, Math.min(STEPS.length - 1, this.index() + delta)));
  }
  addTask() {
    this.tour.finish();
    this.actions.add();
  }
  onKey(event: KeyboardEvent) {
    if (event.key === 'Escape') this.tour.finish();
    else if (event.key === 'ArrowRight' && this.index() < STEPS.length - 1) this.go(1);
    else if (event.key === 'ArrowLeft' && this.index() > 0) this.go(-1);
  }
}
