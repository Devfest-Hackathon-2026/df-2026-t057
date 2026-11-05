import { Component, inject, signal } from '@angular/core';
import { DatePipe } from '@angular/common';
import { FormsModule } from '@angular/forms';
import { MAT_DIALOG_DATA, MatDialogModule, MatDialogRef } from '@angular/material/dialog';
import { MatButtonModule } from '@angular/material/button';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatInputModule } from '@angular/material/input';
import { MatProgressBarModule } from '@angular/material/progress-bar';
import { Draft, PlanChange, State } from '../../../shared/models';
import { Api, localDate, message } from './api';

@Component({
  selector: 'plan-dialog',
  imports: [
    DatePipe,
    FormsModule,
    MatDialogModule,
    MatButtonModule,
    MatFormFieldModule,
    MatInputModule,
    MatProgressBarModule,
  ],
  template: `
    <h2 mat-dialog-title>{{ data.adjust ? '予定を調整' : 'スヌーズ後の予定を確認' }}</h2>
    @if (busy()) {
      <mat-progress-bar mode="indeterminate" aria-label="予定を処理中" />
    }
    <mat-dialog-content>
      <p class="muted">OKを押すまで、現在の予定は変わりません。</p>
      @if (draft(); as d) {
        <div class="notice-info">{{ d.reason }}</div>
        @for (change of d.changes; track change.taskId; let i = $index) {
          <section class="plan-change">
            <div class="change-heading">
              <span class="step-number">{{ i + 1 }}</span>
              <strong>{{ change.title }}</strong>
              <button
                mat-icon-button
                aria-label="順番を上に移動"
                (click)="move(i)"
                [disabled]="i === 0 || busy()"
              >
                <span aria-hidden="true" class="material-symbols-rounded">arrow_upward</span>
              </button>
            </div>
            <p class="muted before">
              変更前 {{ change.before.start | date: 'M/d HH:mm' }} —
              {{ change.before.end | date: 'HH:mm' }} · 元の見積もり {{ change.originalMinutes }}分
            </p>
            <div class="form-row">
              <mat-form-field appearance="outline">
                <mat-label>開始</mat-label>
                <input
                  matInput
                  type="datetime-local"
                  [ngModel]="local(change.after.start)"
                  (ngModelChange)="changeTime(change, 'start', $event)"
                />
              </mat-form-field>
              <mat-form-field appearance="outline">
                <mat-label>終了</mat-label>
                <input
                  matInput
                  type="datetime-local"
                  [ngModel]="local(change.after.end)"
                  (ngModelChange)="changeTime(change, 'end', $event)"
                />
              </mat-form-field>
            </div>
            @for (step of change.after.steps; track $index) {
              <div class="plan-step">
                <span>{{ step.start | date: 'HH:mm' }}–{{ step.end | date: 'HH:mm' }}</span>
                <span>{{ step.title }}</span>
              </div>
            }
          </section>
        }
        @if (api.config()?.aiEnabled) {
          <div class="ai-box">
            <mat-form-field appearance="outline">
              <mat-label>Geminiに調整を頼む</mat-label>
              <textarea
                matInput
                [(ngModel)]="instruction"
                placeholder="最初の課題を15分にして、見直しに時間を回して"
                rows="2"
              ></textarea>
            </mat-form-field>
            <button
              mat-stroked-button
              (click)="adjust()"
              [disabled]="busy() || !instruction.trim()"
            >
              {{ busy() ? '調整しています…' : '候補を作り直す' }}
            </button>
          </div>
        }
      } @else if (!error()) {
        <p>変更する予定を準備しています…</p>
      }
      @if (error()) {
        <p role="alert" class="error">{{ error() }}</p>
      }
      @if (stale()) {
        <button mat-stroked-button (click)="load()">最新の予定で作り直す</button>
      }
    </mat-dialog-content>
    <mat-dialog-actions align="end">
      <button mat-button mat-dialog-close [disabled]="busy()">キャンセル</button>
      <button mat-flat-button (click)="commit()" [disabled]="busy() || !draft() || stale()">
        {{ busy() ? '保存しています…' : 'OK・この予定に変更' }}
      </button>
    </mat-dialog-actions>
  `,
})
export class PlanDialog {
  readonly api = inject(Api);
  readonly data = inject<{ taskId: string; draft?: Draft; adjust: boolean }>(MAT_DIALOG_DATA);
  private readonly dialog = inject(MatDialogRef<PlanDialog>);
  readonly draft = signal<Draft | null>(this.data.draft ? structuredClone(this.data.draft) : null);
  readonly busy = signal(false);
  readonly error = signal('');
  readonly stale = signal(false);
  instruction = '';
  local = localDate;
  constructor() {
    if (!this.draft()) void this.load();
  }
  async load() {
    this.busy.set(true);
    this.error.set('');
    try {
      this.draft.set(
        await this.api.request<Draft>(
          `/tasks/${this.data.taskId}/draft?adjust=${this.data.adjust}`,
        ),
      );
      this.stale.set(false);
    } catch (e) {
      this.error.set(message(e));
    } finally {
      this.busy.set(false);
    }
  }
  changeTime(change: PlanChange, field: 'start' | 'end', value: string) {
    if (!value) return;
    const previous = structuredClone(change.after);
    change.after[field] = new Date(value).toISOString();
    const duration = Date.parse(change.after.end) - Date.parse(change.after.start);
    const ratio = duration / (Date.parse(previous.end) - Date.parse(previous.start));
    let cursor = Date.parse(change.after.start);
    change.after.steps = previous.steps.map((step, i) => {
      const end =
        i === previous.steps.length - 1
          ? Date.parse(change.after.end)
          : cursor + Math.round((Date.parse(step.end) - Date.parse(step.start)) * ratio);
      const result = {
        title: step.title,
        start: new Date(cursor).toISOString(),
        end: new Date(end).toISOString(),
      };
      cursor = end;
      return result;
    });
    change.plannedMinutes = duration / 60000;
  }
  move(index: number) {
    const d = this.draft()!;
    const start = Date.parse(d.changes[0].after.start);
    [d.changes[index - 1], d.changes[index]] = [d.changes[index], d.changes[index - 1]];
    let cursor = start;
    for (const change of d.changes) {
      const delta = cursor - Date.parse(change.after.start);
      change.after = {
        ...change.after,
        start: new Date(cursor).toISOString(),
        end: new Date(Date.parse(change.after.end) + delta).toISOString(),
        steps: change.after.steps.map((step) => ({
          ...step,
          start: new Date(Date.parse(step.start) + delta).toISOString(),
          end: new Date(Date.parse(step.end) + delta).toISOString(),
        })),
      };
      cursor = Date.parse(change.after.end);
    }
  }
  async adjust() {
    this.busy.set(true);
    this.error.set('');
    try {
      this.draft.set(
        await this.api.request<Draft>('/draft/adjust', 'POST', {
          draft: this.draft(),
          instruction: this.instruction,
        }),
      );
    } catch (e) {
      this.error.set(message(e));
    } finally {
      this.busy.set(false);
    }
  }
  async commit() {
    if (this.busy() || this.stale()) return;
    this.busy.set(true);
    this.error.set('');
    try {
      this.api.state.set(await this.api.request<State>('/draft/commit', 'POST', this.draft()));
      await this.api.refresh();
      this.dialog.close(true);
    } catch (e) {
      this.error.set(message(e));
      this.stale.set(e instanceof Error && e.cause === 409);
    } finally {
      this.busy.set(false);
    }
  }
}
