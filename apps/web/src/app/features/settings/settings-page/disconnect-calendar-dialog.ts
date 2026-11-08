import { ChangeDetectionStrategy, Component, inject } from '@angular/core';
import { MatButtonModule } from '@angular/material/button';
import { MatDialogModule, MatDialogRef } from '@angular/material/dialog';
import { LocaleStore } from '../../../core/i18n/locale.store';
import { CalendarStore } from '../../calendar/calendar.store';

/** Google カレンダーとの連携を解除する前の確認。 */
@Component({
  selector: 'app-disconnect-calendar-dialog',
  imports: [MatButtonModule, MatDialogModule],
  template: `
    @let t = text().settings.disconnectDialog;
    <h2 mat-dialog-title>{{ t.title }}</h2>
    <mat-dialog-content>
      <ul class="points">
        <li>{{ t.keepsTasks }}</li>
        <li>{{ t.unlinksEvents }}</li>
        <li>{{ t.canReconnect }}</li>
      </ul>
      @if (calendar.disconnect.error(); as error) {
        <p class="error" role="alert">{{ error.message }}</p>
      }
    </mat-dialog-content>
    <mat-dialog-actions align="end">
      <button mat-button type="button" mat-dialog-close [disabled]="calendar.disconnect.pending()">
        {{ t.cancel }}
      </button>
      <button
        mat-flat-button
        type="button"
        (click)="disconnect()"
        [disabled]="calendar.disconnect.pending()"
      >
        {{ calendar.disconnect.pending() ? t.disconnecting : t.disconnect }}
      </button>
    </mat-dialog-actions>
  `,
  styles: `
    .points {
      margin: 0;
      padding-inline-start: 1.25rem;

      li + li {
        margin-top: 0.25rem;
      }
    }

    .error {
      margin: 1rem 0 0;
      padding: 0.5rem 0.75rem;
      border-radius: var(--mat-sys-corner-small);
      background: var(--mat-sys-error-container);
      color: var(--mat-sys-on-error-container);
    }
  `,
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class DisconnectCalendarDialog {
  protected readonly calendar = inject(CalendarStore);
  protected readonly text = inject(LocaleStore).web;
  private readonly dialogRef =
    inject<MatDialogRef<DisconnectCalendarDialog, boolean>>(MatDialogRef);

  constructor() {
    // 前に開いたときの失敗を、確認し直す前から出さない。
    this.calendar.disconnect.clearError();
  }

  protected async disconnect(): Promise<void> {
    const outcome = await this.calendar.disconnectNow();
    if (outcome.ok) this.dialogRef.close(true);
  }
}
