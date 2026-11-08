import { Injectable, effect, inject, untracked } from '@angular/core';
import { MatSnackBar } from '@angular/material/snack-bar';
import { Router } from '@angular/router';
import { AuthService } from '../../core/auth/auth.service';
import { LocaleStore } from '../../core/i18n/locale.store';
import { CalendarStore } from '../calendar/calendar.store';
import { Tutorial } from '../onboarding/tutorial.service';
import { TaskActions } from '../tasks/task-actions';
import { TasksStore } from '../tasks/tasks.store';
import { Notices } from './notices';

/** 開いたときのクエリ（ページの読み込みごとに一度だけ読む）。 */
export interface LaunchQuery {
  /** 通知の「スヌーズ」から開いた課題。スヌーズ後の予定の確認を開く。 */
  readonly snooze: string | undefined;
  /** 通知から開いた課題。通知プレビューを開く。 */
  readonly notification: string | undefined;
  readonly tutorial: boolean;
  readonly calendar: 'connected' | 'error' | undefined;
}

export function parseLaunchQuery(search: string): LaunchQuery {
  const params = new URLSearchParams(search);
  const calendar = params.get('calendar');
  return {
    snooze: params.get('snooze') ?? undefined,
    notification: params.get('notification') ?? undefined,
    tutorial: params.has('tutorial'),
    calendar: calendar === 'connected' || calendar === 'error' ? calendar : undefined,
  };
}

/**
 * `?snooze=` `?notification=` `?tutorial` `?calendar=` を処理する。
 * 処理したクエリは URL から消し、再読み込みで同じ操作を繰り返さない。
 */
@Injectable({ providedIn: 'root' })
export class LaunchParams {
  private readonly auth = inject(AuthService);
  private readonly tasks = inject(TasksStore);
  private readonly calendar = inject(CalendarStore);
  private readonly actions = inject(TaskActions);
  private readonly tutorial = inject(Tutorial);
  private readonly notices = inject(Notices);
  private readonly snackBar = inject(MatSnackBar);
  private readonly router = inject(Router);
  private readonly text = inject(LocaleStore).web;
  private readonly query = parseLaunchQuery(location.search);
  private handledTask = false;
  private handledTutorial = false;

  constructor() {
    const text = this.text().shell;
    if (this.query.calendar === 'error') this.notices.push(text.launch.calendarError);
    if (this.query.calendar === 'connected') {
      this.snackBar.open(text.launch.calendarConnected, text.common.close);
      void this.calendar.refresh();
    }
    effect(() => {
      if (!this.tasks.loaded()) return;
      untracked(() => {
        this.handleTask();
        this.handleTutorial();
      });
    });
  }

  private handleTask(): void {
    if (this.handledTask) return;
    this.handledTask = true;
    const snooze = this.query.snooze ? this.tasks.find(this.query.snooze) : undefined;
    const notification = this.query.notification
      ? this.tasks.find(this.query.notification)
      : undefined;
    if (snooze) void this.actions.plan(snooze, 'snooze');
    else if (notification) void this.actions.preview(notification);
    this.strip(['snooze', 'notification', 'calendar']);
  }

  private handleTutorial(): void {
    if (this.handledTutorial) return;
    this.handledTutorial = true;
    const fromNotice = this.query.snooze !== undefined || this.query.notification !== undefined;
    if (this.query.tutorial) {
      this.strip(['tutorial']);
      this.tutorial.start();
    } else if (this.auth.mode() === 'live' && !fromNotice && !this.tutorial.seen()) {
      this.tutorial.start();
    }
  }

  private strip(keys: readonly (keyof LaunchQuery)[]): void {
    const params = new URLSearchParams(location.search);
    if (!keys.some((key) => params.has(key))) return;
    const queryParams = Object.fromEntries(keys.map((key) => [key, null]));
    void this.router.navigate([], { queryParams, queryParamsHandling: 'merge', replaceUrl: true });
  }
}
