import { BreakpointObserver } from '@angular/cdk/layout';
import {
  ChangeDetectionStrategy,
  Component,
  computed,
  effect,
  inject,
  input,
  signal,
  untracked,
  viewChild,
} from '@angular/core';
import { toSignal } from '@angular/core/rxjs-interop';
import { MatButtonModule } from '@angular/material/button';
import { MatButtonToggleModule } from '@angular/material/button-toggle';
import { MatDialog } from '@angular/material/dialog';
import { MatIconModule } from '@angular/material/icon';
import { MatTooltipModule } from '@angular/material/tooltip';
import { ActivatedRoute, Router } from '@angular/router';
import type { Task } from '@dopagaki/contract';
import { durationLabel } from '@dopagaki/messages';
import {
  FullCalendarComponent,
  FullCalendarModule,
  type CalendarOptions,
} from '@fullcalendar/angular';
import themePlugin from '@fullcalendar/angular/themes/classic';
import timeGridPlugin from '@fullcalendar/angular/timegrid';
import ja from 'fullcalendar/locales/ja';
import { map } from 'rxjs';
import { AuthService } from '../../../core/auth/auth.service';
import {
  DateTimePipe,
  MonthDayPipe,
  PlanMinutesPipe,
  TimePipe,
  formatTime,
  localDateKey,
} from '../../../shared/ui/format';
import { AssistantPanel } from '../../assistant/assistant-panel';
import { TaskActions } from '../../tasks/task-actions';
import { TasksStore } from '../../tasks/tasks.store';
import { WalletStore } from '../../wallet/wallet.store';
import { WorkSessionStore } from '../../work/work-session.store';
import { CalendarStore } from '../calendar.store';
import { CreditInfoDialog } from './credit-info-dialog';
import { loadFullCalendarStyles } from './fullcalendar-styles';

type View = 'day' | 'week';
const compactQuery = '(max-width: 1100px)';

/** `YYYY-MM-DD` として正しい日付なら返す。 */
export function validDate(value: string | undefined): string | undefined {
  if (!value || !/^\d{4}-\d{2}-\d{2}$/.test(value)) return undefined;
  const date = new Date(`${value}T00:00:00`);
  return !Number.isNaN(date.getTime()) && localDateKey(date) === value ? value : undefined;
}

/** カレンダー（ホーム）。視聴できる時間・次の課題・予定のカレンダー・相談と詳細。 */
@Component({
  selector: 'app-calendar-workspace',
  imports: [
    AssistantPanel,
    DateTimePipe,
    FullCalendarModule,
    MatButtonModule,
    MatButtonToggleModule,
    MatIconModule,
    MatTooltipModule,
    MonthDayPipe,
    PlanMinutesPipe,
    TimePipe,
  ],
  templateUrl: './calendar-workspace.html',
  styleUrl: './calendar-workspace.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class CalendarWorkspace {
  /** URL のクエリ（withComponentInputBinding）。 */
  readonly task = input<string>();
  readonly date = input<string>();
  readonly view = input<string>();

  protected readonly tasks = inject(TasksStore);
  protected readonly wallet = inject(WalletStore);
  protected readonly work = inject(WorkSessionStore);
  protected readonly calendar = inject(CalendarStore);
  protected readonly actions = inject(TaskActions);
  protected readonly auth = inject(AuthService);
  private readonly dialog = inject(MatDialog);
  private readonly router = inject(Router);
  private readonly route = inject(ActivatedRoute);
  private readonly fullCalendar = viewChild(FullCalendarComponent);

  protected readonly stylesReady = signal(false);
  protected readonly rendered = signal(false);
  protected readonly title = signal('');
  protected readonly panel = signal<'assistant' | 'detail'>('assistant');
  protected readonly mobilePart = signal<'calendar' | 'panel'>('calendar');
  protected readonly timeZone = Intl.DateTimeFormat().resolvedOptions().timeZone;

  private readonly compact = toSignal(
    inject(BreakpointObserver)
      .observe(compactQuery)
      .pipe(map((state) => state.matches)),
    { initialValue: typeof matchMedia === 'function' && matchMedia(compactQuery).matches },
  );

  protected readonly currentView = computed<View>(() => {
    const view = this.view();
    if (view === 'day' || view === 'week') return view;
    return this.compact() ? 'day' : 'week';
  });
  protected readonly selected = computed(() => {
    const id = this.task();
    return id ? (this.tasks.find(id) ?? null) : null;
  });
  protected readonly next = this.tasks.next;
  protected readonly creditLabel = computed(() =>
    this.wallet.unlimited() ? '視聴の支援' : '今日、視聴できる時間',
  );
  protected readonly creditValue = computed(() =>
    this.wallet.unlimited()
      ? '制限なし'
      : durationLabel(this.wallet.value()?.availableSeconds ?? 0),
  );
  protected readonly events = computed(() => {
    const selected = this.selected()?.id;
    return this.tasks.tasks().map((task) => ({
      id: task.id,
      title: task.title,
      start: task.plan.start,
      end: task.plan.end,
      classNames: ['workspace-event', task.status, ...(task.id === selected ? ['selected'] : [])],
    }));
  });
  protected readonly syncing = computed(() =>
    this.calendar.connected() ? this.calendar.sync.pending() : this.tasks.load.pending(),
  );

  protected readonly options: CalendarOptions = {
    plugins: [themePlugin, timeGridPlugin],
    locale: ja,
    initialView: this.currentView() === 'day' ? 'timeGridDay' : 'timeGridWeek',
    initialDate: validDate(this.date()) ?? this.selected()?.plan.start ?? new Date(),
    headerToolbar: false,
    firstDay: 1,
    height: 650,
    allDaySlot: false,
    nowIndicator: true,
    editable: false,
    eventInteractive: true,
    slotDuration: '00:30:00',
    scrollTime: `${String(Math.max(0, new Date().getHours() - 1)).padStart(2, '0')}:00:00`,
    eventMinHeight: 36,
    slotEventOverlap: false,
    eventTimeFormat: { hour: '2-digit', minute: '2-digit', hour12: false },
    slotHeaderFormat: { hour: '2-digit', minute: '2-digit', hour12: false },
    eventClick: (info) => {
      const task = this.tasks.find(info.event.id);
      if (task) this.select(task);
    },
    datesSet: (info) => {
      this.title.set(info.view.title);
      this.rendered.set(true);
    },
  };

  constructor() {
    void loadFullCalendarStyles().then(() => {
      this.stylesReady.set(true);
    });
    // URL の表示期間・日付に、カレンダーを合わせる。
    effect(() => {
      const api = this.fullCalendar()?.getApi();
      if (!api || !this.rendered()) return;
      const view = this.currentView() === 'day' ? 'timeGridDay' : 'timeGridWeek';
      const date = validDate(this.date());
      untracked(() => {
        if (api.view.type !== view) api.changeView(view);
        if (date && localDateKey(api.getDate()) !== date) api.gotoDate(date);
      });
    });
    // 新しく課題が選ばれたら、詳細を開く。
    let lastSelected: string | undefined;
    effect(() => {
      const id = this.selected()?.id;
      if (id && id !== lastSelected) {
        this.panel.set('detail');
        this.mobilePart.set('panel');
      }
      if (!id && this.panel() === 'detail') this.panel.set('assistant');
      lastSelected = id;
    });
  }

  protected openCreditInfo(): void {
    this.dialog.open(CreditInfoDialog, { width: '26rem', maxWidth: 'calc(100vw - 2rem)' });
  }

  protected move(direction: -1 | 0 | 1): void {
    const api = this.fullCalendar()?.getApi();
    if (!api) return;
    if (direction === 0) api.today();
    else if (direction < 0) api.prev();
    else api.next();
    this.updateQuery({ date: localDateKey(api.getDate()) });
  }

  protected changeView(view: View): void {
    this.updateQuery({ view });
  }

  protected select(task: Task): void {
    const api = this.fullCalendar()?.getApi();
    api?.gotoDate(task.plan.start);
    api?.scrollToTime(formatTime(task.plan.start));
    this.updateQuery({ task: task.id, date: localDateKey(task.plan.start) });
    this.panel.set('detail');
    this.mobilePart.set('panel');
  }

  protected clearSelection(): void {
    this.updateQuery({ task: null });
  }

  protected openSettings(): void {
    void this.router.navigateByUrl('/settings');
  }

  protected sync(): void {
    if (this.calendar.connected()) void this.calendar.syncNow();
    else void this.tasks.refresh();
  }

  protected startLabel(task: Task): string {
    if (task.status === 'active') return '作業中';
    if (task.status === 'completed') return '完了済み';
    return 'はじめる';
  }

  protected sameDay(start: string, end: string): boolean {
    return localDateKey(start) === localDateKey(end);
  }

  private updateQuery(queryParams: Record<string, string | null>): void {
    void this.router.navigate([], {
      relativeTo: this.route,
      queryParams,
      queryParamsHandling: 'merge',
    });
  }
}
