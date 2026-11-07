import { Component, computed, inject } from '@angular/core';
import { DatePipe } from '@angular/common';
import { Api } from './api';

@Component({ selector: 'app-records', imports: [DatePipe], templateUrl: './records.html' })
export class Records {
  readonly api = inject(Api);
  readonly completed = computed(() =>
    this.api.state()!.tasks.filter((t) => t.status === 'completed'),
  );
  readonly cells = Array.from({ length: 84 }, (_, i) => i);
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
