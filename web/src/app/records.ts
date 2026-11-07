import { Component, computed, inject } from '@angular/core';
import { DatePipe } from '@angular/common';
import { Api } from './api';
import { durationLabel } from '../../../shared/models';

@Component({ selector: 'app-records', imports: [DatePipe], templateUrl: './records.html' })
export class Records {
  durationLabel = durationLabel;
  readonly api = inject(Api);
  readonly completed = computed(() =>
    this.api.state()!.tasks.filter((t) => t.status === 'completed'),
  );
  readonly cells = Array.from({ length: 84 }, (_, i) => i);
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
  readonly starts = computed(() =>
    this.api
      .state()!
      .tasks.filter((t) => t.startedAt && Date.parse(t.startedAt) >= Date.now() - 7 * 86400000),
  );
  readonly onTime = computed(
    () =>
      this.starts().filter(
        (t) =>
          t.scheduledStartAt &&
          Date.parse(t.startedAt!) <= Date.parse(t.scheduledStartAt) + 5 * 60000,
      ).length,
  );
  entryLabel(kind: string) {
    return (
      (
        {
          initial: 'はじめの視聴時間',
          registration: '課題を登録',
          completion: '課題を完了',
          daily: '毎日の補充',
          work: '作業を記録',
        } as Record<string, string>
      )[kind] ?? kind
    );
  }
  activity(index: number) {
    const date = new Date();
    date.setDate(date.getDate() - (83 - index));
    return this.completed().filter(
      (t) => t.completedAt && new Date(t.completedAt).toDateString() === date.toDateString(),
    ).length;
  }
  dayLabel(index: number) {
    const date = new Date();
    date.setDate(date.getDate() - (83 - index));
    return `${date.toLocaleDateString('ja-JP')}: ${this.activity(index)}件完了`;
  }
}
