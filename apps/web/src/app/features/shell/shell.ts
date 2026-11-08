import {
  ChangeDetectionStrategy,
  Component,
  DestroyRef,
  computed,
  effect,
  inject,
  signal,
  untracked,
} from '@angular/core';
import { toSignal } from '@angular/core/rxjs-interop';
import { MatButtonModule } from '@angular/material/button';
import { MatIconModule } from '@angular/material/icon';
import { MatProgressBarModule } from '@angular/material/progress-bar';
import { MatTooltipModule } from '@angular/material/tooltip';
import { NavigationEnd, Router, RouterLink, RouterLinkActive, RouterOutlet } from '@angular/router';
import { filter, map } from 'rxjs';
import { AccountScope } from '../../core/auth/account-scope';
import { AuthService } from '../../core/auth/auth.service';
import { formatFullDate } from '../../shared/ui/format';
import { CalendarStore } from '../calendar/calendar.store';
import { TutorialOverlay } from '../onboarding/tutorial-overlay';
import { Tutorial } from '../onboarding/tutorial.service';
import { ProfileStore } from '../settings/profile.store';
import { SettingsStore } from '../settings/settings.store';
import { MorningCheck } from '../sleep/morning-check';
import { SleepStore } from '../sleep/sleep.store';
import { TasksStore } from '../tasks/tasks.store';
import { ViewingStore } from '../viewing/viewing.store';
import { WalletStore } from '../wallet/wallet.store';
import { FocusBar } from '../work/focus-bar';
import { WorkSessionStore } from '../work/work-session.store';
import { AccountMenu } from './account-menu';
import { grimeOpacity } from './grime';
import { LaunchParams } from './launch-params';
import { Notices } from './notices';

interface NavItem {
  readonly path: string;
  readonly icon: string;
  readonly label: string;
}

/** ログイン後の画面の枠（メニュー・上部・作業タイマー・お知らせ）。 */
@Component({
  selector: 'app-shell',
  imports: [
    AccountMenu,
    FocusBar,
    MatButtonModule,
    MatIconModule,
    MatProgressBarModule,
    MatTooltipModule,
    RouterLink,
    RouterLinkActive,
    RouterOutlet,
    TutorialOverlay,
  ],
  templateUrl: './shell.html',
  styleUrl: './shell.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class Shell {
  protected readonly auth = inject(AuthService);
  protected readonly tutorial = inject(Tutorial);
  protected readonly notices = inject(Notices);
  protected readonly calendar = inject(CalendarStore);
  protected readonly work = inject(WorkSessionStore);
  private readonly profile = inject(ProfileStore);
  private readonly settings = inject(SettingsStore);
  private readonly tasks = inject(TasksStore);
  private readonly wallet = inject(WalletStore);
  private readonly sleep = inject(SleepStore);
  private readonly viewing = inject(ViewingStore);
  private readonly router = inject(Router);

  protected readonly nav: readonly NavItem[] = [
    { path: '/calendar', icon: 'calendar_month', label: 'カレンダー' },
    { path: '/tasks', icon: 'assignment', label: '課題' },
    { path: '/records', icon: 'grass', label: '記録' },
    { path: '/about', icon: 'info', label: 'アプリについて' },
  ];

  /** 1分ごとに進む時計（日付の表示と画面の汚れ）。 */
  private readonly now = signal(Date.now());
  protected readonly today = computed(() => formatFullDate(this.now()));

  /** 画面に必要な store がそろったか。 */
  protected readonly ready = computed(
    () =>
      this.tasks.loaded() &&
      this.settings.loaded() &&
      this.wallet.loaded() &&
      this.profile.loaded(),
  );
  protected readonly loadError = computed(
    () =>
      this.tasks.load.error() ??
      this.settings.load.error() ??
      this.wallet.load.error() ??
      this.profile.load.error(),
  );

  private readonly url = toSignal(
    this.router.events.pipe(
      filter((event) => event instanceof NavigationEnd),
      map((event) => event.urlAfterRedirects),
    ),
    { initialValue: this.router.url },
  );

  protected readonly grime = computed(() => {
    const settings = this.settings.value();
    if (!settings?.grime || this.work.session() || !this.url().startsWith('/calendar')) return 0;
    return grimeOpacity(this.tasks.tasks(), this.now());
  });

  constructor() {
    const scope = inject(AccountScope);
    // アカウントが切り替わると store は空に戻るので、そのたびに読み込み直す。
    effect(() => {
      scope.epoch();
      if (!this.auth.signedIn()) return;
      untracked(() => {
        this.loadAll();
      });
    });
    // 「動きを控える」をページ全体のアニメーションに反映する（global styles の html.reduced-motion）。
    effect(() => {
      document.documentElement.classList.toggle(
        'reduced-motion',
        this.settings.value()?.reducedMotion === true,
      );
    });
    inject(MorningCheck);
    inject(LaunchParams);
    const timer = setInterval(() => {
      this.now.set(Date.now());
    }, 60_000);
    inject(DestroyRef).onDestroy(() => {
      clearInterval(timer);
    });
  }

  protected loadAll(): void {
    for (const store of [
      this.profile,
      this.settings,
      this.tasks,
      this.work,
      this.wallet,
      this.sleep,
      this.calendar,
      this.viewing,
    ]) {
      if (store.load.error()) void store.refresh();
      else store.ensureLoaded();
    }
  }
}
