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
import { MatCheckboxModule } from '@angular/material/checkbox';
import { MatDialog } from '@angular/material/dialog';
import { MatIconModule } from '@angular/material/icon';
import { MatTooltipModule } from '@angular/material/tooltip';
import { ActivatedRoute, Router } from '@angular/router';
import type { Task } from '@dopagaki/contract';
import type { Locale } from '@dopagaki/messages';
import {
  FullCalendarComponent,
  FullCalendarModule,
  type CalendarOptions,
  type EventInput,
  type LocaleSingularArg,
} from '@fullcalendar/angular';
import themePlugin from '@fullcalendar/angular/themes/classic';
import timeGridPlugin from '@fullcalendar/angular/timegrid';
import ja from 'fullcalendar/locales/ja';
import { map } from 'rxjs';
import { AuthService } from '../../../core/auth/auth.service';
import { LocaleStore } from '../../../core/i18n/locale.store';
import {
  DateTimePipe,
  MonthDayPipe,
  LongDatePipe,
  PlanMinutesPipe,
  TimePipe,
  localDateKey,
} from '../../../shared/ui/format';
import { AssistantPanel } from '../../assistant/assistant-panel';
import { Tutorial } from '../../onboarding/tutorial.service';
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
// 英語は FullCalendar に組み込まれているので、コードを渡すだけでよい。
const calendarLocales: Record<Locale, LocaleSingularArg> = { ja, en: 'en' };

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
    MatCheckboxModule,
    MatIconModule,
    MatTooltipModule,
    MonthDayPipe,
    LongDatePipe,
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
  private readonly locale = inject(LocaleStore).locale;
  protected readonly text = inject(LocaleStore).web;
  private readonly shared = inject(LocaleStore).shared;
  private readonly dialog = inject(MatDialog);
  private readonly router = inject(Router);
  private readonly route = inject(ActivatedRoute);
  private readonly tutorial = inject(Tutorial);
  private readonly fullCalendar = viewChild(FullCalendarComponent);

  protected readonly stylesReady = signal(false);
  protected readonly rendered = signal(false);
  protected readonly title = signal('');
  /** 表示している期間（FullCalendar の datesSet）。主カレンダーの予定はこの期間だけ読む。 */
  private readonly range = signal<{ readonly start: Date; readonly end: Date } | undefined>(
    undefined,
  );
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
    this.wallet.unlimited()
      ? this.text().calendar.common.support
      : this.text().calendar.common.availableToday,
  );
  protected readonly creditValue = computed(() =>
    this.wallet.unlimited()
      ? this.text().calendar.common.unlimited
      : this.shared().durationLabel(this.wallet.value()?.availableSeconds ?? 0),
  );
  /** Google の連携を使える環境（live）だけ、表示する予定を選べるようにする。 */
  protected readonly layersAvailable = computed(() => this.calendar.value()?.enabled === true);
  private readonly showTasks = computed(
    () => !this.layersAvailable() || this.calendar.layers().tasks,
  );
  private readonly showGoogle = computed(
    () =>
      this.layersAvailable() && this.calendar.layers().google && this.calendar.primaryReadable(),
  );
  private readonly googleEvents = computed<EventInput[]>(() => {
    if (!this.showGoogle()) return [];
    const untitled = this.text().calendar.workspace.untitled;
    return this.calendar.primaryEvents().flatMap((event) => {
      // 仕様では、終日なら startDate・endDate、それ以外は start・end が入る。
      const start = event.allDay ? event.startDate : event.start;
      const end = event.allDay ? event.endDate : event.end;
      if (start === undefined || end === undefined) return [];
      return {
        // 課題の ID と重ならないようにする（クリックで課題を探すときに見つからないようにする）。
        id: `google:${event.id}`,
        title: event.title || untitled,
        allDay: event.allDay,
        start,
        end,
        // 表示するだけなので、選んだり開いたりできないようにする。
        interactive: false,
        classNames: ['google-event'],
      };
    });
  });
  protected readonly events = computed<EventInput[]>(() => {
    const selected = this.selected()?.id;
    const tasks = this.showTasks()
      ? this.tasks.tasks().map((task) => ({
          id: task.id,
          title: task.title,
          start: task.plan.start,
          end: task.plan.end,
          classNames: [
            'workspace-event',
            task.status,
            ...(task.id === selected ? ['selected'] : []),
          ],
        }))
      : [];
    return [...tasks, ...this.googleEvents()];
  });
  /** 終日の予定があるときだけ、終日の欄を出す（課題は時刻を持つので、ふだんは要らない）。 */
  private readonly allDaySlot = computed(() => this.googleEvents().some((event) => event.allDay));
  protected readonly syncing = computed(() =>
    this.calendar.connected() ? this.calendar.sync.pending() : this.tasks.load.pending(),
  );

  private readonly baseOptions: CalendarOptions = {
    plugins: [themePlugin, timeGridPlugin],
    initialView: this.currentView() === 'day' ? 'timeGridDay' : 'timeGridWeek',
    initialDate: validDate(this.date()) ?? this.selected()?.plan.start ?? new Date(),
    headerToolbar: false,
    firstDay: 1,
    // 高さは中身に合わせ、スクロールは外側の .calendar-scroll で行う（テンプレートのコメントを参照）。
    height: 'auto',
    nowIndicator: true,
    editable: false,
    eventInteractive: true,
    slotDuration: '00:30:00',
    eventMinHeight: 36,
    slotEventOverlap: false,
    eventTimeFormat: { hour: '2-digit', minute: '2-digit', hour12: false },
    views: {
      // 週表示では30分の枠を予定の最小の高さと同じにする。続けて並ぶ予定が重ならず、
      // 列の幅いっぱいに課題名を出せる（時刻はテンプレートで週表示だけ省く）。
      timeGridWeek: { slotMinHeight: 36 },
    },
    slotHeaderFormat: { hour: '2-digit', minute: '2-digit', hour12: false },
    eventClick: (info) => {
      const task = this.tasks.find(info.event.id);
      if (task) this.select(task);
    },
    datesSet: (info) => {
      this.title.set(info.view.title);
      this.range.set({ start: info.start, end: info.end });
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
  /**
   * FullCalendar は options の各値を前回と比べ、変わったものだけを反映する。
   * ほかの値は同じものを渡し、言語を切り替えたときは locale だけを変える（表示期間の見出しは datesSet で取り直される）。
   */
  protected readonly options = computed<CalendarOptions>(() => ({
    ...this.baseOptions,
    locale: calendarLocales[this.locale()],
    allDaySlot: this.allDaySlot(),
  }));

  constructor() {
    // 狭い画面ではカレンダーと相談の片方しか出ない。使い方の案内が強調する側を表示する。
    effect(() => {
      const target = this.tutorial.target();
      untracked(() => {
        if (target === 'assistant') {
          this.panel.set('assistant');
          this.mobilePart.set('panel');
        } else if (target === 'calendar') {
          this.mobilePart.set('calendar');
        }
      });
    });
    // 表示する期間が変わったら、主カレンダーの予定を読み直す。
    effect(() => {
      const range = this.range();
      if (!range || !this.showGoogle()) return;
      untracked(() => void this.calendar.loadPrimaryEvents(range.start, range.end));
    });
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
    const start = new Date(task.plan.start);
    api?.scrollToTime({ hours: start.getHours(), minutes: start.getMinutes() });
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
    const range = this.range();
    if (range && this.showGoogle()) void this.calendar.loadPrimaryEvents(range.start, range.end);
  }

  /** 作業を開始する。失敗は押した場所の近くに出す（テンプレートが `startOrigin` で選ぶ）。 */
  protected async start(task: Task, origin: StartOrigin): Promise<void> {
    this.startOrigin.set(origin);
    await this.actions.start(task);
  }

  protected startLabel(task: Task): string {
    const text = this.text().calendar.workspace;
    if (task.status === 'active') return text.active;
    if (task.status === 'completed') return text.completed;
    return text.start;
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
