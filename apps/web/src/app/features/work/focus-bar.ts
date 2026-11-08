import { ChangeDetectionStrategy, Component, computed, inject } from '@angular/core';
import { MatButtonModule } from '@angular/material/button';
import { MatProgressBarModule } from '@angular/material/progress-bar';
import { clockLabel } from '@dopagaki/messages';
import { LocaleStore } from '../../core/i18n/locale.store';
import { TaskActions } from '../tasks/task-actions';
import { TasksStore } from '../tasks/tasks.store';
import { WorkSessionStore } from './work-session.store';

/** 作業中に画面の上に出すタイマー。 */
@Component({
  selector: 'app-focus-bar',
  imports: [MatButtonModule, MatProgressBarModule],
  templateUrl: './focus-bar.html',
  styleUrl: './focus-bar.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class FocusBar {
  protected readonly work = inject(WorkSessionStore);
  protected readonly tasks = inject(TasksStore);
  protected readonly actions = inject(TaskActions);
  protected readonly text = inject(LocaleStore).web;

  protected readonly task = computed(() => {
    const session = this.work.session();
    return session ? this.tasks.find(session.taskId) : undefined;
  });
  protected readonly clock = computed(() => clockLabel(this.work.elapsedSeconds()));
  protected readonly busy = computed(
    () =>
      this.work.pause.pending() ||
      this.work.resume.pending() ||
      this.work.confirm.pending() ||
      this.tasks.complete.pending(),
  );
  protected readonly error = computed(
    () =>
      this.work.pause.error() ??
      this.work.resume.error() ??
      this.work.confirm.error() ??
      this.tasks.complete.error(),
  );

  constructor() {
    // タイマーは作業が始まるたびに作られる。前の作業で失敗した操作のエラーを持ち越さない。
    for (const operation of [
      this.work.pause,
      this.work.resume,
      this.work.confirm,
      this.tasks.complete,
    ])
      operation.clearError();
  }

  protected toggle(): void {
    if (this.work.session()?.running) void this.work.pauseWork();
    else void this.work.resumeWork();
  }

  protected complete(): void {
    const task = this.task();
    if (task) void this.actions.complete(task);
  }
}
