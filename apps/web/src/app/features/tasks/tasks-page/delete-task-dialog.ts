import { ChangeDetectionStrategy, Component, inject } from '@angular/core';
import { MatButtonModule } from '@angular/material/button';
import { MAT_DIALOG_DATA, MatDialogModule, MatDialogRef } from '@angular/material/dialog';
import type { Task } from '@dopagaki/contract';
import { TasksStore } from '../tasks.store';

/** 課題を削除する前の確認。 */
@Component({
  selector: 'app-delete-task-dialog',
  imports: [MatButtonModule, MatDialogModule],
  template: `
    <h2 mat-dialog-title>課題を削除しますか？</h2>
    <mat-dialog-content>
      <p class="title">{{ task.title }}</p>
      <p>
        削除すると、未開始の課題の予定を組み直します。これまでに増えた視聴時間はそのまま残ります。
      </p>
      @if (tasks.remove.error(); as error) {
        <p class="error" role="alert">{{ error.message }}</p>
      }
    </mat-dialog-content>
    <mat-dialog-actions align="end">
      <button mat-button type="button" mat-dialog-close [disabled]="tasks.remove.pending()">
        キャンセル
      </button>
      <button mat-flat-button type="button" (click)="remove()" [disabled]="tasks.remove.pending()">
        {{ tasks.remove.pending() ? '削除しています…' : '削除する' }}
      </button>
    </mat-dialog-actions>
  `,
  styles: `
    .title {
      font: var(--mat-sys-title-medium);
      overflow-wrap: anywhere;
    }

    .error {
      padding: 0.5rem 0.75rem;
      border-radius: var(--mat-sys-corner-small);
      background: var(--mat-sys-error-container);
      color: var(--mat-sys-on-error-container);
    }
  `,
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class DeleteTaskDialog {
  protected readonly task = inject<Task>(MAT_DIALOG_DATA);
  protected readonly tasks = inject(TasksStore);
  private readonly dialogRef = inject<MatDialogRef<DeleteTaskDialog, boolean>>(MatDialogRef);

  protected async remove(): Promise<void> {
    const outcome = await this.tasks.deleteTask(this.task.id);
    if (outcome.ok) this.dialogRef.close(true);
  }
}
