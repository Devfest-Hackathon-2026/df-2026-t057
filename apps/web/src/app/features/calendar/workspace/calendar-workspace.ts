import { BreakpointObserver } from '@angular/cdk/layout';
import type { ElementRef } from '@angular/core';
import {
  ChangeDetectionStrategy,
  Component,
  computed,
  effect,
  inject,
  input,
  linkedSignal,
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
/** 「はじめる」を押した場所。失敗はその近くに出す。 */
type StartOrigin = 'next' | 'detail';
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

  private readonly scroller = viewChild<ElementRef<HTMLElement>>('scroller');
  private scrolledToNow = false;
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
  protected readonly startOrigin = signal<StartOrigin | undefined>(undefined);
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
  private readonly selectedId = computed(() => this.selected()?.id);
  /** 課題が選ばれたら詳細を開く。選択が外れたら相談に戻す。その後の切り替えは利用者に任せる。 */
  protected readonly panel = linkedSignal<string | undefined, 'assistant' | 'detail'>({
    source: this.selectedId,
    computation: (id) => (id ? 'detail' : 'assistant'),
  });
  /** 狭い画面では、課題が選ばれたらパネル側を表示する。 */
  protected readonly mobilePart = linkedSignal<string | undefined, 'calendar' | 'panel'>({
    source: this.selectedId,
    computation: (id, previous) => (id ? 'panel' : (previous?.value ?? 'calendar')),
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
    // 高さは中身に合わせ、スクロールは外側の .calendar-scroll で行う（テンプレートのコメントを参照）。
    height: 'auto',
    allDaySlot: false,
    nowIndicator: true,
    editable: false,
    eventInteractive: true,
    slotDuration: '00:30:00',
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
    // 開いたときは、現在時刻の1時間前あたりから表示する。
    nowIndicatorLineDidMount: (info) => {
      if (this.scrolledToNow) return;
      this.scrolledToNow = true;
      requestAnimationFrame(() => {
        const scroller = this.scroller()?.nativeElement;
        if (!scroller) return;
        const offset = info.el.getBoundingClientRect().top - scroller.getBoundingClientRect().top;
        scroller.scrollTop += Math.max(0, offset - scroller.clientHeight / 4);
      });
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

  /** 作業を開始する。失敗は押した場所の近くに出す（テンプレートが `startOrigin` で選ぶ）。 */
  protected async start(task: Task, origin: StartOrigin): Promise<void> {
    this.startOrigin.set(origin);
    await this.actions.start(task);
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
