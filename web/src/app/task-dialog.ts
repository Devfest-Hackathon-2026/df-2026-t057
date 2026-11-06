import { Component, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { MatDialogModule, MatDialogRef } from '@angular/material/dialog';
import { MatButtonModule } from '@angular/material/button';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatInputModule } from '@angular/material/input';
import { MatProgressBarModule } from '@angular/material/progress-bar';
import { Api, localDate, message } from './api';

interface Input {
  id: string;
  title: string;
  notes: string;
  why: string;
  deadline: string;
  minutes: number;
  url: string;
}
@Component({
  selector: 'task-dialog',
  imports: [
    FormsModule,
    MatDialogModule,
    MatButtonModule,
    MatFormFieldModule,
    MatInputModule,
    MatProgressBarModule,
  ],
  template: `
    <h2 mat-dialog-title>課題を登録</h2>
    @if (busy()) {
      <mat-progress-bar mode="indeterminate" aria-label="課題を準備中" />
    }
    <mat-dialog-content>
      <p class="muted">締切から逆算して予定を作ります。登録で視聴時間 ＋10分。</p>
      @if (api.config()?.aiEnabled) {
        <div class="ai-box">
          <mat-form-field appearance="outline">
            <mat-label>Geminiにまとめて入力</mat-label>
            <textarea
              matInput
              [(ngModel)]="text"
              placeholder="明日18時までにレポート、60分くらい"
              rows="2"
            ></textarea>
          </mat-form-field>
          <button mat-stroked-button (click)="parse()" [disabled]="busy() || !text.trim()">
            {{ busy() ? '内容を整理しています…' : '入力から課題を作る' }}
          </button>
        </div>
      }
      @for (item of items; track item.id) {
        <div class="form-stack">
          <mat-form-field appearance="outline">
            <mat-label>課題名</mat-label>
            <input matInput [(ngModel)]="item.title" maxlength="200" required />
          </mat-form-field>
          <div class="form-row">
            <mat-form-field appearance="outline">
              <mat-label>締切</mat-label>
              <input matInput type="datetime-local" [(ngModel)]="item.deadline" required />
            </mat-form-field>
            <mat-form-field appearance="outline">
              <mat-label>必要時間（分）</mat-label>
              <input matInput type="number" min="1" max="720" [(ngModel)]="item.minutes" required />
            </mat-form-field>
          </div>
          <mat-form-field appearance="outline">
            <mat-label>なぜやる？（任意・一行）</mat-label>
            <input
              matInput
              [(ngModel)]="item.why"
              maxlength="200"
              placeholder="来週のゼミで、自信をもって発表したいから"
            />
            <mat-hint>通知のときに表示します</mat-hint>
          </mat-form-field>
          <mat-form-field appearance="outline">
            <mat-label>作業リンク（任意）</mat-label>
            <input
              matInput
              type="url"
              [(ngModel)]="item.url"
              placeholder="https://docs.google.com/…"
            />
          </mat-form-field>
          <mat-form-field appearance="outline">
            <mat-label>やること・メモ</mat-label>
            <textarea matInput [(ngModel)]="item.notes" rows="2" maxlength="4000"></textarea>
          </mat-form-field>
        </div>
      }
      @if (error()) {
        <p class="error" role="alert">{{ error() }}</p>
      }
    </mat-dialog-content>
    <mat-dialog-actions align="end">
      <button mat-button mat-dialog-close [disabled]="busy()">キャンセル</button>
      <button mat-flat-button (click)="save()" [disabled]="busy() || !valid()">
        {{ busy() ? '処理中…' : '登録する' }}
      </button>
    </mat-dialog-actions>
  `,
})
export class TaskDialog {
  readonly api = inject(Api);
  private readonly dialog = inject(MatDialogRef<TaskDialog>);
  busy = signal(false);
  error = signal('');
  text = '';
  items: Input[] = [
    {
      id: crypto.randomUUID(),
      title: '',
      notes: '',
      why: '',
      deadline: localDate(new Date(Date.now() + 86400_000).toISOString()),
      minutes: 30,
      url: '',
    },
  ];
  valid() {
    return this.items.every(
      (t) => t.title.trim() && t.deadline && t.minutes >= 1 && t.minutes <= 720,
    );
  }
  async parse() {
    this.busy.set(true);
    this.error.set('');
    try {
      const result = await this.api.request<{ tasks: Input[] }>('/parse', 'POST', {
        text: this.text,
      });
      this.items = result.tasks.map((t) => ({
        ...t,
        why: t.why ?? '',
        deadline: localDate(t.deadline),
      }));
    } catch (e) {
      this.error.set(message(e));
    } finally {
      this.busy.set(false);
    }
  }
  async save() {
    if (!this.valid() || this.busy()) return;
    this.busy.set(true);
    this.error.set('');
    try {
      for (const item of this.items) {
        this.api.state.set(
          await this.api.request('/tasks', 'POST', {
            ...item,
            deadline: new Date(item.deadline).toISOString(),
          }),
        );
      }
      await this.api.refresh();
      this.dialog.close(true);
    } catch (e) {
      this.error.set(message(e));
    } finally {
      this.busy.set(false);
    }
  }
}
