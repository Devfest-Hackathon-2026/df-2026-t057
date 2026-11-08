import { ChangeDetectionStrategy, Component, inject, signal } from '@angular/core';
import { MatButtonModule } from '@angular/material/button';
import type { components } from '@dopagaki/contract';
import { AccountScope } from '../../core/auth/account-scope';
import { API_CLIENT, ifMatch, requireData, requireOk } from '../../core/http/api-client';
import { Operation } from '../../core/http/operation';
import { LocaleStore } from '../../core/i18n/locale.store';
import { DateTimePipe } from '../../shared/ui/format';
import { CalendarStore } from './calendar.store';
@Component({
  selector: 'app-calendar-conflicts',
  imports: [MatButtonModule, DateTimePipe],
  template: `
    @let t = text().calendar.conflicts;
    <h3>{{ t.heading }}</h3>
    <button mat-button type="button" (click)="load()" [disabled]="operation.pending()">
      {{ t.load }}
    </button>
    <button mat-button type="button" (click)="retry()" [disabled]="operation.pending()">
      {{ t.retry }}
    </button>
    @if (calendar.value()?.sync === 'reauth_required') {
      <p>{{ t.reconnect }}</p>
    }
    @for (c of conflicts(); track c.task.id) {
      <article>
        <h4>{{ c.task.title }}</h4>
        <p>{{ t.appSide(c.task.deadline | dateTime, c.task.notes) }}</p>
        <p>
          {{
            t.googleSide(
              c.deleted ? t.googleDeleted : c.googleTitle,
              c.googleDeadline ? (c.googleDeadline | dateTime) : '',
              c.googleNotes
            )
          }}
        </p>
        <button
          mat-stroked-button
          type="button"
          [disabled]="operation.pending() || c.pending"
          (click)="resolve(c, 'app')"
        >
          {{ t.useApp }}
        </button>
        <button
          mat-button
          type="button"
          [disabled]="operation.pending() || c.pending"
          (click)="resolve(c, 'google')"
        >
          {{ t.useGoogle }}
        </button>
        @if (c.pending) {
          <p>{{ t.pending }}</p>
        }
      </article>
    }
    @if (loaded() && conflicts().length === 0) {
      <p>{{ t.empty }}</p>
    }
    @if (operation.error(); as error) {
      <p role="alert">{{ error.message }}</p>
    }
  `,
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class CalendarConflicts {
  private readonly api = inject(API_CLIENT);
  protected readonly text = inject(LocaleStore).web;
  protected readonly calendar = inject(CalendarStore);
  protected readonly operation = new Operation(inject(AccountScope));
  protected readonly conflicts = signal<components['schemas']['CalendarConflict'][]>([]);
  protected readonly loaded = signal(false);
  async load(): Promise<void> {
    await this.operation.run(
      () => requireData(this.api.GET('/calendar/conflicts')),
      (data) => {
        this.conflicts.set(data);
        this.loaded.set(true);
      },
    );
  }
  async retry(): Promise<void> {
    await this.operation.run(() =>
      requireData(this.api.POST('/calendar/retry', { params: { header: ifMatch(0) } })),
    );
    await this.calendar.refresh();
    await this.load();
  }
  async resolve(
    c: components['schemas']['CalendarConflict'],
    choice: 'app' | 'google',
  ): Promise<void> {
    const result = await this.operation.run(() =>
      requireOk(
        this.api.POST('/calendar/conflicts/{taskId}/resolve', {
          params: { path: { taskId: c.task.id }, header: ifMatch(c.revision) },
          body: { choice, etag: c.etag },
        }),
      ),
    );
    if (result.ok) await this.load();
  }
}
