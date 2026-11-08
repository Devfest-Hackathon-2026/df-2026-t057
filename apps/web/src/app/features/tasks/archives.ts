import { ChangeDetectionStrategy, Component, inject, signal } from '@angular/core';
import { MatButtonModule } from '@angular/material/button';
import type { Task } from '@dopagaki/contract';
import { AccountScope } from '../../core/auth/account-scope';
import { API_CLIENT, idempotent, requireData, requireOk } from '../../core/http/api-client';
import { Operation } from '../../core/http/operation';
import { LocaleStore } from '../../core/i18n/locale.store';
import { TasksStore } from './tasks.store';
@Component({
  selector: 'app-archives',
  imports: [MatButtonModule],
  template: `
    @let archives = text().tasks.archives;
    <h2>{{ archives.heading }}</h2>
    <button mat-button type="button" (click)="load(false)" [disabled]="operation.pending()">
      {{ archives.show }}
    </button>
    @for (t of items(); track t.id) {
      <p>
        {{ t.title }}
        <button mat-button type="button" (click)="restore(t)" [disabled]="operation.pending()">
          {{ archives.restore }}
        </button>
      </p>
    }
    @if (loaded() && items().length === 0) {
      <p>{{ archives.empty }}</p>
    }
    @if (cursor()) {
      <button mat-button type="button" (click)="load(true)" [disabled]="operation.pending()">
        {{ archives.more }}
      </button>
    }
    @if (operation.error(); as error) {
      <p role="alert">{{ error.message }}</p>
    }
  `,
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class Archives {
  private readonly api = inject(API_CLIENT);
  private readonly tasks = inject(TasksStore);
  protected readonly text = inject(LocaleStore).web;
  protected readonly operation = new Operation(inject(AccountScope));
  protected readonly items = signal<Task[]>([]);
  protected readonly cursor = signal<string | undefined>(undefined);
  readonly loaded = signal(false);
  async load(more: boolean): Promise<void> {
    await this.operation.run(
      () =>
        requireData(
          this.api.GET('/tasks/archived', {
            params: { query: more && this.cursor() ? { cursor: this.cursor() ?? '' } : {} },
          }),
        ),
      (data) => {
        this.items.set(more ? [...this.items(), ...data.items] : data.items);
        this.cursor.set(data.cursor);
        this.loaded.set(true);
      },
    );
  }
  async restore(t: Task): Promise<void> {
    const result = await this.operation.run(() =>
      requireOk(
        this.api.POST('/tasks/{taskId}/restore', {
          params: { path: { taskId: t.id }, header: idempotent() },
        }),
      ),
    );
    if (result.ok) {
      await this.tasks.refresh();
      await this.load(false);
    }
  }
}
