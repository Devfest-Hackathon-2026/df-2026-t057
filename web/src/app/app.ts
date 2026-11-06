import { Component, computed, effect, inject, OnDestroy, signal } from '@angular/core';
import { DatePipe } from '@angular/common';
import { A11yModule } from '@angular/cdk/a11y';
import { FormsModule } from '@angular/forms';
import { MatButtonModule } from '@angular/material/button';
import { MatDialog } from '@angular/material/dialog';
import { MatProgressBarModule } from '@angular/material/progress-bar';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatInputModule } from '@angular/material/input';
import { MatSlideToggleModule } from '@angular/material/slide-toggle';
import { MatSnackBar, MatSnackBarModule } from '@angular/material/snack-bar';
import { Api, message } from './api';
import { TaskDialog } from './task-dialog';
import { PlanDialog } from './plan-dialog';
import { AccountMenu } from './account-menu';
import {
  DEFAULT_SETTINGS,
  Profile,
  Settings,
  Task,
  displayTime,
  elapsedSeconds,
  plannedSeconds,
} from '../../../shared/models';

@Component({
  selector: 'app-root',
  imports: [
    DatePipe,
    A11yModule,
    FormsModule,
    MatButtonModule,
    MatProgressBarModule,
    MatFormFieldModule,
    MatInputModule,
    AccountMenu,
    MatSlideToggleModule,
    MatSnackBarModule,
  ],
  templateUrl: './app.html',
})
export class App implements OnDestroy {
  readonly api = inject(Api);
  private readonly dialog = inject(MatDialog);
  private readonly snack = inject(MatSnackBar);
  readonly page = signal('today');
  readonly now = signal(Date.now());
  readonly preview = signal<Task | null>(null);
  readonly pending = computed(() =>
    (this.api.state()?.tasks ?? [])
      .filter((t) => t.status !== 'completed')
      .sort((a, b) => Date.parse(a.plan.start) - Date.parse(b.plan.start)),
  );
  readonly upNext = computed(() => this.pending().find((t) => t.status === 'pending'));
  readonly completed = computed(() =>
    (this.api.state()?.tasks ?? []).filter((t) => t.status === 'completed'),
  );
  readonly active = computed(() =>
    this.api.state()?.tasks.find((t) => t.id === this.api.state()?.session?.taskId),
  );
  readonly elapsed = computed(() =>
    this.api.state()?.session ? elapsedSeconds(this.api.state()!.session!, this.now()) : 0,
  );
  readonly progress = computed(() =>
    Math.min(100, (this.elapsed() / (this.active() ? plannedSeconds(this.active()!) : 60)) * 100),
  );
  readonly cells = Array.from({ length: 84 }, (_, i) => i);
  readonly grimeOpacity = computed(() => {
    if (!this.api.state()?.settings.grime || this.api.state()?.session || this.page() !== 'today')
      return 0;
    const dates = this.completed().map((t) => Date.parse(t.completedAt!));
    if (!dates.length) return 0;
    const days = Math.floor((this.now() - Math.max(...dates)) / 86400000);
    return Math.min(0.15, Math.max(0, days - 1) * 0.025);
  });
  settings: Settings = { ...DEFAULT_SETTINGS };
  profile: Profile = { name: '', context: '', timeZone: 'Asia/Tokyo' };
  displayTime = displayTime;
  plannedMinutes(task: Task) {
    return Math.ceil(plannedSeconds(task) / 60);
  }
  private readonly clock = setInterval(() => this.now.set(Date.now()), 1000);
  private readonly poll = setInterval(() => {
    if (this.api.state() && !this.api.busy() && !this.dialog.openDialogs.length)
      void this.api.refresh().catch((e) => this.api.error.set(message(e)));
  }, 30000);

  constructor() {
    effect(() => {
      if (!this.api.state()) {
        this.dialog.closeAll();
        this.preview.set(null);
        this.page.set('today');
        this.settings = { ...DEFAULT_SETTINGS };
        this.profile = { name: '', context: '', timeZone: 'Asia/Tokyo' };
      }
    });
    const params = new URLSearchParams(location.search);
    const taskId = params.get('snooze') ?? params.get('notification');
    let notificationOpened = false;
    effect(() => {
      if (!taskId || notificationOpened) return;
      const task = this.api.state()?.tasks.find((task) => task.id === taskId);
      if (!task) return;
      notificationOpened = true;
      if (params.has('snooze')) this.plan(task);
      else this.preview.set(task);
    });
    void this.api.init().then(() => {
      if (new URLSearchParams(location.search).get('calendar') === 'error')
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
    if (page === 'settings' && this.api.state()) {
      this.settings = { ...this.api.state()!.settings };
      this.profile = { ...this.api.state()!.profile };
    }
    this.page.set(page);
  }
  addTask() {
    this.dialog.open(TaskDialog, { width: '640px', maxWidth: '96vw', disableClose: true });
  }
  plan(task: Task, adjust = false) {
    this.preview.set(null);
    const candidate = this.api.state()?.nextDraft;
    this.dialog.open(PlanDialog, {
      width: '700px',
      maxWidth: '96vw',
      disableClose: true,
      data: {
        taskId: task.id,
        draft: !adjust && candidate?.taskId === task.id ? candidate : undefined,
        adjust,
      },
    });
  }
  async start(task: Task) {
    if (await this.api.mutate(`/tasks/${task.id}/start`)) {
      this.preview.set(null);
      if (task.url) window.open(task.url, '_blank', 'noopener');
    }
  }
  async complete(task: Task) {
    if (await this.api.mutate(`/tasks/${task.id}/complete`))
      this.snack.open('課題完了！ 視聴時間 ＋20分', '閉じる', { duration: 5000 });
  }
  async saveSettings() {
    if (await this.api.mutate('/settings', this.settings, 'PUT'))
      this.snack.open('設定を保存しました', '閉じる', { duration: 3000 });
  }
  async saveProfile() {
    if (await this.api.mutate('/profile', this.profile, 'PUT'))
      this.snack.open('作業の傾向を保存しました', '閉じる', { duration: 3000 });
  }
  async connect() {
    await this.api.run(async () => {
      const result = await this.api.request<{ url: string }>('/calendar/connect', 'POST');
      location.assign(result.url);
    });
  }
  activity(index: number): number {
    const daysAgo = 83 - index;
    const date = new Date(this.now());
    date.setDate(date.getDate() - daysAgo);
    return this.completed().filter(
      (t) => t.completedAt && new Date(t.completedAt).toDateString() === date.toDateString(),
    ).length;
  }
  dayLabel(index: number): string {
    const d = new Date(this.now());
    d.setDate(d.getDate() - (83 - index));
    return `${d.toLocaleDateString('ja-JP')}: ${this.activity(index)}件完了`;
  }
  overrun(task: Task): boolean {
    return Date.parse(task.plan.start) < this.now() && task.status === 'pending';
  }
}
