import {
  afterNextRender,
  Component,
  ElementRef,
  inject,
  input,
  Injector,
  OnDestroy,
  output,
  signal,
  viewChild,
} from '@angular/core';
import { DatePipe } from '@angular/common';
import { FormsModule } from '@angular/forms';
import { TextFieldModule } from '@angular/cdk/text-field';
import { MatButtonModule } from '@angular/material/button';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatInputModule } from '@angular/material/input';
import { MatProgressBarModule } from '@angular/material/progress-bar';
import { AssistantTurn, Task } from '../../../shared/models';
import { Api, message } from './api';
import { AssistantCard } from './assistant-card';

@Component({
  selector: 'app-assistant',
  imports: [
    DatePipe,
    FormsModule,
    TextFieldModule,
    MatButtonModule,
    MatFormFieldModule,
    MatInputModule,
    MatProgressBarModule,
    AssistantCard,
  ],
  templateUrl: './assistant.html',
  styleUrl: './assistant.scss',
})
export class Assistant implements OnDestroy {
  readonly api = inject(Api);
  private readonly injector = inject(Injector);
  readonly openSettings = output<void>();
  readonly selectedTask = input<Task | null>(null);
  readonly clearSelection = output<void>();
  taskTitle(id: string) {
    return this.api.state()?.tasks.find((t) => t.id === id)?.title ?? '削除された課題';
  }
  readonly thread = viewChild<ElementRef<HTMLElement>>('thread');
  readonly turns = signal<AssistantTurn[]>([]);
  readonly loading = signal(true);
  readonly sending = signal(false);
  readonly error = signal('');
  readonly editingMemory = signal(false);
  text = '';
  memoryText = '';
  private readonly poll = setInterval(() => {
    if (!this.sending() && this.turns().some((t) => t.status === 'pending')) void this.load(false);
  }, 5000);

  constructor() {
    void this.load();
  }
  ngOnDestroy() {
    clearInterval(this.poll);
  }
  private scroll() {
    afterNextRender(
      () => {
        const el = this.thread()?.nativeElement;
        if (el) el.scrollTop = el.scrollHeight;
      },
      { injector: this.injector },
    );
  }
  async load(initial = true) {
    if (initial) this.loading.set(true);
    const el = this.thread()?.nativeElement;
    const follow = !el || el.scrollTop + el.clientHeight >= el.scrollHeight - 48;
    try {
      const result = await this.api.request<{ turns: AssistantTurn[] }>('/assistant');
      this.turns.set(result.turns);
      this.error.set('');
      if (initial || follow) this.scroll();
    } catch (e) {
      this.error.set(message(e));
    } finally {
      if (initial) this.loading.set(false);
    }
  }
  async send(event?: Event) {
    if (event instanceof KeyboardEvent && event.isComposing) return;
    event?.preventDefault();
    const text = this.text.trim();
    if (!text || this.sending() || !this.api.config()?.aiEnabled || text.length > 2000) return;
    const last = this.turns().at(-1);
    const taskId = this.selectedTask()?.id ?? '';
    const id =
      last?.status === 'error' && last.text === text && (last.taskId ?? '') === taskId
        ? last.id
        : crypto.randomUUID();
    await this.deliver(id, text, taskId);
  }
  async retry(turn: AssistantTurn) {
    if (!this.sending()) await this.deliver(turn.id, turn.text, turn.taskId ?? '');
  }
  private put(turn: AssistantTurn) {
    this.turns.update((turns) => {
      const index = turns.findIndex((t) => t.id === turn.id);
      if (index < 0) return [...turns, turn];
      return turns.map((t, i) => (i === index ? turn : t));
    });
  }
  private async deliver(id: string, text: string, taskId: string) {
    this.sending.set(true);
    this.error.set('');
    const pending: AssistantTurn = {
      id,
      text,
      taskId,
      reply: '',
      cards: [],
      status: 'pending',
      createdAt: new Date().toISOString(),
    };
    this.put(pending);
    this.scroll();
    try {
      const turn = await this.api.request<AssistantTurn>('/assistant/messages', 'POST', {
        id,
        text,
        taskId,
      });
      this.put(turn);
      if (turn.status === 'ready' && this.text.trim() === text) this.text = '';
    } catch (e) {
      this.put({ ...pending, status: 'error', failure: message(e) });
    } finally {
      this.sending.set(false);
    }
  }
  editMemory() {
    this.memoryText = this.api.state()?.profile.context ?? '';
    this.editingMemory.set(true);
  }
  async saveMemory() {
    const profile = this.api.state()?.profile;
    if (
      profile &&
      (await this.api.mutate('/profile', { ...profile, context: this.memoryText }, 'PUT'))
    )
      this.editingMemory.set(false);
  }
}
