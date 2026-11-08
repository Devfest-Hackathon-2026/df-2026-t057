import { Component, computed, inject, signal } from '@angular/core';
import { DatePipe } from '@angular/common';
import { MatButtonModule } from '@angular/material/button';
import { MatTooltipModule } from '@angular/material/tooltip';
import { durationLabel } from '../../../shared/models';
import { buildActivity } from '../../../shared/activity';
import { Api } from './api';

interface StoredRecords {
  nextCursor: string;
  activity: { completed: number; days: { date: string; completed: number }[] };
  completions: {
    taskId: string;
    title: string;
    at: string;
    workSeconds?: number;
    rewardSeconds: number;
  }[];
  week: { completed: number; starts: number; onTime: number; snoozes: number; workSeconds: number };
}
@Component({
  selector: 'app-records',
  imports: [DatePipe, MatTooltipModule, MatButtonModule],
  templateUrl: './records.html',
  styleUrl: './records.scss',
})
export class Records {
  readonly api = inject(Api);
  readonly stored = signal<StoredRecords | null>(null);
  readonly recordError = signal('');
  constructor() {
    void this.api
      .request<StoredRecords>('/records')
      .then((value) => this.stored.set(value))
      .catch(() =>
        this.recordError.set('保存記録を読み込めませんでした。画面を開き直してください。'),
      );
  }

  durationLabel = durationLabel;
  readonly activity = computed(() => {
    const state = this.api.state()!;
    const base = buildActivity(state.tasks, state.ledger);
    const saved = this.stored()?.activity;
    if (!saved) return base;
    return {
      ...base,
      completed: saved.completed,
      days: saved.days.map((day) => {
        const date = new Date(day.date + 'T00:00:00');
        const key = date.toDateString();
        return {
          date,
          key,
          completed: day.completed,
          earnedSeconds: base.days.find((d) => d.key === key)?.earnedSeconds ?? 0,
        };
      }),
    };
  });
  readonly loadingMore = signal(false);
  async loadMore() {
    const previous = this.stored();
    if (!previous?.nextCursor || this.loadingMore()) return;
    this.loadingMore.set(true);
    this.recordError.set('');
    try {
      const next = await this.api.request<StoredRecords>(
        '/records?cursor=' + encodeURIComponent(previous.nextCursor),
      );
      this.stored.set({ ...next, completions: [...previous.completions, ...next.completions] });
    } catch {
      this.recordError.set('続きを読み込めませんでした。もう一度お試しください。');
    } finally {
      this.loadingMore.set(false);
    }
  }
  readonly selectedKey = signal(new Date().toDateString());
  readonly selectedDay = computed(
    () =>
      this.activity().days.find((day) => day.key === this.selectedKey()) ??
      this.activity().days[83],
  );
  readonly weeks = computed(() => Math.ceil((this.activity().days[0].date.getDay() + 84) / 7));
  readonly cells = computed(() => {
    const days = this.activity().days;
    const offset = days[0].date.getDay();
    return Array.from({ length: this.weeks() * 7 }, (_, index) => days[index - offset] ?? null);
  });
  readonly viewing = computed(() =>
    [
      ...(this.api.state()!.viewingHistory ?? []),
      ...(this.api.state()!.viewingSession ? [this.api.state()!.viewingSession!] : []),
    ].filter((x) => Date.parse(x.startedAt) >= Date.now() - 7 * 86400000),
  );
  readonly ended = computed(() => this.viewing().filter((x) => x.endedAt));
  readonly extensions = computed(() => this.viewing().filter((x) => x.extendFrom).length);
  readonly notStarted = computed(
    () =>
      this.api
        .state()!
        .tasks.filter(
          (t) =>
            !t.startedAt &&
            t.status === 'pending' &&
            Date.parse(t.plan.start) < Date.now() &&
            Date.parse(t.plan.start) >= Date.now() - 7 * 86400000,
        ).length,
  );
  entryLabel(kind: string) {
    return (
      (
        {
          initial: 'はじめの視聴時間',
          reset: '新ルールへ統一（残高10分）',
          registration: '課題を登録',
          completion: '課題を完了',
          daily: '毎日の補充',
          work: '作業を記録',
          sleep: '睡眠の報酬',
          viewing: '視聴',
        } as Record<string, string>
      )[kind] ?? kind
    );
  }
  readonly history = computed(() => {
    const state = this.api.state()!;
    const titles = new Map(state.tasks.map((task) => [task.id, task.title]));
    return state.ledger
      .slice()
      .sort((a, b) => Date.parse(b.at) - Date.parse(a.at))
      .map((entry) => ({
        ...entry,
        title: titles.get(entry.taskId ?? '') ?? this.entryLabel(entry.kind),
        action: this.entryLabel(entry.kind),
      }));
  });
  dayLabel(day: ReturnType<typeof buildActivity>['days'][number]) {
    return `${day.date.toLocaleDateString('ja-JP')}: ${day.completed}件完了、視聴時間 ＋${durationLabel(day.earnedSeconds)}`;
  }
}
