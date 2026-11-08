import {
  ChangeDetectionStrategy,
  Component,
  computed,
  inject,
  input,
  linkedSignal,
  output,
  signal,
} from '@angular/core';
import { MatButtonModule } from '@angular/material/button';
import { MatProgressBarModule } from '@angular/material/progress-bar';
import type { Draft, TaskInput } from '@dopagaki/contract';
import { DateTimePipe, TimePipe } from '../../../shared/ui/format';
import { CalendarStore } from '../../calendar/calendar.store';
import { TasksStore } from '../tasks.store';

export const calendarPendingMessage =
  '管理画面の変更は保存済みですが、Googleへの反映を確認できていません。カレンダーの「Googleと同期」で再試行してください。';

/**
 * 課題の内容の変更案（変更前後と時間割）を見せ、確認したら PUT /tasks/{id}（If-Match = draft.baseRevision）で保存する。
 * 計画の版が変わっていたら「最新の予定で再確認」で edit-preview を取り直す。
 * TaskDialog（手入力の編集）と、相談の update カードで使う。
 */
@Component({
  selector: 'app-task-change',
  imports: [DateTimePipe, MatButtonModule, MatProgressBarModule, TimePipe],
  templateUrl: './task-change.html',
  styleUrl: './task-change.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class TaskChange {
  private readonly tasks = inject(TasksStore);
  private readonly calendar = inject(CalendarStore);

  readonly taskId = input.required<string>();
  readonly input = input.required<TaskInput>();
  readonly draft = input.required<Draft>();
  /** 保存できたとき。 */
  readonly saved = output();

  /** 表示中の変更案。「最新の予定で再確認」で取り直したら置き換える。 */
  protected readonly candidate = linkedSignal(() => this.draft());
  protected readonly current = computed(() => this.tasks.find(this.taskId()));
  protected readonly stale = computed(
    () => this.candidate().baseRevision !== this.tasks.revision(),
  );
  protected readonly pending = computed(
    () => this.tasks.update.pending() || this.tasks.previewEdit.pending(),
  );
  protected readonly done = signal(false);
  protected readonly error = signal<string | undefined>(undefined);

  protected async recheck(): Promise<void> {
    this.error.set(undefined);
    await this.tasks.refresh();
    const outcome = await this.tasks.previewTaskEdit(this.taskId(), this.input());
    if (outcome.ok) this.candidate.set(outcome.value);
    else this.error.set(outcome.error.message);
  }

  protected async save(): Promise<void> {
    if (this.stale()) return;
    this.error.set(undefined);
    const outcome = await this.tasks.updateTask(
      this.taskId(),
      this.input(),
      this.candidate().baseRevision,
    );
    if (outcome.ok) {
      this.done.set(true);
      this.saved.emit();
      return;
    }
    await this.calendar.refresh();
    this.error.set(
      this.calendar.needsRetry()
        ? `${calendarPendingMessage}（${outcome.error.message}）`
        : outcome.error.message,
    );
  }
}
