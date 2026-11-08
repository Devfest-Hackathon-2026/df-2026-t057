import { ChangeDetectionStrategy, Component, inject, signal } from '@angular/core';
import { MatButtonModule } from '@angular/material/button';
import type { components } from '@dopagaki/contract';
import { AccountScope } from '../../core/auth/account-scope';
import { API_CLIENT, ifMatch, requireData, requireOk } from '../../core/http/api-client';
import { Operation } from '../../core/http/operation';
import { DateTimePipe } from '../../shared/ui/format';
import { CalendarStore } from './calendar.store';
@Component({
  selector: 'app-calendar-conflicts',
  imports: [MatButtonModule, DateTimePipe],
  template: `
    <h3>Google同期の復旧</h3>
    <button mat-button type="button" (click)="load()" [disabled]="operation.pending()">
      競合を確認
    </button>
    <button mat-button type="button" (click)="retry()" [disabled]="operation.pending()">
      同期を再試行
    </button>
    @if (calendar.value()?.sync === 'reauth_required') {
      <p>同じGoogleアカウントでカレンダーを連携し直してください。</p>
    }
    @for (c of conflicts(); track c.task.id) {
      <article>
        <h4>{{ c.task.title }}</h4>
        <p>DOPAGAKI：{{ c.task.deadline | dateTime }} · {{ c.task.notes }}</p>
        <p>
          Google：{{ c.deleted ? '予定が削除されています' : c.googleTitle }} ·
          @if (c.googleDeadline; as end) {
            {{ end | dateTime }}
          }
          · {{ c.googleNotes }}
        </p>
        <button
          mat-stroked-button
          type="button"
          [disabled]="operation.pending() || c.pending"
          (click)="resolve(c, 'app')"
        >
          DOPAGAKIを採用
        </button>
        <button
          mat-button
          type="button"
          [disabled]="operation.pending() || c.pending"
          (click)="resolve(c, 'google')"
        >
          Googleを採用
        </button>
        @if (c.pending) {
          <p>反映待ちです。もう一度確認してください。</p>
        }
      </article>
    }
    @if (loaded() && conflicts().length === 0) {
      <p>解消待ちの競合はありません。</p>
    }
    @if (operation.error(); as error) {
      <p role="alert">{{ error.message }}</p>
    }
  `,
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class CalendarConflicts {
  private readonly api = inject(API_CLIENT);
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
