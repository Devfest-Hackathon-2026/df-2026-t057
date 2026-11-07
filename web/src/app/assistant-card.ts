import { Component, computed, effect, inject, input, signal } from '@angular/core';
import { DatePipe } from '@angular/common';
import { FormsModule } from '@angular/forms';
import { MatButtonModule } from '@angular/material/button';
import { MatCardModule } from '@angular/material/card';
import { MatDialog } from '@angular/material/dialog';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatInputModule } from '@angular/material/input';
import { MatProgressBarModule } from '@angular/material/progress-bar';
import { AssistantCard as Card, Draft, Plan, TaskInput } from '../../../shared/models';
import { Api, localDate, message } from './api';
import { PlanDialog } from './plan-dialog';

@Component({
  selector: 'app-assistant-card',
  imports: [
    DatePipe,
    FormsModule,
    MatButtonModule,
    MatCardModule,
    MatFormFieldModule,
    MatInputModule,
    MatProgressBarModule,
  ],
  templateUrl: './assistant-card.html',
  styleUrl: './assistant-card.scss',
})
export class AssistantCard {
  readonly api = inject(Api);
  private readonly dialog = inject(MatDialog);
  readonly card = input.required<Card>();
  readonly busy = signal(false);
  readonly error = signal('');
  readonly editing = signal(false);
  readonly preview = signal<{ plan: Plan; revision: number } | null>(null);
  readonly task = computed(() =>
    this.api.state()?.tasks.find((t) => t.id === (this.card().taskId || this.card().input?.id)),
  );
  readonly registered = computed(
    () =>
      !!this.task() ||
      !!this.api
        .state()
        ?.ledger.some((e) => e.kind === 'registration' && e.taskId === this.card().input?.id),
  );
  readonly remembered = computed(() => !!this.api.state()?.memoryReceipts?.[this.card().id]);
  readonly sleepSet = computed(() => {
    const sleep = this.api.state()?.sleep;
    const proposed = this.card().sleep;
    return !!sleep && sleep.bedtime === proposed?.bedtime && sleep.wake === proposed?.wake;
  });
  readonly stale = computed(() => this.preview()?.revision !== this.api.state()?.planRevision);
  form: TaskInput = { id: '', title: '', notes: '', deadline: '', minutes: 30, url: '' };
  memoryText = '';
  private initialized = '';
  private confirmed!: TaskInput;
  local = (value: string) => (value ? localDate(value) : '');

  constructor() {
    effect(() => {
      const card = this.card();
      if (this.initialized === card.id) return;
      this.initialized = card.id;
      if (card.input) this.form = structuredClone(card.input);
      this.confirmed = structuredClone(this.form);
      this.memoryText = card.memory ?? '';
      if (card.preview) this.preview.set({ plan: card.preview, revision: card.revision! });
    });
  }
  edit() {
    this.form = structuredClone(this.confirmed);
    this.editing.set(true);
    this.error.set('');
  }
  deadline(value: string) {
    this.form.deadline = value ? new Date(value).toISOString() : '';
  }
  cancelEdit() {
    this.form = structuredClone(this.confirmed);
    this.editing.set(false);
    this.error.set('');
  }
  async check() {
    if (this.busy() || !this.form.title.trim() || !this.form.deadline) return;
    this.busy.set(true);
    this.error.set('');
    const candidate = structuredClone(this.form);
    try {
      this.preview.set(
        await this.api.request<{ plan: Plan; revision: number }>(
          '/assistant/preview',
          'POST',
          candidate,
        ),
      );
      this.confirmed = candidate;
      this.editing.set(false);
    } catch (e) {
      this.error.set(message(e));
    } finally {
      this.busy.set(false);
    }
  }
  async register() {
    if (this.busy() || this.registered() || this.stale()) return;
    this.busy.set(true);
    this.error.set('');
    try {
      const ok = await this.api.mutate('/tasks', {
        ...this.confirmed,
        baseRevision: this.preview()!.revision,
      });
      if (!ok) {
        this.error.set(this.api.error());
        await this.api.refresh();
      }
    } catch (e) {
      this.error.set(message(e));
    } finally {
      this.busy.set(false);
    }
  }
  async adjust() {
    const task = this.task();
    if (!task || this.busy() || task.status !== 'pending') return;
    this.busy.set(true);
    this.error.set('');
    try {
      let draft: Draft | undefined;
      const card = this.card();
      if (
        card.kind === 'task' &&
        card.preview &&
        card.revision === this.api.state()?.planRevision
      ) {
        draft = await this.api.request<Draft>(
          `/tasks/${task.id}/draft?adjust=true&start=${encodeURIComponent(card.preview.start)}&revision=${card.revision}`,
        );
      }
      this.dialog.open(PlanDialog, {
        width: '700px',
        maxWidth: '96vw',
        disableClose: true,
        data: { taskId: task.id, draft, adjust: true },
      });
    } catch (e) {
      this.error.set(message(e));
      if (e instanceof Error && e.cause === 409) await this.api.refresh();
    } finally {
      this.busy.set(false);
    }
  }
  async setSleep() {
    if (this.busy() || this.sleepSet() || !this.card().sleep) return;
    this.busy.set(true);
    this.error.set('');
    if (!(await this.api.mutate('/sleep', this.card().sleep, 'PUT')))
      this.error.set(this.api.error());
    this.busy.set(false);
  }
  async remember() {
    if (this.busy() || this.remembered() || !this.memoryText.trim()) return;
    this.busy.set(true);
    this.error.set('');
    if (
      !(await this.api.mutate('/assistant/memory', { id: this.card().id, text: this.memoryText }))
    )
      this.error.set(this.api.error());
    this.busy.set(false);
  }
}
