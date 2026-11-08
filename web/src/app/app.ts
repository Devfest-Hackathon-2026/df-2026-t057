import { CalendarConflicts } from './calendar-conflicts';
import { Component, computed, effect, inject, OnDestroy, signal } from '@angular/core';
import { DatePipe } from '@angular/common';
import { A11yModule } from '@angular/cdk/a11y';
import { Router, RouterLink, RouterLinkActive, RouterOutlet, NavigationEnd } from '@angular/router';
import { toSignal } from '@angular/core/rxjs-interop';
import { filter, map } from 'rxjs';
import { MatButtonModule } from '@angular/material/button';
import { MatTooltipModule } from '@angular/material/tooltip';
import { MatProgressBarModule } from '@angular/material/progress-bar';
import { MatSnackBar, MatSnackBarModule } from '@angular/material/snack-bar';
import { Api, message } from './api';
import { AccountMenu } from './account-menu';
import { LoginScreen } from './login-screen';
import { TaskActions } from './task-actions';
import {
  Morning,
  State,
  displayTime,
  elapsedSeconds,
  plannedSeconds,
  workLimitSeconds,
  rewardDescription,
  Task,
} from '../../../shared/models';

@Component({
  selector: 'app-root',
  imports: [
    CalendarConflicts,
    DatePipe,
    A11yModule,
    MatButtonModule,
    MatTooltipModule,
    MatProgressBarModule,
    MatSnackBarModule,
    AccountMenu,
    LoginScreen,
    RouterLink,
    RouterLinkActive,
    RouterOutlet,
  ],
  templateUrl: './app.html',
})
export class App implements OnDestroy {
  readonly api = inject(Api);
  readonly actions = inject(TaskActions);
  private readonly router = inject(Router);
  readonly page = toSignal(
    this.router.events.pipe(
      filter((e) => e instanceof NavigationEnd),
      map(() => this.router.url.split('?')[0].slice(1)),
    ),
    { initialValue: 'calendar' },
  );
  readonly now = signal(Date.now());
  readonly preview = this.actions.preview;
  readonly companionLine = signal('');
  private readonly snack = inject(MatSnackBar);
  private morningChecked = false;
  readonly active = computed(() =>
    this.api.state()?.tasks.find((t) => t.id === this.api.state()?.session?.taskId),
  );
  readonly elapsed = computed(() =>
    this.api.state()?.session ? elapsedSeconds(this.api.state()!.session!, this.now()) : 0,
  );
  readonly progress = computed(() =>
    Math.min(
      100,
      (this.elapsed() /
        (this.active() ? workLimitSeconds(this.api.state()!, this.active()!) : 60)) *
        100,
    ),
  );
  readonly grimeOpacity = computed(() => {
    if (
      !this.api.state()?.settings.grime ||
      this.api.state()?.session ||
      this.page() !== 'calendar'
    )
      return 0;
    const dates = this.api
      .state()!
      .tasks.filter((t) => t.completedAt)
      .map((t) => Date.parse(t.completedAt!));
    if (!dates.length) return 0;
    return Math.min(
      0.15,
      Math.max(0, Math.floor((this.now() - Math.max(...dates)) / 86400000) - 1) * 0.025,
    );
  });
  displayTime = displayTime;
  rewardDescription = rewardDescription;
  workMinutes() {
    return this.active() ? Math.ceil(workLimitSeconds(this.api.state()!, this.active()!) / 60) : 0;
  }
  plannedMinutes(task: Task) {
    return Math.ceil(plannedSeconds(task) / 60);
  }
  private readonly clock = setInterval(() => this.now.set(Date.now()), 1000);
  private readonly poll = setInterval(() => {
    if (this.api.state() && !this.api.busy() && !this.actions.dialog.openDialogs.length)
      void this.api.refresh().catch((e) => this.api.error.set(message(e)));
  }, 30000);
  constructor() {
    effect(() => {
      const task = this.preview();
      this.companionLine.set('');
      if (!task) return;
      void this.api
        .request<{ line: string }>('/companion/line', 'POST', { moment: 'notice', taskId: task.id })
        .then((reply) => {
          if (this.preview()?.id === task.id) this.companionLine.set(reply.line);
        })
        .catch(() => undefined);
    });
    effect(() => {
      if (this.morningChecked || !this.api.state()?.sleep) return;
      this.morningChecked = true;
      void this.checkMorning();
    });
    effect(() => {
      if (!this.api.state()) {
        this.actions.dialog.closeAll();
        this.preview.set(null);
      }
    });
    const params = new URLSearchParams(location.search);
    const taskId = params.get('snooze') ?? params.get('notification');
    let notificationOpened = false;
    effect(() => {
      const task = this.api.state()?.tasks.find((t) => t.id === taskId);
      if (!task || notificationOpened) return;
      notificationOpened = true;
      if (params.has('snooze')) this.actions.plan(task);
      else this.preview.set(task);
    });
    void this.api.init().then(() => {
      if (
        this.api.config()?.mode === 'live' &&
        this.api.signedIn() &&
        this.api.state() &&
        !this.api.state()?.calendarConnected &&
        this.router.url.split('?')[0] === '/calendar'
      )
        void this.router.navigateByUrl('/setup');
      if (params.get('calendar') === 'error')
        this.api.error.set(
          'ログイン中と同じGoogleアカウントを選び、設定からカレンダーを連携し直してください。',
        );
    });
  }
  ngOnDestroy() {
    clearInterval(this.clock);
    clearInterval(this.poll);
  }
  navigate(page: string) {
    void this.router.navigateByUrl('/' + page);
  }
  // Once per load: settle last night and let the companion say good morning.
  private async checkMorning() {
    try {
      const result = await this.api.request<{ state: State; morning: Morning | null }>(
        '/sleep/morning',
        'POST',
      );
      this.api.state.set(result.state);
      const morning = result.morning;
      if (!morning) return;
      const { line } = await this.api.request<{ line: string }>('/companion/line', 'POST', {
        moment: 'morning',
      });
      const gains = [
        morning.kept ? `早寝ボーナス ＋${morning.bonusSeconds / 60}分` : '',
        morning.releasedSeconds ? `昨夜の報酬 ＋${morning.releasedSeconds / 60}分` : '',
      ].filter(Boolean);
      this.snack.open([line, ...gains].join(' ・ '), '閉じる', { duration: 8000 });
    } catch {
      /* The morning check-in is optional. */
    }
  }
}
