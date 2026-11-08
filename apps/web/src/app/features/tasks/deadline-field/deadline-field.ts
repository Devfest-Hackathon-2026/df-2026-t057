import {
  ChangeDetectionStrategy,
  Component,
  computed,
  forwardRef,
  inject,
  signal,
} from '@angular/core';
import { NG_VALUE_ACCESSOR, type ControlValueAccessor } from '@angular/forms';
import { MatButtonModule } from '@angular/material/button';
import { MatDatepickerModule } from '@angular/material/datepicker';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatInputModule } from '@angular/material/input';
import { MatSelectModule } from '@angular/material/select';
import type { WebMessages } from '@dopagaki/messages';
import { LocaleStore } from '../../../core/i18n/locale.store';
import { addDays, dateKey } from '../local-datetime';

const pad = (value: number): string => String(value).padStart(2, '0');

/** 15分ごとと、「その日のうちに」の 23:59。 */
const times: readonly string[] = [
  ...Array.from(
    { length: 96 },
    (_, index) => `${pad(Math.floor(index / 4))}:${pad((index % 4) * 15)}`,
  ),
  '23:59',
].sort();

interface QuickDate {
  readonly label: string;
  readonly key: string;
}

interface Summary {
  readonly past: boolean;
  readonly text: string;
}

type DeadlineFieldText = WebMessages['tasks']['deadlineField'];

/** 締切までの残りを「あとN日M時間」などで表す（選んだ値の表示だけ）。 */
export function remainingLabel(milliseconds: number, text: DeadlineFieldText): string {
  const hours = Math.floor(milliseconds / 3_600_000);
  if (hours >= 24) return text.remainingDays(Math.floor(hours / 24), hours % 24);
  if (hours >= 1) return text.remainingHours(hours);
  return text.remainingMinutes(Math.max(1, Math.round(milliseconds / 60_000)));
}

/** 今日を基準にした日付のすばやい選択肢。「今週末」は月〜木曜だけ出す。 */
export function quickDates(today: Date, text: DeadlineFieldText): QuickDate[] {
  const untilSunday = (7 - today.getDay()) % 7;
  return [
    { label: text.today, key: dateKey(today) },
    { label: text.tomorrow, key: dateKey(addDays(today, 1)) },
    { label: text.dayAfterTomorrow, key: dateKey(addDays(today, 2)) },
    ...(untilSunday > 2
      ? [{ label: text.thisWeekend, key: dateKey(addDays(today, untilSunday)) }]
      : []),
    { label: text.inAWeek, key: dateKey(addDays(today, 7)) },
  ];
}

/**
 * 締切の入力。日付（すばやい選択・カレンダー）と15分刻みの時刻を選ぶ。
 * 値はこの端末の `YYYY-MM-DDTHH:mm`。
 */
@Component({
  selector: 'app-deadline-field',
  imports: [
    MatButtonModule,
    MatDatepickerModule,
    MatFormFieldModule,
    MatInputModule,
    MatSelectModule,
  ],
  templateUrl: './deadline-field.html',
  styleUrl: './deadline-field.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
  providers: [
    { provide: NG_VALUE_ACCESSOR, useExisting: forwardRef(() => DeadlineField), multi: true },
  ],
})
export class DeadlineField implements ControlValueAccessor {
  private readonly locale = inject(LocaleStore);
  protected readonly text = this.locale.web;
  protected readonly value = signal('');
  protected readonly disabled = signal(false);
  protected readonly today = addDays(new Date(), 0);
  protected readonly quick = computed(() =>
    quickDates(this.today, this.text().tasks.deadlineField),
  );
  private readonly summaryFormat = computed(
    () =>
      new Intl.DateTimeFormat(this.locale.intlLocale(), {
        month: 'long',
        day: 'numeric',
        weekday: 'short',
        hour: '2-digit',
        minute: '2-digit',
        hour12: false,
      }),
  );

  protected readonly date = computed(() => this.value().slice(0, 10));
  protected readonly time = computed(() => this.value().slice(11, 16) || '23:59');
  protected readonly dateObject = computed(() => {
    const [year, month, day] = this.date().split('-').map(Number);
    return year && month && day ? new Date(year, month - 1, day) : null;
  });
  protected readonly times = computed(() =>
    times.includes(this.time()) ? times : [...times, this.time()].sort(),
  );
  protected readonly summary = computed<Summary | null>(() => {
    const target = new Date(this.value());
    if (Number.isNaN(target.getTime())) return null;
    const text = this.text().tasks.deadlineField;
    const deadline = this.summaryFormat().format(target);
    const left = target.getTime() - Date.now();
    if (left <= 0) return { past: true, text: text.past(deadline) };
    return { past: false, text: text.until(deadline, remainingLabel(left, text)) };
  });

  private onChange: (value: string) => void = () => undefined;
  protected onTouched: () => void = () => undefined;

  writeValue(value: unknown): void {
    this.value.set(typeof value === 'string' ? value : '');
  }

  registerOnChange(fn: (value: string) => void): void {
    this.onChange = fn;
  }

  registerOnTouched(fn: () => void): void {
    this.onTouched = fn;
  }

  setDisabledState(disabled: boolean): void {
    this.disabled.set(disabled);
  }

  protected setDate(key: string): void {
    this.update(`${key}T${this.time()}`);
  }

  protected pickDate(date: Date | null): void {
    if (date && !Number.isNaN(date.getTime())) this.setDate(dateKey(date));
  }

  protected setTime(time: string): void {
    this.update(`${this.date()}T${time}`);
  }

  private update(value: string): void {
    this.value.set(value);
    this.onChange(value);
    this.onTouched();
  }
}
