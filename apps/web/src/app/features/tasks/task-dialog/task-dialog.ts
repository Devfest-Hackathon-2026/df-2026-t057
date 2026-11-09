import {
  ChangeDetectionStrategy,
  Component,
  DestroyRef,
  computed,
  effect,
  inject,
  signal,
} from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import {
  FormControl,
  FormGroup,
  ReactiveFormsModule,
  Validators,
  type AbstractControl,
  type ValidationErrors,
} from '@angular/forms';
import { MatButtonModule } from '@angular/material/button';
import { MatButtonToggleModule } from '@angular/material/button-toggle';
import { MAT_DIALOG_DATA, MatDialogModule, MatDialogRef } from '@angular/material/dialog';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatInputModule } from '@angular/material/input';
import { MatProgressBarModule } from '@angular/material/progress-bar';
import { Router } from '@angular/router';
import { ApiError, newIdempotencyKey, type Draft, type TaskInput } from '@dopagaki/contract';
import { ConfigStore } from '../../../core/config/config.store';
import type { OperationError } from '../../../core/http/operation';
import { LocaleStore } from '../../../core/i18n/locale.store';
import { DateTimePipe, DurationPipe } from '../../../shared/ui/format';
import { AssistantPanel } from '../../assistant/assistant-panel';
import { AssistantStore } from '../../assistant/assistant.store';
import { SleepStore } from '../../sleep/sleep.store';
import { WalletStore } from '../../wallet/wallet.store';
import { DeadlineField } from '../deadline-field/deadline-field';
import { defaultDeadline, fromLocalInput, toLocalInput } from '../local-datetime';
import { TaskChange } from '../task-change/task-change';
import { TasksStore } from '../tasks.store';
import type { TaskDialogData } from './task-dialog.model';

type Mode = 'manual' | 'ai';
type FieldName = 'title' | 'deadline' | 'minutes' | 'why' | 'url' | 'notes';

const fieldNames: readonly FieldName[] = ['title', 'deadline', 'minutes', 'why', 'url', 'notes'];

const required = (control: AbstractControl): ValidationErrors | null =>
  Validators.required(control);

/** 締切が未来か（登録のときだけ確かめる。編集では過ぎた締切のままでもよい）。 */
function futureDeadline(control: AbstractControl<string>): Record<string, true> | null {
  const iso = fromLocalInput(control.value);
  return iso && Date.parse(iso) > Date.now() ? null : { past: true };
}

/** 課題の登録と、内容の変更（手入力・AIに相談）。 */
@Component({
  selector: 'app-task-dialog',
  imports: [
    AssistantPanel,
    DateTimePipe,
    DeadlineField,
    DurationPipe,
    MatButtonModule,
    MatButtonToggleModule,
    MatDialogModule,
    MatFormFieldModule,
    MatInputModule,
    MatProgressBarModule,
    ReactiveFormsModule,
    TaskChange,
  ],
  templateUrl: './task-dialog.html',
  styleUrl: './task-dialog.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class TaskDialog {
  protected readonly data =
    inject<TaskDialogData | null>(MAT_DIALOG_DATA, { optional: true }) ?? {};
  private readonly dialogRef = inject<MatDialogRef<TaskDialog, boolean>>(MatDialogRef);
  private readonly router = inject(Router);
  protected readonly tasks = inject(TasksStore);
  protected readonly wallet = inject(WalletStore);
  protected readonly sleep = inject(SleepStore);
  protected readonly assistant = inject(AssistantStore);
  private readonly config = inject(ConfigStore);
  private readonly locale = inject(LocaleStore);
  protected readonly text = this.locale.web;

  protected readonly editing = this.data.taskId !== undefined;
  protected readonly task = computed(() =>
    this.data.taskId === undefined ? null : (this.tasks.find(this.data.taskId) ?? null),
  );
  protected readonly missing = computed(() => this.editing && this.task() === null);
  protected readonly aiEnabled = computed(() => this.config.config()?.features.ai === true);

  protected readonly mode = signal<Mode>(this.data.mode ?? 'manual');
  /** AI のパネルは初めて開いたときに作り、以後は隠して残す（会話と入力を保つ）。 */
  protected readonly aiOpened = signal(this.mode() === 'ai');
  protected readonly minutePresets: readonly number[] = [15, 30, 60, 90, 120];

  /** 登録する課題の ID。再送しても二重に登録しないよう、ダイアログごとに一度だけ作る。 */
  private readonly newTaskId = newIdempotencyKey();

  protected readonly form = new FormGroup({
    title: new FormControl('', {
      nonNullable: true,
      validators: [required, Validators.maxLength(200)],
    }),
    deadline: new FormControl(defaultDeadline(), {
      nonNullable: true,
      validators: this.editing ? [required] : [required, futureDeadline],
    }),
    minutes: new FormControl(30, {
      nonNullable: true,
      validators: [required, Validators.min(1), Validators.max(720)],
    }),
    why: new FormControl('', { nonNullable: true, validators: [Validators.maxLength(200)] }),
    url: new FormControl('', { nonNullable: true, validators: [Validators.maxLength(2000)] }),
    notes: new FormControl('', { nonNullable: true, validators: [Validators.maxLength(4000)] }),
  });
  private readonly formValue = signal(this.form.getRawValue());

  /** 編集の変更案。入力を変えたら消す。 */
  protected readonly change = signal<{ input: TaskInput; draft: Draft } | null>(null);
  protected readonly error = signal<string | undefined>(undefined);

  protected readonly pending = computed(
    () => this.tasks.create.pending() || this.tasks.previewEdit.pending(),
  );
  protected readonly locked = computed(() => this.pending() || this.assistant.send.pending());

  // 文章から課題を取り出す（登録のときだけ）
  protected readonly extractText = new FormControl('', {
    nonNullable: true,
    validators: [Validators.maxLength(6000)],
  });
  protected readonly candidates = signal<readonly TaskInput[]>([]);
  protected readonly extractError = signal<string | undefined>(undefined);

  constructor() {
    this.assistant.startDialog(!this.editing);
    const task = this.task();
    if (task) {
      this.form.setValue({
        title: task.title,
        deadline: toLocalInput(task.deadline),
        minutes: task.minutes,
        why: task.why,
        url: task.url,
        notes: task.notes,
      });
      this.formValue.set(this.form.getRawValue());
    }
    this.form.valueChanges.pipe(takeUntilDestroyed(inject(DestroyRef))).subscribe(() => {
      this.formValue.set(this.form.getRawValue());
      this.change.set(null);
    });
    effect(() => {
      if (this.pending()) this.form.disable({ emitEvent: false });
      else this.form.enable({ emitEvent: false });
    });
  }

  protected minutesValue(): number {
    return this.formValue().minutes;
  }

  protected setMode(mode: Mode): void {
    this.mode.set(mode);
    if (mode === 'ai') this.aiOpened.set(true);
  }

  protected setMinutes(minutes: number): void {
    this.form.controls.minutes.setValue(minutes);
  }

  protected openSettings(): void {
    this.dialogRef.close();
    void this.router.navigateByUrl('/settings');
  }

  protected serverError(name: FieldName): string | undefined {
    const value: unknown = this.form.controls[name].errors?.['server'];
    return typeof value === 'string' ? value : undefined;
  }

  private input(): TaskInput | undefined {
    const value = this.form.getRawValue();
    const deadline = fromLocalInput(value.deadline);
    if (!deadline) return undefined;
    return {
      title: value.title.trim(),
      deadline,
      minutes: value.minutes,
      why: value.why.trim(),
      url: value.url.trim(),
      notes: value.notes,
    };
  }

  protected async save(): Promise<void> {
    this.form.markAllAsTouched();
    const input = this.input();
    if (this.form.invalid || !input || input.title === '' || this.missing() || this.pending())
      return;
    this.error.set(undefined);
    if (this.editing && this.data.taskId !== undefined) {
      const outcome = await this.tasks.previewTaskEdit(this.data.taskId, input);
      if (outcome.ok) this.change.set({ input, draft: outcome.value });
      else this.fail(outcome.error);
      return;
    }
    const outcome = await this.tasks.createTask({ ...input, id: this.newTaskId });
    if (outcome.ok) this.dialogRef.close(true);
    else this.fail(outcome.error);
  }

  protected async extract(): Promise<void> {
    const text = this.extractText.value.trim();
    if (!text || this.tasks.extract.pending()) return;
    this.extractError.set(undefined);
    const outcome = await this.tasks.extractTasks(text);
    if (outcome.ok) this.candidates.set(outcome.value);
    else this.extractError.set(outcome.error.message);
  }

  /** 取り出した候補を手入力の欄に入れて、確認してから登録してもらう。 */
  protected useCandidate(candidate: TaskInput): void {
    this.form.setValue({
      title: candidate.title,
      deadline: toLocalInput(candidate.deadline) || defaultDeadline(),
      minutes: candidate.minutes,
      why: candidate.why,
      url: candidate.url,
      notes: candidate.notes,
    });
    this.setMode('manual');
  }

  protected close(): void {
    this.dialogRef.close(false);
  }

  private fail(error: OperationError): void {
    this.error.set(error.message);
    if (!(error.cause instanceof ApiError)) return;
    for (const field of error.cause.fields) {
      const name = fieldNames.find((candidate) => candidate === field.field);
      if (!name) continue;
      const control = this.form.controls[name];
      control.setErrors({ ...control.errors, server: this.locale.shared().fields[field.code] });
      control.markAsTouched();
    }
  }
}
