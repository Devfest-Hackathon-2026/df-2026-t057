import { Component, computed, inject, signal, viewChild } from '@angular/core';
import { Router } from '@angular/router';
import { FormsModule } from '@angular/forms';
import { MAT_DIALOG_DATA, MatDialogModule, MatDialogRef } from '@angular/material/dialog';
import { MatButtonModule } from '@angular/material/button';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatInputModule } from '@angular/material/input';
import { MatProgressBarModule } from '@angular/material/progress-bar';
import { MatButtonToggleModule } from '@angular/material/button-toggle';
import { Assistant } from './assistant';
import { TaskChange } from './task-change';
import { TaskEdit, TaskInput } from '../../../shared/models';
import { Api, localDate, message } from './api';

@Component({
  selector: 'task-dialog',
  imports: [
    Assistant,
    TaskChange,
    MatButtonToggleModule,
    FormsModule,
    MatDialogModule,
    MatButtonModule,
    MatFormFieldModule,
    MatInputModule,
    MatProgressBarModule,
  ],
  styles: `
    mat-button-toggle-group {
      margin-bottom: 16px;
    }
    .input-panel {
      padding-top: 8px;
    }
    .registration-reward {
      font-size: 11px;
      color: #626a5c;
      margin: 0 auto 0 0;
    }
    mat-dialog-actions {
      gap: 4px;
      row-gap: 12px;
    }
    @media (max-width: 550px) {
      .registration-reward {
        flex-basis: 100%;
      }
    }
  `,
  template: `
    <h2 mat-dialog-title>{{ data?.taskId ? '課題の内容を調整' : '課題を登録' }}</h2>
    @if (busy()) {
      <mat-progress-bar mode="indeterminate" aria-label="課題を準備中" />
    }
    <mat-dialog-content>
      @if (data?.taskId && !task()) {
        <p class="error" role="alert">
          この課題は見つかりません。閉じて、課題を選び直してください。
        </p>
      }
      <mat-button-toggle-group
        [value]="mode()"
        (change)="setMode($event.value)"
        aria-label="課題の入力方法"
        [disabled]="busy() || api.busy() || assistant()?.sending()"
      >
        <mat-button-toggle value="manual">手入力</mat-button-toggle>
        <mat-button-toggle value="ai">AIに相談</mat-button-toggle>
      </mat-button-toggle-group>
      <div [hidden]="mode() !== 'ai' || (data?.taskId && !task())" class="input-panel">
        @if (mode() === 'ai' || aiOpened()) {
          <app-assistant
            [selectedTask]="task()"
            [fixedTask]="true"
            (openSettings)="openSettings()"
          />
        }
      </div>
      <div [hidden]="mode() !== 'manual' || (data?.taskId && !task())" class="input-panel">
        <div class="form-stack">
          <mat-form-field appearance="outline">
            <mat-label>課題名</mat-label>
            <input
              matInput
              [disabled]="busy() || api.busy()"
              (ngModelChange)="change.set(null)"
              [(ngModel)]="form.title"
              maxlength="200"
              required
            />
          </mat-form-field>
          <div class="form-row">
            <mat-form-field appearance="outline">
              <mat-label>締切</mat-label>
              <input
                matInput
                type="datetime-local"
                [disabled]="busy() || api.busy()"
                (ngModelChange)="change.set(null)"
                [(ngModel)]="form.deadline"
                required
              />
            </mat-form-field>
            <mat-form-field appearance="outline">
              <mat-label>必要時間（分）</mat-label>
              <input
                matInput
                type="number"
                min="1"
                max="720"
                [disabled]="busy() || api.busy()"
                (ngModelChange)="change.set(null)"
                [(ngModel)]="form.minutes"
                required
              />
            </mat-form-field>
          </div>
          <details class="disclosure optional-fields" [open]="!!data?.taskId">
            <summary>詳細を追加</summary>
            <div class="form-stack">
              <mat-form-field appearance="outline">
                <mat-label>目的（任意）</mat-label>
                <input
                  matInput
                  [disabled]="busy() || api.busy()"
                  (ngModelChange)="change.set(null)"
                  [(ngModel)]="form.why"
                  maxlength="200"
                />
              </mat-form-field>
              <mat-form-field appearance="outline">
                <mat-label>作業リンク（任意）</mat-label>
                <input
                  matInput
                  type="url"
                  [disabled]="busy() || api.busy()"
                  (ngModelChange)="change.set(null)"
                  [(ngModel)]="form.url"
                  placeholder="https://docs.google.com/…"
                />
              </mat-form-field>
              <mat-form-field appearance="outline">
                <mat-label>やること・メモ</mat-label>
                <textarea
                  matInput
                  [disabled]="busy() || api.busy()"
                  (ngModelChange)="change.set(null)"
                  [(ngModel)]="form.notes"
                  rows="2"
                  maxlength="4000"
                ></textarea>
              </mat-form-field>
            </div>
          </details>
        </div>
        @if (change(); as candidate) {
          <app-task-change [change]="candidate" />
        }
      </div>
      @if (error()) {
        <p class="error" role="alert">{{ error() }}</p>
      }
    </mat-dialog-content>
    <mat-dialog-actions align="end">
      @if (!data?.taskId) {
        <p class="registration-reward">
          新しい課題の登録で視聴＋5分
          <br />
          残高の上限30分まで
          @if (api.state()?.sleep) {
            （夜の報酬は翌朝）
          }
        </p>
      }
      <button
        mat-button
        mat-dialog-close
        [disabled]="busy() || api.busy() || assistant()?.sending()"
      >
        {{ mode() === 'ai' ? '閉じる' : 'キャンセル' }}
      </button>
      @if (mode() === 'manual') {
        <button
          mat-flat-button
          (click)="save()"
          [disabled]="busy() || api.busy() || !valid() || (data?.taskId && !task())"
        >
          {{ busy() ? '処理中…' : data?.taskId ? '変更内容を確認' : '登録する' }}
        </button>
      }
    </mat-dialog-actions>
  `,
})
export class TaskDialog {
  readonly api = inject(Api);
  readonly data = inject<{ taskId?: string; mode?: 'ai' | 'manual' }>(MAT_DIALOG_DATA, {
    optional: true,
  });
  readonly task = computed(
    () => this.api.state()?.tasks.find((t) => t.id === this.data?.taskId) ?? null,
  );
  readonly mode = signal<'ai' | 'manual'>(this.data?.mode ?? 'manual');
  readonly aiOpened = signal(this.mode() === 'ai');
  readonly assistant = viewChild(Assistant);
  readonly change = signal<TaskEdit | null>(null);
  private readonly router = inject(Router);
  private readonly dialog = inject(MatDialogRef<TaskDialog>);
  busy = signal(false);
  error = signal('');
  form: TaskInput = {
    id: crypto.randomUUID(),
    title: '',
    notes: '',
    why: '',
    deadline: localDate(new Date(Date.now() + 86400_000).toISOString()),
    minutes: 30,
    url: '',
  };
  valid() {
    return (
      this.form.title.trim() &&
      this.form.deadline &&
      this.form.minutes >= 1 &&
      this.form.minutes <= 720
    );
  }
  constructor() {
    const task = this.task();
    if (task)
      this.form = {
        id: task.id,
        title: task.title,
        notes: task.notes,
        why: task.why ?? '',
        deadline: localDate(task.deadline),
        minutes: task.minutes,
        url: task.url,
      };
  }
  openSettings() {
    this.dialog.close();
    void this.router.navigateByUrl('/settings');
  }
  setMode(mode: 'ai' | 'manual') {
    this.mode.set(mode);
    if (mode === 'ai') this.aiOpened.set(true);
  }
  async save() {
    if (!this.valid() || this.busy()) return;
    this.busy.set(true);
    this.error.set('');
    try {
      const input = { ...this.form, deadline: new Date(this.form.deadline).toISOString() };
      if (this.data?.taskId) {
        this.change.set(
          await this.api.request<TaskEdit>(`/tasks/${input.id}/preview`, 'POST', input),
        );
        return;
      }
      this.api.state.set(await this.api.request('/tasks', 'POST', input));
      await this.api.refresh();
      this.dialog.close(true);
    } catch (e) {
      this.error.set(message(e));
    } finally {
      this.busy.set(false);
    }
  }
}
