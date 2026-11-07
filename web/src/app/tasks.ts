import { Component, computed, inject, signal } from '@angular/core';
import { DatePipe } from '@angular/common';
import { RouterLink } from '@angular/router';
import { MatButtonModule } from '@angular/material/button';
import { MatTooltipModule } from '@angular/material/tooltip';
import { MatButtonToggleModule } from '@angular/material/button-toggle';
import { Api, localDate } from './api';
import { TaskActions } from './task-actions';
import { plannedSeconds, Task } from '../../../shared/models';

@Component({
  selector: 'app-tasks',
  imports: [DatePipe, RouterLink, MatButtonModule, MatTooltipModule, MatButtonToggleModule],
  templateUrl: './tasks.html',
})
export class Tasks {
  readonly api = inject(Api);
  readonly actions = inject(TaskActions);
  readonly status = signal('pending');
  readonly tasks = computed(() =>
    (this.api.state()?.tasks ?? [])
      .filter((t) => (t.status === 'completed') === (this.status() === 'completed'))
      .sort((a, b) => Date.parse(a.deadline) - Date.parse(b.deadline)),
  );
  taskDate(value: string) {
    return localDate(value).slice(0, 10);
  }
  plannedMinutes(task: Task) {
    return Math.ceil(plannedSeconds(task) / 60);
  }
}
