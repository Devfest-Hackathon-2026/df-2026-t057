import { Component, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { DatePipe } from '@angular/common';
import { MatButtonModule } from '@angular/material/button';
import { MatSelectModule } from '@angular/material/select';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatInputModule } from '@angular/material/input';
import { Api, message, localDate } from './api';
import { State, TaskInput, TaskEdit } from '../../../shared/models';
@Component({
  selector: 'app-calendar-conflicts',
  imports: [
    FormsModule,
    DatePipe,
    MatButtonModule,
    MatSelectModule,
    MatFormFieldModule,
    MatInputModule,
  ],
  template: `
    @for (conflict of api.state()?.calendarConflicts ?? []; track conflict.taskId) {
      <section aria-label="Googleカレンダーとの編集競合">
        <h2>両方で編集された予定があります</h2>
        <p>
          アプリ：{{ title(conflict.taskId) }} ／ Google：{{ conflict.remote.title }}（締切
          {{ conflict.remote.deadline | date: 'M/d HH:mm' }}）
        </p>
        @if (selected() !== conflict.taskId) {
          <button mat-stroked-button type="button" (click)="open(conflict.taskId)">
            内容を比較して解決
          </button>
        }
        @if (selected() === conflict.taskId) {
          <mat-form-field>
            <mat-label>採用する内容</mat-label>
            <mat-select [(ngModel)]="choice" (ngModelChange)="preview.set(null)">
              <mat-option value="app">アプリの内容</mat-option>
              <mat-option value="google">Googleの内容</mat-option>
              <mat-option value="edit">編集して確定</mat-option>
            </mat-select>
          </mat-form-field>
          @if (choice === 'edit' && input) {
            <mat-form-field>
              <mat-label>課題名</mat-label>
              <input
                matInput
                [disabled]="pending()"
                [(ngModel)]="input.title"
                (ngModelChange)="preview.set(null)"
                maxlength="200"
              />
            </mat-form-field>
            <mat-form-field>
              <mat-label>メモ</mat-label>
              <textarea
                matInput
                [disabled]="pending()"
                [(ngModel)]="input.notes"
                (ngModelChange)="preview.set(null)"
                maxlength="4000"
              ></textarea>
            </mat-form-field>
            <mat-form-field>
              <mat-label>必要時間（分）</mat-label>
              <input
                matInput
                [disabled]="pending()"
                type="number"
                [(ngModel)]="input.minutes"
                (ngModelChange)="preview.set(null)"
                min="1"
                max="720"
              />
            </mat-form-field>
            <mat-form-field>
              <mat-label>締切</mat-label>
              <input
                matInput
                [disabled]="pending()"
                type="datetime-local"
                [(ngModel)]="deadline"
                (ngModelChange)="preview.set(null)"
              />
            </mat-form-field>
          }
          <button mat-button type="button" (click)="resolve(false)" [disabled]="pending()">
            変更後を確認
          </button>
          @if (preview(); as proposal) {
            <p>
              変更後：{{ proposal.input.title }} ／ {{ proposal.input.minutes }}分 ／ 締切
              {{ proposal.input.deadline | date: 'M/d HH:mm' }}
            </p>
            @for (change of proposal.draft.changes; track change.taskId) {
              <p>
                {{ change.title }}：{{ change.before.start | date: 'M/d HH:mm' }} →
                {{ change.after.start | date: 'M/d HH:mm' }}
              </p>
            }
            <button mat-flat-button type="button" (click)="resolve(true)" [disabled]="pending()">
              この内容で確定
            </button>
          }
          <button mat-button type="button" (click)="selected.set('')" [disabled]="pending()">
            閉じる
          </button>
          @if (error()) {
            <p role="alert">{{ error() }}</p>
          }
        }
      </section>
    }
  `,
})
export class CalendarConflicts {
  readonly api = inject(Api);
  readonly selected = signal('');
  readonly preview = signal<TaskEdit | null>(null);
  readonly pending = signal(false);
  readonly error = signal('');
  private previewETag = '';
  private resolutionID = '';
  choice = 'app';
  input?: TaskInput;
  deadline = '';
  title(id: string) {
    return this.api.state()?.tasks.find((t) => t.id === id)?.title ?? '課題';
  }
  open(id: string) {
    this.selected.set(id);
    this.preview.set(null);
    this.error.set('');
    this.choice = 'app';
    this.previewETag = '';
    this.resolutionID = '';
    const t = this.api.state()?.tasks.find((t) => t.id === id);
    if (t) {
      this.input = {
        id: t.id,
        title: t.title,
        notes: t.notes,
        why: t.why,
        url: t.url,
        minutes: t.minutes,
        deadline: t.deadline,
      };
      this.deadline = localDate(t.deadline);
    }
  }
  async resolve(commit: boolean) {
    if (this.pending() || (commit && !this.preview())) return;
    const state = this.api.state()!;
    const conflict = state.calendarConflicts?.find((c) => c.taskId === this.selected());
    if (!conflict) return;
    this.pending.set(true);
    this.error.set('');
    try {
      const payload = {
        choice: this.choice,
        etag: commit ? this.previewETag : conflict.etag,
        id: commit ? this.resolutionID : crypto.randomUUID(),
        revision: commit ? this.preview()!.draft.baseRevision : state.planRevision,
        ...(this.choice === 'edit'
          ? { input: { ...this.input, deadline: new Date(this.deadline).toISOString() } }
          : {}),
      };
      const path = '/tasks/' + encodeURIComponent(conflict.taskId) + '/calendar-resolution';
      if (commit) {
        this.api.state.set(await this.api.request<State>(path, 'POST', payload));
        this.selected.set('');
        this.preview.set(null);
      } else {
        const proposal = await this.api.request<TaskEdit>(path + '?preview=true', 'POST', payload);
        this.previewETag = conflict.etag;
        this.resolutionID = payload.id;
        this.preview.set(proposal);
      }
    } catch (e) {
      this.error.set(message(e));
      if (e instanceof Error && e.cause === 409) {
        this.preview.set(null);
        await this.api.load();
      }
    } finally {
      this.pending.set(false);
    }
  }
}
