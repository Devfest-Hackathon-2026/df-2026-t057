import { Component, computed, effect, inject, OnDestroy, signal } from '@angular/core';
import { DatePipe } from '@angular/common';
import { A11yModule } from '@angular/cdk/a11y';
import { Router, RouterLink, RouterLinkActive, RouterOutlet, NavigationEnd } from '@angular/router';
import { toSignal } from '@angular/core/rxjs-interop';
import { filter, map } from 'rxjs';
import { MatButtonModule } from '@angular/material/button';
import { MatProgressBarModule } from '@angular/material/progress-bar';
import { MatSnackBarModule } from '@angular/material/snack-bar';
import { Api, message } from './api';
import { AccountMenu } from './account-menu';
import { TaskActions } from './task-actions';
import { displayTime, elapsedSeconds, plannedSeconds, Task } from '../../../shared/models';

@Component({
  selector: 'app-root',
  imports: [
    DatePipe,
    A11yModule,
    MatButtonModule,
    MatProgressBarModule,
    MatSnackBarModule,
    AccountMenu,
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
  readonly active = computed(() =>
    this.api.state()?.tasks.find((t) => t.id === this.api.state()?.session?.taskId),
  );
  readonly elapsed = computed(() =>
    this.api.state()?.session ? elapsedSeconds(this.api.state()!.session!, this.now()) : 0,
  );
  readonly progress = computed(() =>
    Math.min(100, (this.elapsed() / (this.active() ? plannedSeconds(this.active()!) : 60)) * 100),
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
}
