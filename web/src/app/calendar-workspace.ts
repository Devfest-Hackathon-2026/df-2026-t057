import { Component, computed, effect, inject, signal, viewChild } from '@angular/core';
import { DatePipe } from '@angular/common';
import { ActivatedRoute, Router } from '@angular/router';
import { toSignal } from '@angular/core/rxjs-interop';
import { MatButtonModule } from '@angular/material/button';
import { MatButtonToggleModule } from '@angular/material/button-toggle';
import { CalendarOptions, FullCalendarComponent, FullCalendarModule } from '@fullcalendar/angular';
import timeGridPlugin from '@fullcalendar/angular/timegrid';
import themePlugin from '@fullcalendar/angular/themes/classic';
// The Angular 7.1.4 package omits individual locale files; use the public core export.
import ja from 'fullcalendar/locales/ja';
import { Api, localDate } from './api';
import { TaskActions } from './task-actions';
import { Assistant } from './assistant';
import { AssistantCard } from './assistant-card';
import { Task, AssistantCard as Card } from '../../../shared/models';

@Component({
  selector: 'app-calendar-workspace',
  imports: [
    DatePipe,
    MatButtonModule,
    MatButtonToggleModule,
    FullCalendarModule,
    Assistant,
    AssistantCard,
  ],
  templateUrl: './calendar-workspace.html',
  styleUrl: './calendar-workspace.scss',
})
export class CalendarWorkspace {
  readonly api = inject(Api);
  readonly actions = inject(TaskActions);
  private readonly router = inject(Router);
  private readonly route = inject(ActivatedRoute);
  private readonly params = toSignal(this.route.queryParamMap, {
    initialValue: this.route.snapshot.queryParamMap,
  });
  readonly calendar = viewChild(FullCalendarComponent);
  readonly title = signal('');
  readonly ready = signal(false);
  readonly panel = signal<'assistant' | 'detail'>('assistant');
  readonly mobilePart = signal('calendar');
  readonly selected = computed(
    () => this.api.state()?.tasks.find((t) => t.id === this.params().get('task')) ?? null,
  );
  readonly card = computed<Card>(() => ({
    id: `task-${this.selected()?.id}`,
    kind: 'task',
    taskId: this.selected()?.id,
  }));
  readonly next = computed(
    () =>
      this.api
        .state()
        ?.tasks.filter((t) => t.status === 'pending')
        .sort((a, b) => Date.parse(a.plan.start) - Date.parse(b.plan.start))[0],
  );
  readonly view = computed(() =>
    this.params().get('view') === 'day'
      ? 'day'
      : this.params().get('view') === 'week'
        ? 'week'
        : window.matchMedia('(max-width: 1100px)').matches
          ? 'day'
          : 'week',
  );
  readonly events = computed(() =>
    (this.api.state()?.tasks ?? []).map((t) => ({
      id: t.id,
      title: t.title,
      start: t.plan.start,
      end: t.plan.end,
      className: [
        'workspace-event',
        t.status,
        ...(this.selected()?.id === t.id ? ['selected'] : []),
      ].join(' '),
    })),
  );
  readonly timeZone = Intl.DateTimeFormat().resolvedOptions().timeZone;
  private lastSelected = '';
  readonly options: CalendarOptions = {
    plugins: [themePlugin, timeGridPlugin],
    locale: ja,
    initialView: this.view() === 'day' ? 'timeGridDay' : 'timeGridWeek',
    initialDate: this.validDate(this.params().get('date')) ?? this.selected()?.plan.start,
    headerToolbar: false,
    firstDay: 1,
    height: 650,
    allDaySlot: false,
    nowIndicator: true,
    editable: false,
    eventInteractive: true,
    slotDuration: '00:30:00',
    scrollTime: `${Math.max(0, new Date().getHours() - 1)
      .toString()
      .padStart(2, '0')}:00:00`,
    eventMinHeight: 36,
    slotEventOverlap: false,
    eventTimeFormat: { hour: '2-digit', minute: '2-digit', hour12: false },
    slotHeaderFormat: { hour: '2-digit', minute: '2-digit', hour12: false },
    eventClick: (info) => {
      const task = this.api.state()?.tasks.find((t) => t.id === info.event.id);
      if (task) this.select(task);
    },
    datesSet: (info) => {
      this.title.set(info.view.title);
      this.ready.set(true);
    },
  };
  constructor() {
    effect(() => {
      const calendar = this.calendar()?.getApi();
      if (!calendar || !this.ready()) return;
      const view = this.view() === 'day' ? 'timeGridDay' : 'timeGridWeek';
      if (calendar.view.type !== view) calendar.changeView(view);
      const date = this.validDate(this.params().get('date'));
      if (date && localDate(calendar.getDate().toISOString()).slice(0, 10) !== date)
        calendar.gotoDate(date);
      const task = this.selected();
      if (task && task.id !== this.lastSelected) {
        this.panel.set('detail');
        this.mobilePart.set('panel');
      }
      this.lastSelected = task?.id ?? '';
    });
  }
  private validDate(value: string | null) {
    if (!value || !/^\d{4}-\d{2}-\d{2}$/.test(value)) return undefined;
    const date = new Date(value + 'T00:00:00');
    return !Number.isNaN(+date) && localDate(date.toISOString()).slice(0, 10) === value
      ? value
      : undefined;
  }
  private update(params: Record<string, string | null>) {
    void this.router.navigate([], {
      relativeTo: this.route,
      queryParams: params,
      queryParamsHandling: 'merge',
    });
  }
  move(direction: -1 | 0 | 1) {
    const calendar = this.calendar()!.getApi();
    if (direction === 0) calendar.today();
    else if (direction < 0) calendar.prev();
    else calendar.next();
    this.update({ date: localDate(calendar.getDate().toISOString()).slice(0, 10) });
  }
  changeView(view: string) {
    this.update({ view });
  }
  select(task: Task) {
    this.calendar()!.getApi().gotoDate(task.plan.start);
    this.calendar()!.getApi().scrollToTime(localDate(task.plan.start).slice(11, 16));
    this.update({ task: task.id, date: localDate(task.plan.start).slice(0, 10) });
    this.panel.set('detail');
    this.mobilePart.set('panel');
  }
  clearSelection() {
    this.update({ task: null });
  }
  routerSettings() {
    void this.router.navigateByUrl('/settings');
  }
}
