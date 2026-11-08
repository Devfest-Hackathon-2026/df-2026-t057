import { ChangeDetectionStrategy, Component, inject } from '@angular/core';
import { MatButtonModule } from '@angular/material/button';
import { MAT_DIALOG_DATA, MatDialogModule, MatDialogRef } from '@angular/material/dialog';
import type { Task } from '@dopagaki/contract';
import { LocaleStore } from '../../../core/i18n/locale.store';
import { TasksStore } from '../tasks.store';

/** 課題を削除する前の確認。 */
@Component({
  selector: 'app-delete-task-dialog',
  imports: [MatButtonModule, MatDialogModule],
  template: `
    <h2 mat-dialog-title>{{ text().tasks.deleteDialog.title }}</h2>
    <mat-dialog-content>
      <p class="title">{{ task.title }}</p>
      <p>{{ text().tasks.deleteDialog.body }}</p>
      @if (tasks.remove.error(); as error) {
        <p class="error" role="alert">{{ error.message }}</p>
      }
    </mat-dialog-content>
    <mat-dialog-actions align="end">
      <button mat-button type="button" mat-dialog-close [disabled]="tasks.remove.pending()">
        {{ text().tasks.common.cancel }}
      </button>
      <button mat-flat-button type="button" (click)="remove()" [disabled]="tasks.remove.pending()">
        {{
          tasks.remove.pending()
            ? text().tasks.deleteDialog.deleting
            : text().tasks.deleteDialog.delete
        }}
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
  protected readonly text = inject(LocaleStore).web;
  private readonly dialogRef = inject<MatDialogRef<DeleteTaskDialog, boolean>>(MatDialogRef);

  protected async remove(): Promise<void> {
    const outcome = await this.tasks.deleteTask(this.task.id);
    if (outcome.ok) this.dialogRef.close(true);
  }
}
