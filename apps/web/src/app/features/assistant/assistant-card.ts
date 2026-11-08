import {
  ChangeDetectionStrategy,
  Component,
  computed,
  effect,
  inject,
  input,
  signal,
  untracked,
} from '@angular/core';
import {
  FormControl,
  FormGroup,
  ReactiveFormsModule,
  Validators,
  type AbstractControl,
  type ValidationErrors,
} from '@angular/forms';
import { MatButtonModule } from '@angular/material/button';
import { MatCardModule } from '@angular/material/card';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatIconModule } from '@angular/material/icon';
import { MatInputModule } from '@angular/material/input';
import { MatProgressBarModule } from '@angular/material/progress-bar';
import type { AssistantCard, Plan, Task, TaskCreateInput, TaskInput } from '@dopagaki/contract';
import { AuthService } from '../../core/auth/auth.service';
import { ConfigStore } from '../../core/config/config.store';
import type { Outcome } from '../../core/http/operation';
import { LocaleStore } from '../../core/i18n/locale.store';
import { DateTimePipe, TimePipe } from '../../shared/ui/format';
import { CalendarStore } from '../calendar/calendar.store';
import { ProfileStore } from '../settings/profile.store';
import { SleepStore } from '../sleep/sleep.store';
import { TaskActions } from '../tasks/task-actions';
import { TaskChange } from '../tasks/task-change/task-change';
import { TasksStore } from '../tasks/tasks.store';
import { WalletStore } from '../wallet/wallet.store';
import { AssistantStore } from './assistant.store';

function required(control: AbstractControl): ValidationErrors | null {
  return Validators.required(control);
}

/** ISO の日時を `datetime-local` の値（この端末の時刻）にする。 */
function toLocalInput(iso: string): string {
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return '';
  const pad = (value: number): string => String(value).padStart(2, '0');
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

function fromLocalInput(local: string): string {
  const date = new Date(local);
  return Number.isNaN(date.getTime()) ? '' : date.toISOString();
}

function taskInputOf(input: TaskCreateInput): TaskInput {
  return {
    title: input.title,
    notes: input.notes,
    why: input.why,
    deadline: input.deadline,
    minutes: input.minutes,
    url: input.url,
  };
}

/**
 * 相談の返答に付く提案のカード。押すまで何も保存しない。
 * 操作の通信状態とエラーは、カードごとに持つ（同じ種類のカードが並んでも、ほかのカードに出さない）。
 */
@Component({
  selector: 'app-assistant-card',
  imports: [
    DateTimePipe,
    MatButtonModule,
    MatCardModule,
    MatFormFieldModule,
    MatIconModule,
    MatInputModule,
    MatProgressBarModule,
    ReactiveFormsModule,
    TaskChange,
    TimePipe,
  ],
  templateUrl: './assistant-card.html',
  styleUrl: './assistant-card.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class AssistantCardView {
  readonly card = input.required<AssistantCard>();

  protected readonly tasks = inject(TasksStore);
  protected readonly calendar = inject(CalendarStore);
  private readonly assistant = inject(AssistantStore);
  private readonly profile = inject(ProfileStore);
  private readonly sleep = inject(SleepStore);
  private readonly wallet = inject(WalletStore);
  private readonly actions = inject(TaskActions);
  private readonly auth = inject(AuthService);
  private readonly config = inject(ConfigStore);
  private readonly shared = inject(LocaleStore).shared;
  protected readonly text = inject(LocaleStore).web;

  protected readonly pending = signal(false);
  protected readonly error = signal<string | undefined>(undefined);

  protected readonly createCard = computed(() => {
    const card = this.card();
    return card.kind === 'create' ? card : undefined;
  });
  protected readonly rescheduleCard = computed(() => {
    const card = this.card();
    return card.kind === 'reschedule' ? card : undefined;
  });
  protected readonly updateCard = computed(() => {
    const card = this.card();
    return card.kind === 'update' ? card : undefined;
  });
  protected readonly memoryCard = computed(() => {
    const card = this.card();
    return card.kind === 'memory' ? card : undefined;
  });
  protected readonly sleepCard = computed(() => {
    const card = this.card();
    return card.kind === 'sleep' ? card : undefined;
  });

  protected readonly demo = computed(() => this.auth.mode() === 'demo');

  // ── create ──
  /** 確認済みの内容（登録に使う）。 */
  protected readonly confirmed = signal<TaskCreateInput | undefined>(undefined);
  protected readonly preview = signal<{ plan: Plan; revision: number } | undefined>(undefined);
  protected readonly editing = signal(false);
  protected readonly form = new FormGroup({
    title: new FormControl('', {
      nonNullable: true,
      validators: [required, Validators.maxLength(200)],
    }),
    deadline: new FormControl('', { nonNullable: true, validators: [required] }),
    minutes: new FormControl(30, {
      nonNullable: true,
      validators: [required, Validators.min(1), Validators.max(720)],
    }),
    notes: new FormControl('', { nonNullable: true, validators: [Validators.maxLength(4000)] }),
    url: new FormControl('', { nonNullable: true, validators: [Validators.maxLength(2000)] }),
  });
  /** 登録する課題の ID。カードを押し直しても二重に登録しない。 */
  protected readonly createTaskId = computed(() => {
    const card = this.createCard();
    return card ? (card.input.id ?? card.id) : undefined;
  });
  /** カードに結び付いた、登録済みの課題。 */
  protected readonly task = computed<Task | undefined>(() => {
    const card = this.card();
    const id =
      card.kind === 'create'
        ? (card.input.id ?? card.id)
        : card.kind === 'reschedule' || card.kind === 'update'
          ? card.taskId
          : undefined;
    return id === undefined ? undefined : this.tasks.find(id);
  });
  protected readonly stale = computed(() => {
    const preview = this.preview();
    return preview !== undefined && preview.revision !== this.tasks.revision();
  });
  /** live でカレンダー連携を使える環境では、連携してから登録する。 */
  protected readonly needsCalendar = computed(
    () =>
      this.auth.mode() === 'live' &&
      this.config.config()?.features.calendar === true &&
      !this.calendar.connected(),
  );
  protected readonly registerLabel = computed(() => {
    const reward = this.shared().durationLabel(
      this.wallet.value()?.rules.registrationRewardSeconds ?? 300,
    );
    const card = this.text().assistant.card;
    return this.sleep.night() ? card.registerAtNight(reward) : card.register(reward);
  });

  // ── reschedule ──
  protected readonly rescheduleFresh = computed(() => {
    const card = this.rescheduleCard();
    return card !== undefined && card.revision === this.tasks.revision();
  });

  // ── memory ──
  protected readonly memoryText = new FormControl('', {
    nonNullable: true,
    validators: [required, Validators.maxLength(500)],
  });
  protected readonly remembered = computed(() =>
    this.assistant.rememberedCards().has(this.card().id),
  );

  // ── sleep ──
  protected readonly sleepSet = computed(() => {
    const card = this.sleepCard();
    const current = this.sleep.sleep();
    return (
      card !== undefined &&
      current !== null &&
      current.bedtime === card.sleep.bedtime &&
      current.wake === card.sleep.wake
    );
  });
  protected readonly sleepBonus = computed(() =>
    this.shared().durationLabel(this.wallet.value()?.rules.sleepBonusSeconds ?? 900),
  );
  protected readonly balanceCap = computed(() =>
    this.shared().durationLabel(this.wallet.value()?.rules.balanceCapSeconds ?? 1800),
  );

  constructor() {
    // カードが初めて表示されたときに、提案の内容を手元にコピーする。
    let initializedFor: string | undefined;
    effect(() => {
      const card = this.card();
      if (initializedFor === card.id) return;
      initializedFor = card.id;
      untracked(() => {
        if (card.kind === 'create') {
          this.confirmed.set(card.input);
          this.preview.set({ plan: card.preview, revision: card.revision });
          this.resetForm(card.input);
        } else if (card.kind === 'memory') {
          this.memoryText.setValue(card.memory);
        }
      });
    });
  }

  protected edit(): void {
    const confirmed = this.confirmed();
    if (confirmed) this.resetForm(confirmed);
    this.editing.set(true);
  }

  protected cancelEdit(): void {
    this.editing.set(false);
  }

  /** 編集した内容（または確認済みの内容）で、登録した場合の計画を取り直す。 */
  protected async check(): Promise<void> {
    const confirmed = this.confirmed();
    if (!confirmed) return;
    const next: TaskCreateInput = this.editing()
      ? {
          ...confirmed,
          title: this.form.controls.title.value.trim(),
          deadline: fromLocalInput(this.form.controls.deadline.value),
          minutes: this.form.controls.minutes.value,
          notes: this.form.controls.notes.value,
          url: this.form.controls.url.value.trim(),
        }
      : confirmed;
    if (!next.title || !next.deadline) return;
    const outcome = await this.track(this.tasks.previewNewTask(taskInputOf(next)));
    if (!outcome.ok) return;
    this.confirmed.set(next);
    this.preview.set(outcome.value);
    this.editing.set(false);
  }

  protected async register(): Promise<void> {
    const confirmed = this.confirmed();
    const preview = this.preview();
    const id = this.createTaskId();
    if (!confirmed || !preview || id === undefined || this.task() || this.stale()) return;
    const outcome = await this.track(this.tasks.createTask({ ...confirmed, id }, preview.revision));
    if (!outcome.ok) void this.tasks.refresh();
  }

  /** 登録済みの課題の予定を調整する。提案した開始時刻が新しければ、その案から始める。 */
  protected async adjust(task: Task): Promise<void> {
    const card = this.rescheduleCard();
    if (card?.start !== undefined && this.rescheduleFresh()) {
      const outcome = await this.track(
        this.tasks.rescheduleDraft(task.id, {
          mode: 'adjust',
          start: card.start,
          revision: card.revision,
        }),
      );
      if (outcome.ok) {
        void this.actions.plan(task, 'adjust', outcome.value);
        return;
      }
      if (outcome.error.code === 'precondition.failed') void this.tasks.refresh();
      return;
    }
    void this.actions.plan(task, 'adjust');
  }

  protected async remember(): Promise<void> {
    const text = this.memoryText.value.trim();
    if (!text) return;
    const outcome = await this.track(this.profile.rememberLine(text));
    if (outcome.ok) this.assistant.markRemembered(this.card().id);
  }

  protected async setSleep(): Promise<void> {
    const card = this.sleepCard();
    if (card) await this.track(this.sleep.update(card.sleep));
  }

  private resetForm(input: TaskCreateInput): void {
    this.form.setValue({
      title: input.title,
      deadline: toLocalInput(input.deadline),
      minutes: input.minutes,
      notes: input.notes,
      url: input.url,
    });
  }

  private async track<T>(request: Promise<Outcome<T>>): Promise<Outcome<T>> {
    this.pending.set(true);
    this.error.set(undefined);
    try {
      const outcome = await request;
      if (!outcome.ok) this.error.set(outcome.error.message);
      return outcome;
    } finally {
      this.pending.set(false);
    }
  }
}
