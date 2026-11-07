import { Injectable, inject, signal } from '@angular/core';
import { MatDialog } from '@angular/material/dialog';
import { MatSnackBar } from '@angular/material/snack-bar';
import { Api } from './api';
import { Task } from '../../../shared/models';
import { PlanDialog } from './plan-dialog';
import { TaskDialog } from './task-dialog';

@Injectable({ providedIn: 'root' })
export class TaskActions {
  readonly api = inject(Api);
  readonly dialog = inject(MatDialog);
  private readonly snack = inject(MatSnackBar);
  readonly preview = signal<Task | null>(null);

  add() {
    this.dialog.open(TaskDialog, { width: '640px', maxWidth: '96vw', disableClose: true });
  }
  edit(task: Task) {
    this.dialog.open(TaskDialog, {
      width: '720px',
      maxWidth: '96vw',
      disableClose: true,
      data: { taskId: task.id, mode: 'ai' },
    });
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
    if (!(await this.api.mutate(`/tasks/${task.id}/complete`))) return;
    const deferred = this.api
      .state()
      ?.ledger.some((e) => e.id === `completion:${task.id}` && e.deferred);
    this.snack.open(
      deferred ? '課題完了！ ＋20分は朝に受け取れます' : '課題完了！ 視聴時間 ＋20分',
      '閉じる',
      { duration: 5000 },
    );
  }
  async connect() {
    await this.api.run(async () => {
      const result = await this.api.request<{ url: string }>('/calendar/connect', 'POST');
      location.assign(result.url);
    });
  }
}
