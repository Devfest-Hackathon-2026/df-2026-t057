import { Injectable, inject, signal } from '@angular/core';
import { MatDialog, type MatDialogRef } from '@angular/material/dialog';
import { MatSnackBar } from '@angular/material/snack-bar';
import type { Draft, Task } from '@dopagaki/contract';
import { firstValueFrom } from 'rxjs';
import type { OperationError } from '../../core/http/operation';
import { WalletStore } from '../wallet/wallet.store';
import { workRecordedMessage } from '../work/work-messages';
import { WorkSessionStore } from '../work/work-session.store';
import type { NotificationPreviewData } from '../notification/notification-preview.model';
import type { PlanDialogData } from './plan-dialog/plan-dialog.model';
import type { TaskDialogData } from './task-dialog/task-dialog.model';
import { TasksStore } from './tasks.store';

const wideDialog = { maxWidth: 'calc(100vw - 2rem)', autoFocus: 'first-heading' } as const;

/**
 * 課題の操作の入口。ダイアログを開き、store の操作を呼び、結果をスナックバーで知らせる。
 * 通信状態とエラーは store の操作ごとに持つ。ここで失敗を捨てずに、必ず知らせる。
 */
@Injectable({ providedIn: 'root' })
export class TaskActions {
  private readonly dialog = inject(MatDialog);
  private readonly snackBar = inject(MatSnackBar);
  private readonly tasks = inject(TasksStore);
  private readonly work = inject(WorkSessionStore);
  private readonly wallet = inject(WalletStore);
  private previewRef: MatDialogRef<unknown> | undefined;

  /** 通知プレビューに出している課題。 */
  readonly previewTaskId = signal<string | undefined>(undefined);

  /** 課題を登録する。登録したら true。 */
  async add(): Promise<boolean> {
    const { TaskDialog } = await import('./task-dialog/task-dialog');
    const data: TaskDialogData = { mode: 'manual' };
    const ref = this.dialog.open(TaskDialog, {
      ...wideDialog,
      width: '35rem',
      disableClose: true,
      data,
    });
    return (await firstValueFrom(ref.afterClosed())) === true;
  }

  /** 課題の内容を変更する。 */
  async edit(task: Task): Promise<void> {
    const { TaskDialog } = await import('./task-dialog/task-dialog');
    const data: TaskDialogData = { taskId: task.id, mode: 'ai' };
    this.dialog.open(TaskDialog, { ...wideDialog, width: '45rem', disableClose: true, data });
  }

  /**
   * 予定の変更案を確認する。
   * `snooze` は、通知と同じスヌーズ案（あらかじめ計算された `nextSnooze`）があればそれを使う。
   */
  async plan(task: Task, mode: 'snooze' | 'adjust', draft?: Draft): Promise<boolean> {
    this.closePreview();
    const { PlanDialog } = await import('./plan-dialog/plan-dialog');
    const next = this.tasks.nextSnooze();
    const prepared = draft ?? (mode === 'snooze' && next?.taskId === task.id ? next : undefined);
    const data: PlanDialogData =
      prepared === undefined
        ? { taskId: task.id, mode }
        : { taskId: task.id, mode, draft: prepared };
    const ref = this.dialog.open(PlanDialog, {
      ...wideDialog,
      width: '44rem',
      disableClose: true,
      data,
    });
    return (await firstValueFrom(ref.afterClosed())) === true;
  }

  /**
   * 作業を開始する。作業リンクがあれば新しいタブで開く。
   * 失敗は `tasks.start.error` に残り、押したボタンの近くに出す（スナックバーでは知らせない）。
   */
  async start(task: Task): Promise<boolean> {
    const outcome = await this.tasks.startTask(task.id);
    if (!outcome.ok) return false;
    this.closePreview();
    if (task.url) window.open(task.url, '_blank', 'noopener');
    return true;
  }

  async complete(task: Task): Promise<boolean> {
    const outcome = await this.tasks.completeTask(task.id);
    if (!outcome.ok) {
      this.notifyError(outcome.error);
      return false;
    }
    this.snackBar.open(workRecordedMessage(outcome.value.work), '閉じる', { duration: 5000 });
    return true;
  }

  /** ここまでの作業を記録する。 */
  async confirmWork(): Promise<boolean> {
    const outcome = await this.work.confirmWork();
    if (!outcome.ok) {
      this.notifyError(outcome.error, '作業を保存できませんでした。');
      return false;
    }
    void this.tasks.refresh();
    void this.wallet.refresh();
    this.snackBar.open(workRecordedMessage(outcome.value), '閉じる', { duration: 5000 });
    return true;
  }

  /** 通知プレビューを開く。 */
  async preview(task: Task): Promise<void> {
    this.closePreview();
    const { NotificationPreview } = await import('../notification/notification-preview');
    const data: NotificationPreviewData = { taskId: task.id };
    this.previewTaskId.set(task.id);
    const ref = this.dialog.open(NotificationPreview, {
      data,
      width: '100vw',
      maxWidth: '100vw',
      height: '100dvh',
      panelClass: 'notice-pane',
      ariaLabel: '通知プレビュー',
    });
    this.previewRef = ref;
    ref.afterClosed().subscribe(() => {
      if (this.previewRef === ref) {
        this.previewRef = undefined;
        this.previewTaskId.set(undefined);
      }
    });
  }

  closePreview(): void {
    this.previewRef?.close();
    this.previewRef = undefined;
    this.previewTaskId.set(undefined);
  }

  /** 操作の失敗を知らせる。 */
  notifyError(error: OperationError, prefix?: string): void {
    this.snackBar.open(prefix ? `${prefix}${error.message}` : error.message, '閉じる', {
      duration: 8000,
      politeness: 'assertive',
    });
  }
}
