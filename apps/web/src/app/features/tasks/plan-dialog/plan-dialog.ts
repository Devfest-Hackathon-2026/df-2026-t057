import { ChangeDetectionStrategy, Component, computed, inject, signal } from '@angular/core';
import { FormControl, ReactiveFormsModule, Validators } from '@angular/forms';
import { MatButtonModule } from '@angular/material/button';
import { MAT_DIALOG_DATA, MatDialogModule, MatDialogRef } from '@angular/material/dialog';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatIconModule } from '@angular/material/icon';
import { MatInputModule } from '@angular/material/input';
import { MatProgressBarModule } from '@angular/material/progress-bar';
import { ApiError, type Draft, type PlanChange } from '@dopagaki/contract';
import { fieldMessages, planReasonLabels } from '@dopagaki/messages';
import { ConfigStore } from '../../../core/config/config.store';
import type { OperationError } from '../../../core/http/operation';
import { DateTimePipe, TimePipe } from '../../../shared/ui/format';
import { fromLocalInput, toLocalInput } from '../local-datetime';
import { TasksStore } from '../tasks.store';
import { changeIndexOf, editStep, moveChangeUp, retimeChange } from './draft-editing';
import type { PlanDialogData } from './plan-dialog.model';

/** 計画の版が変わったなど、案を作り直す必要があるエラー。 */
const staleCodes = new Set([
  'precondition.failed',
  'plan.changes_mismatch',
  'precondition.required',
]);

/** スヌーズ後の予定の確認と、予定の調整。保存するまで現在の予定は変わらない。 */
@Component({
  selector: 'app-plan-dialog',
  imports: [
    DateTimePipe,
    MatButtonModule,
    MatDialogModule,
    MatFormFieldModule,
    MatIconModule,
    MatInputModule,
    MatProgressBarModule,
    ReactiveFormsModule,
    TimePipe,
  ],
  templateUrl: './plan-dialog.html',
  styleUrl: './plan-dialog.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class PlanDialog {
  protected readonly data = inject<PlanDialogData>(MAT_DIALOG_DATA);
  private readonly dialogRef = inject<MatDialogRef<PlanDialog, boolean>>(MatDialogRef);
  protected readonly tasks = inject(TasksStore);
  private readonly config = inject(ConfigStore);

  protected readonly draft = signal<Draft | null>(
    this.data.draft ? structuredClone(this.data.draft) : null,
  );
  protected readonly stale = signal(false);
  protected readonly error = signal<string | undefined>(undefined);
  /** 何番目の予定に、どのエラーがあるか。 */
  protected readonly changeErrors = signal<ReadonlyMap<number, string>>(new Map());
  protected readonly aiEnabled = computed(() => this.config.config()?.features.ai === true);
  protected readonly instruction = new FormControl('', {
    nonNullable: true,
    validators: [Validators.maxLength(2000)],
  });
  protected readonly startAt = new FormControl('', { nonNullable: true });

  protected readonly pending = computed(
    () => this.tasks.draft.pending() || this.tasks.adjust.pending() || this.tasks.commit.pending(),
  );
  protected readonly reasons = computed(() => {
    const draft = this.draft();
    if (!draft) return [];
    const reasons = new Set(draft.changes.map((change) => change.after.reason));
    if (draft.shortened) reasons.add('snooze_shortened');
    return [...reasons].map((reason) => planReasonLabels[reason]);
  });

  protected readonly toLocal = toLocalInput;

  constructor() {
    const draft = this.draft();
    if (draft) this.startAt.setValue(toLocalInput(draft.changes[0]?.after.start ?? ''));
    else void this.load();
  }

  /** 最新の計画から案を作り直す。 */
  protected async load(start?: string): Promise<void> {
    this.error.set(undefined);
    this.changeErrors.set(new Map());
    const current = this.draft();
    const outcome = await this.tasks.rescheduleDraft(
      this.data.taskId,
      start !== undefined && current
        ? { mode: this.data.mode, start, revision: current.baseRevision }
        : { mode: this.data.mode },
    );
    if (!outcome.ok) {
      this.fail(outcome.error);
      return;
    }
    this.draft.set(outcome.value);
    this.startAt.setValue(toLocalInput(outcome.value.changes[0]?.after.start ?? ''));
    this.stale.set(false);
  }

  protected rebuildFromStart(): void {
    const start = fromLocalInput(this.startAt.value);
    if (start) void this.load(start);
  }

  protected changeTime(index: number, field: 'start' | 'end', value: string): void {
    const time = fromLocalInput(value);
    if (time) this.updateChange(index, (change) => retimeChange(change, field, time));
  }

  protected changeStepTime(
    index: number,
    step: number,
    field: 'start' | 'end',
    value: string,
  ): void {
    const time = fromLocalInput(value);
    if (time) this.updateChange(index, (change) => editStep(change, step, field, time));
  }

  protected changeStepTitle(index: number, step: number, value: string): void {
    if (value.trim()) this.updateChange(index, (change) => editStep(change, step, 'title', value));
  }

  protected move(index: number): void {
    const draft = this.draft();
    if (draft) this.draft.set(moveChangeUp(draft, index));
  }

  protected async adjust(): Promise<void> {
    const draft = this.draft();
    const instruction = this.instruction.value.trim();
    if (!draft || !instruction) return;
    this.error.set(undefined);
    const outcome = await this.tasks.adjustDraft(draft, instruction);
    if (outcome.ok) this.draft.set(outcome.value);
    else this.fail(outcome.error);
  }

  protected async commit(): Promise<void> {
    const draft = this.draft();
    if (!draft || this.stale()) return;
    this.error.set(undefined);
    this.changeErrors.set(new Map());
    const outcome = await this.tasks.commitDraft(draft);
    if (outcome.ok) this.dialogRef.close(true);
    else this.fail(outcome.error);
  }

  protected cancel(): void {
    this.dialogRef.close(false);
  }

  protected inputValue(event: Event): string {
    return event.target instanceof HTMLInputElement ? event.target.value : '';
  }

  private updateChange(index: number, edit: (change: PlanChange) => PlanChange): void {
    const draft = this.draft();
    const change = draft?.changes[index];
    if (!draft || !change) return;
    this.draft.set({
      ...draft,
      changes: draft.changes.map((item, position) => (position === index ? edit(change) : item)),
    });
  }

  private fail(error: OperationError): void {
    this.error.set(error.message);
    if (error.code !== undefined && staleCodes.has(error.code)) this.stale.set(true);
    if (!(error.cause instanceof ApiError)) return;
    const errors = new Map<number, string>();
    for (const field of error.cause.fields) {
      const index = changeIndexOf(field.field);
      if (index !== undefined && !errors.has(index)) errors.set(index, fieldMessages[field.code]);
    }
    this.changeErrors.set(errors);
  }
}
