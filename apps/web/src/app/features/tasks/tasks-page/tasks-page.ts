import { ChangeDetectionStrategy, Component, computed, inject, signal } from '@angular/core';
import { MatButtonModule } from '@angular/material/button';
import { MatButtonToggleModule } from '@angular/material/button-toggle';
import { MatDialog } from '@angular/material/dialog';
import { MatIconModule } from '@angular/material/icon';
import { MatTooltipModule } from '@angular/material/tooltip';
import { RouterLink } from '@angular/router';
import type { Task } from '@dopagaki/contract';
import { LocaleStore } from '../../../core/i18n/locale.store';
import { DateTimePipe, localDateKey, planMinutes } from '../../../shared/ui/format';
import { TaskActions } from '../task-actions';
import { TasksStore } from '../tasks.store';
import { DeleteTaskDialog } from './delete-task-dialog';

type Filter = 'pending' | 'completed';

/** 未完了（作業中を含む）か完了の課題を、締切の早い順に並べる。 */
export function filterTasks(tasks: readonly Task[], filter: Filter): Task[] {
  return tasks
    .filter((task) => (task.status === 'completed') === (filter === 'completed'))
    .sort((a, b) => Date.parse(a.deadline) - Date.parse(b.deadline));
}

/** 課題の一覧。 */
@Component({
  selector: 'app-tasks-page',
  imports: [
    DateTimePipe,
    MatButtonModule,
    MatButtonToggleModule,
    MatIconModule,
    MatTooltipModule,
    RouterLink,
  ],
  templateUrl: './tasks-page.html',
  styleUrl: './tasks-page.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class TasksPage {
  protected readonly store = inject(TasksStore);
  protected readonly actions = inject(TaskActions);
  private readonly dialog = inject(MatDialog);
  protected readonly text = inject(LocaleStore).web;

  protected readonly filter = signal<Filter>('pending');
  protected readonly tasks = computed(() => filterTasks(this.store.tasks(), this.filter()));
  protected readonly planMinutes = planMinutes;
  protected readonly dateKey = localDateKey;

  protected confirmDelete(task: Task): void {
    this.store.remove.clearError();
    this.dialog.open(DeleteTaskDialog, {
      data: task,
      width: '28rem',
      maxWidth: 'calc(100vw - 2rem)',
    });
  }
}
