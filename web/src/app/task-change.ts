import { Component, computed, inject, input, signal } from '@angular/core';
import { DatePipe } from '@angular/common';
import { MatButtonModule } from '@angular/material/button';
import { MatProgressBarModule } from '@angular/material/progress-bar';
import { TaskEdit } from '../../../shared/models';
import { Api, message } from './api';

@Component({
  selector: 'app-task-change',
  imports: [DatePipe, MatButtonModule, MatProgressBarModule],
  template: `
    @if (busy()) {
      <mat-progress-bar mode="indeterminate" aria-label="課題の変更を処理中" />
    }
    @if (saved()) {
      <p role="status">課題の変更を保存しました。</p>
    } @else {
      @if (candidate(); as change) {
        <h3>課題の変更案</h3>
        <p class="muted">確認するまで保存しません。編集による視聴時間の追加はありません。</p>
        @if (current(); as task) {
          <p>
            変更前：{{ task.title }} · 締切 {{ task.deadline | date: 'M/d HH:mm' }} ·
            {{ task.minutes }}分
          </p>
          <p class="notes">メモ：{{ task.notes || 'なし' }}</p>
          <p>なぜ：{{ task.why || '未設定' }}</p>
          <p class="notes">作業リンク：{{ task.url || '未設定' }}</p>
        }
        <h4>{{ change.input.title }}</h4>
        <p>締切 {{ change.input.deadline | date: 'M/d HH:mm' }} · {{ change.input.minutes }}分</p>
        <p class="notes">メモ：{{ change.input.notes || 'なし' }}</p>
        <p>なぜ：{{ change.input.why || '未設定' }}</p>
        <p class="notes">作業リンク：{{ change.input.url || '未設定' }}</p>
        <details>
          <summary>変更後の時間割（{{ change.draft.changes.length }}件）</summary>
          @for (item of change.draft.changes; track item.taskId) {
            <p>
              <strong>{{ item.title }}</strong>
              <br />
              変更前 {{ item.before.start | date: 'M/d HH:mm' }}–{{
                item.before.end | date: 'HH:mm'
              }}
              <br />
              変更後 {{ item.after.start | date: 'M/d HH:mm' }}–{{ item.after.end | date: 'HH:mm' }}
            </p>
          }
        </details>
        <p class="muted">保存時に、変更した内容に合わせて作業手順を具体化します。</p>
        @if (stale()) {
          <p class="muted">予定が更新されています。最新の時間割で確認してください。</p>
          <button mat-stroked-button (click)="refresh()" [disabled]="busy() || api.busy()">
            最新の予定で再確認
          </button>
        } @else {
          <button mat-flat-button (click)="save()" [disabled]="busy() || api.busy()">
            この内容に変更する
          </button>
        }
      }
    }
    @if (error()) {
      <p class="error" role="alert">{{ error() }}</p>
    }
  `,
  styles: `
    .notes {
      white-space: pre-wrap;
      overflow-wrap: anywhere;
    }
    h4 {
      margin-bottom: 8px;
    }
    details {
      margin: 16px 0;
    }
    summary {
      cursor: pointer;
    }
  `,
})
export class TaskChange {
  readonly api = inject(Api);
  readonly change = input.required<TaskEdit>();
  readonly updated = signal<TaskEdit | null>(null);
  readonly candidate = computed(() => this.updated() ?? this.change());
  readonly current = computed(() =>
    this.api.state()?.tasks.find((t) => t.id === this.candidate().input.id),
  );
  readonly stale = computed(
    () => this.candidate().draft.baseRevision !== this.api.state()?.planRevision,
  );
  readonly busy = signal(false);
  readonly error = signal('');
  readonly saved = signal(false);
  async refresh() {
    if (this.busy()) return;
    this.busy.set(true);
    this.error.set('');
    try {
      const { input } = this.candidate();
      await this.api.refresh();
      this.updated.set(
        await this.api.request<TaskEdit>(`/tasks/${input.id}/preview`, 'POST', input),
      );
    } catch (e) {
      this.error.set(message(e));
    } finally {
      this.busy.set(false);
    }
  }
  async save() {
    if (this.busy() || this.stale()) return;
    this.busy.set(true);
    this.error.set('');
    const change = this.candidate();
    try {
      const ok = await this.api.mutate(`/tasks/${change.input.id}`, change, 'PUT');
      if (ok) this.saved.set(true);
      else {
        const failure = this.api.error();
        await this.api.refresh();
        this.error.set(
          this.api.state()?.calendarSync === 'pending'
            ? '管理画面の変更は保存済みですが、Googleへの反映を確認できていません。カレンダーの「Googleと同期」で再試行してください。' +
                failure
            : failure,
        );
      }
    } catch (e) {
      this.error.set(message(e));
    } finally {
      this.busy.set(false);
    }
  }
}
