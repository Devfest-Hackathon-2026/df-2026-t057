import {
  ChangeDetectionStrategy,
  Component,
  type ElementRef,
  Injector,
  afterNextRender,
  computed,
  effect,
  inject,
  input,
  output,
  signal,
  untracked,
  viewChild,
} from '@angular/core';
import { TextFieldModule } from '@angular/cdk/text-field';
import { FormControl, ReactiveFormsModule } from '@angular/forms';
import { MatButtonModule } from '@angular/material/button';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatIconModule } from '@angular/material/icon';
import { MatInputModule } from '@angular/material/input';
import { MatProgressBarModule } from '@angular/material/progress-bar';
import { MatTooltipModule } from '@angular/material/tooltip';
import type { AssistantTurn, Task } from '@dopagaki/contract';
import { ConfigStore } from '../../core/config/config.store';
import { LocaleStore } from '../../core/i18n/locale.store';
import { TimePipe } from '../../shared/ui/format';
import { CalendarStore } from '../calendar/calendar.store';
import { ProfileStore } from '../settings/profile.store';
import { TasksStore } from '../tasks/tasks.store';
import { AssistantCardView } from './assistant-card';
import { AssistantStore, type ComposerKey } from './assistant.store';

const maxLength = 2000;

/**
 * AI 相談の会話と入力欄。カレンダーの右側と、TaskDialog の「AIに相談」で使う。
 */
@Component({
  selector: 'app-assistant',
  host: { '[class.embedded]': 'embedded()' },
  imports: [
    AssistantCardView,
    MatButtonModule,
    MatFormFieldModule,
    MatIconModule,
    MatInputModule,
    MatProgressBarModule,
    MatTooltipModule,
    ReactiveFormsModule,
    TextFieldModule,
    TimePipe,
  ],
  templateUrl: './assistant-panel.html',
  styleUrl: './assistant-panel.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class AssistantPanel {
  /** 相談の対象にする課題。 */
  readonly selectedTask = input<Task | null>(null);
  /** 対象の課題を外せないようにする（TaskDialog）。 */
  readonly fixedTask = input(false);
  /** 下書きを持つ場所。 */
  readonly embedded = input(false);
  readonly composer = input<ComposerKey>('workspace');
  readonly clearSelection = output();
  readonly openSettings = output();

  protected readonly store = inject(AssistantStore);
  protected readonly profile = inject(ProfileStore);
  protected readonly calendar = inject(CalendarStore);
  private readonly config = inject(ConfigStore);
  private readonly tasks = inject(TasksStore);
  private readonly injector = inject(Injector);
  private readonly thread = viewChild<ElementRef<HTMLElement>>('thread');
  protected readonly text = inject(LocaleStore).web;

  protected readonly maxLength = maxLength;
  protected readonly aiEnabled = computed(() => this.config.config()?.features.ai === true);
  protected readonly turns = computed(() => this.store.turnsFor(this.composer()));
  protected readonly draft = computed(() => this.store.draft(this.composer()));
  protected readonly sending = this.store.send.pending;
  protected readonly canSend = computed(() => {
    const text = this.draft().trim();
    return this.aiEnabled() && !this.sending() && text !== '' && text.length <= maxLength;
  });
  protected readonly firstLoad = computed(() => !this.store.loaded() && this.store.load.pending());

  protected readonly editingMemory = signal(false);
  protected readonly memory = new FormControl('', { nonNullable: true });
  protected readonly memoryError = signal<string | undefined>(undefined);

  constructor() {
    this.store.ensureLoaded();
    // 会話が増えたら、下の方を見ていたときだけ一番下までスクロールする。
    let initial = true;
    effect(() => {
      const count = this.turns().length;
      untracked(() => {
        const element = this.thread()?.nativeElement;
        const follow =
          initial ||
          !element ||
          element.scrollTop + element.clientHeight >= element.scrollHeight - 160;
        if (count > 0) initial = false;
        if (!follow) return;
        afterNextRender(
          () => {
            const target = this.thread()?.nativeElement;
            if (target) target.scrollTop = target.scrollHeight;
          },
          { injector: this.injector },
        );
      });
    });
  }

  protected taskTitle(taskId: string): string {
    return this.tasks.find(taskId)?.title ?? this.text().assistant.panel.deletedTask;
  }

  protected setText(value: string): void {
    this.store.setDraft(this.composer(), value);
  }

  protected onInput(event: Event): void {
    this.setText((event.target as HTMLTextAreaElement).value);
  }

  protected send(event?: Event): void {
    if (event instanceof KeyboardEvent && event.isComposing) return;
    event?.preventDefault();
    if (!this.canSend()) return;
    void this.store.sendMessage(this.draft().trim(), this.selectedTask()?.id, this.composer());
  }

  protected retry(turn: AssistantTurn): void {
    if (this.sending()) return;
    void this.store.retry(turn, this.composer());
  }

  protected editMemory(): void {
    this.memory.setValue(this.profile.value()?.context ?? '');
    this.memoryError.set(undefined);
    this.editingMemory.set(true);
  }

  protected async saveMemory(): Promise<void> {
    const profile = this.profile.value();
    if (!profile) return;
    const outcome = await this.profile.update({
      name: profile.name,
      timeZone: profile.timeZone,
      locale: profile.locale,
      context: this.memory.value,
    });
    if (outcome.ok) this.editingMemory.set(false);
    else this.memoryError.set(outcome.error.message);
  }
}
