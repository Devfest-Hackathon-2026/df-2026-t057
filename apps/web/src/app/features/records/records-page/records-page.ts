import type { ElementRef } from '@angular/core';
import {
  ChangeDetectionStrategy,
  Component,
  afterRenderEffect,
  computed,
  inject,
  signal,
  viewChild,
} from '@angular/core';
import { RouterLink } from '@angular/router';
import { MatButtonModule } from '@angular/material/button';
import { MatIconModule } from '@angular/material/icon';
import { MatProgressBarModule } from '@angular/material/progress-bar';
import { MatTooltipModule } from '@angular/material/tooltip';
import type { LedgerEntry, Records } from '@dopagaki/contract';
import { LocaleStore } from '../../../core/i18n/locale.store';
import { DateTimePipe, DurationPipe } from '../../../shared/ui/format';
import { PageHeading } from '../../../shared/ui/page-heading';
import { LedgerStore } from '../../wallet/ledger.store';
import { WalletStore } from '../../wallet/wallet.store';
import { RecordsStore } from '../records.store';

type RecordDay = Records['days'][number];

export interface DayCell {
  readonly date: string;
  readonly completed: number;
  readonly earnedSeconds: number;
  readonly index: number;
}

interface DateParts {
  readonly year: number;
  readonly month: number;
  readonly day: number;
}

/** `YYYY-MM-DD` を分解する。日付はサーバーが本人のタイムゾーンで区切ったものなので、時刻の計算をしない。 */
export function dateParts(date: string): DateParts {
  const [year = 0, month = 1, day = 1] = date.split('-').map(Number);
  return { year, month, day };
}

/** その日の曜日（0 = 日曜）。 */
export function weekday(date: string): number {
  const { year, month, day } = dateParts(date);
  return new Date(Date.UTC(year, month - 1, day)).getUTCDay();
}

/** 日曜始まりの縦7マスの列に並べる。最初の週の、記録より前の日は空ける。 */
export function layoutDays(days: readonly RecordDay[]): (DayCell | null)[] {
  const first = days[0];
  const lead = first ? weekday(first.date) : 0;
  return [
    ...Array.from({ length: lead }, () => null),
    ...days.map((day, index) => ({ ...day, index })),
  ];
}

/** 日付をその日のまま表示するため、UTC の0時として作り、UTC で書式にする（端末のタイムゾーンでずらさない）。 */
function utcDate(date: string): Date {
  const { year, month, day } = dateParts(date);
  return new Date(Date.UTC(year, month - 1, day));
}

/** 記録。集計はサーバーが行い、ここでは並べて表示するだけ。 */
@Component({
  selector: 'app-records-page',
  imports: [
    DateTimePipe,
    DurationPipe,
    MatButtonModule,
    MatIconModule,
    MatProgressBarModule,
    MatTooltipModule,
    PageHeading,
    RouterLink,
  ],
  templateUrl: './records-page.html',
  styleUrl: './records-page.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class RecordsPage {
  protected readonly records = inject(RecordsStore);
  protected readonly ledger = inject(LedgerStore);
  protected readonly wallet = inject(WalletStore);
  protected readonly text = inject(LocaleStore).web;
  protected readonly shared = inject(LocaleStore).shared;
  private readonly intlLocale = inject(LocaleStore).intlLocale;
  /** マスごとに作ると重いので、言語ごとに1組だけ作る。 */
  private readonly dateFormats = computed(() => {
    const locale = this.intlLocale();
    return {
      full: new Intl.DateTimeFormat(locale, {
        year: 'numeric',
        month: 'numeric',
        day: 'numeric',
        timeZone: 'UTC',
      }),
      monthDay: new Intl.DateTimeFormat(locale, {
        month: 'numeric',
        day: 'numeric',
        timeZone: 'UTC',
      }),
      long: new Intl.DateTimeFormat(locale, { month: 'long', day: 'numeric', timeZone: 'UTC' }),
    };
  });
  private readonly grassScroll = viewChild<ElementRef<HTMLElement>>('grassScroll');

  protected readonly days = computed(() => this.records.value()?.days ?? []);
  protected readonly cells = computed(() => layoutDays(this.days()));
  protected readonly weeks = computed(() => Math.ceil(this.cells().length / 7));
  private readonly selectedIndex = signal<number | undefined>(undefined);
  protected readonly selected = computed(() => {
    const days = this.days();
    const index = this.selectedIndex() ?? days.length - 1;
    const day = days[index];
    return day ? { ...day, index } : undefined;
  });
  protected readonly entries = computed<readonly LedgerEntry[]>(() => [
    ...(this.ledger.value()?.entries ?? []),
    ...this.ledger.older(),
  ]);

  protected readonly practiceHistory = computed(() =>
    (this.records.value()?.practice.history ?? []).filter(
      (day) => day.workSeconds > 0 || day.viewedSeconds > 0,
    ),
  );
  protected readonly recordedWeeks = computed(() =>
    (this.records.value()?.weeks ?? [])
      .filter((week) => week.starts > 0 || week.workSeconds > 0 || week.snoozes > 0)
      .toReversed(),
  );

  constructor() {
    this.records.ensureLoaded();
    this.ledger.ensureLoaded();
    this.wallet.ensureLoaded();
    // 狭い画面でマスが収まらないときは、直近の週が見えるよう右端から始める（表示されたときに一度だけ）。
    afterRenderEffect(() => {
      const scroller = this.grassScroll()?.nativeElement;
      if (scroller) scroller.scrollLeft = scroller.scrollWidth;
    });
  }

  protected dayLabel(cell: DayCell): string {
    return this.text().records.page.dayLabel(
      this.dateFormats().full.format(utcDate(cell.date)),
      cell.completed,
      this.shared().durationLabel(cell.earnedSeconds),
    );
  }

  protected monthDay(date: string): string {
    return this.dateFormats().monthDay.format(utcDate(date));
  }

  protected longDay(date: string): string {
    return this.dateFormats().long.format(utcDate(date));
  }

  protected select(index: number): void {
    this.selectedIndex.set(index);
  }

  /** 矢印キーで日を移動する（上下は1日、左右は1週）。 */
  protected move(event: KeyboardEvent, index: number): void {
    const steps: Partial<Record<string, number>> = {
      ArrowUp: -1,
      ArrowDown: 1,
      ArrowLeft: -7,
      ArrowRight: 7,
      Home: -index,
      End: this.days().length - 1 - index,
    };
    const step = steps[event.key];
    if (step === undefined) return;
    event.preventDefault();
    const next = Math.min(this.days().length - 1, Math.max(0, index + step));
    this.selectedIndex.set(next);
    const grid = (event.currentTarget as HTMLElement).parentElement;
    grid?.querySelector<HTMLButtonElement>(`[data-index="${next}"]`)?.focus();
  }

  protected entryTitle(entry: LedgerEntry): string {
    return entry.taskTitle ?? this.shared().ledgerKinds[entry.kind];
  }

  protected amount(entry: LedgerEntry): string {
    const text = this.text().records.page;
    const duration = this.shared().durationLabel(Math.abs(entry.seconds));
    return entry.seconds < 0 ? text.loss(duration) : text.gain(duration);
  }
}
