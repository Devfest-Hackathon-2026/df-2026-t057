import {
  ChangeDetectionStrategy,
  Component,
  computed,
  forwardRef,
  signal,
  viewChild,
} from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import {
  FormControl,
  ReactiveFormsModule,
  Validators,
  NG_VALIDATORS,
  NG_VALUE_ACCESSOR,
  type AbstractControl,
  type ControlValueAccessor,
  type ValidationErrors,
  type Validator,
} from '@angular/forms';
import { MatAutocomplete, MatAutocompleteModule } from '@angular/material/autocomplete';
import { MatButtonModule } from '@angular/material/button';
import { MatDatepickerModule } from '@angular/material/datepicker';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatInputModule } from '@angular/material/input';
import { MatIconModule } from '@angular/material/icon';
import { addDays, dateKey, fromLocalInput } from '../local-datetime';

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

/** 締切までの残りを「あとN日M時間」などで表す（選んだ値の表示だけ）。 */
export function remainingLabel(milliseconds: number): string {
  const hours = Math.floor(milliseconds / 3_600_000);
  if (hours >= 24) {
    const rest = hours % 24;
    return `あと${Math.floor(hours / 24)}日${rest ? `${rest}時間` : ''}`;
  }
  if (hours >= 1) return `あと${hours}時間`;
  return `あと${Math.max(1, Math.round(milliseconds / 60_000))}分`;
}

/** 今日を基準にした日付のすばやい選択肢。「今週末」は月〜木曜だけ出す。 */
export function quickDates(today: Date): QuickDate[] {
  const untilSunday = (7 - today.getDay()) % 7;
  return [
    { label: '今日', key: dateKey(today) },
    { label: '明日', key: dateKey(addDays(today, 1)) },
    { label: '明後日', key: dateKey(addDays(today, 2)) },
    ...(untilSunday > 2 ? [{ label: '今週末', key: dateKey(addDays(today, untilSunday)) }] : []),
    { label: '1週間後', key: dateKey(addDays(today, 7)) },
  ];
}

const summaryFormat = new Intl.DateTimeFormat('ja-JP', {
  month: 'long',
  day: 'numeric',
  weekday: 'short',
  hour: '2-digit',
  minute: '2-digit',
  hour12: false,
});

/**
 * 締切の入力。日付と時刻の候補を選ぶか、1分単位で時刻を入力する。
 * 値はこの端末の `YYYY-MM-DDTHH:mm`。
 */
@Component({
  selector: 'app-deadline-field',
  imports: [
    MatButtonModule,
    MatDatepickerModule,
    MatFormFieldModule,
    MatInputModule,
    MatAutocompleteModule,
    MatIconModule,
    ReactiveFormsModule,
  ],
  templateUrl: './deadline-field.html',
  styleUrl: './deadline-field.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
  providers: [
    { provide: NG_VALUE_ACCESSOR, useExisting: forwardRef(() => DeadlineField), multi: true },
    { provide: NG_VALIDATORS, useExisting: forwardRef(() => DeadlineField), multi: true },
  ],
})
export class DeadlineField implements ControlValueAccessor, Validator {
  protected readonly value = signal('');
  protected readonly disabled = signal(false);
  protected readonly today = addDays(new Date(), 0);
  protected readonly quick = quickDates(new Date());
  protected readonly timePattern = /^(?:[01]\d|2[0-3]):[0-5]\d$/;
  protected readonly timeControl = new FormControl('', {
    nonNullable: true,
    validators: [
      (control: AbstractControl): ValidationErrors | null => Validators.required(control),
      Validators.pattern(this.timePattern),
    ],
  });
  private readonly autocomplete = viewChild(MatAutocomplete);

  protected readonly date = computed(() => this.value().slice(0, 10));
  protected readonly time = computed(() => this.value().slice(11));
  protected readonly dateObject = computed(() => {
    const [year, month, day] = this.date().split('-').map(Number);
    return year && month && day ? new Date(year, month - 1, day) : null;
  });
  protected readonly times = computed(() =>
    !this.timePattern.test(this.time()) || times.includes(this.time())
      ? times
      : [...times, this.time()].sort(),
  );
  protected readonly summary = computed<Summary | null>(() => {
    const iso = fromLocalInput(this.value());
    if (!iso) return null;
    const target = new Date(iso);
    const text = summaryFormat.format(target);
    const left = target.getTime() - Date.now();
    if (left <= 0)
      return { past: true, text: `${text} は過ぎています。未来の日時を選んでください。` };
    return { past: false, text: `${text} まで（${remainingLabel(left)}）` };
  });

  private onChange: (value: string) => void = () => undefined;
  protected onTouched: () => void = () => undefined;

  constructor() {
    this.timeControl.valueChanges.pipe(takeUntilDestroyed()).subscribe((time) => {
      this.setTime(time);
    });
  }

  writeValue(value: unknown): void {
    this.value.set(typeof value === 'string' ? value : '');
    this.timeControl.setValue(this.time(), { emitEvent: false });
  }

  registerOnChange(fn: (value: string) => void): void {
    this.onChange = fn;
  }

  registerOnTouched(fn: () => void): void {
    this.onTouched = fn;
  }

  validate(control: AbstractControl): ValidationErrors | null {
    const value: unknown = control.value;
    return typeof value === 'string' && fromLocalInput(value) ? null : { datetime: true };
  }

  setDisabledState(disabled: boolean): void {
    this.disabled.set(disabled);
    if (disabled) this.timeControl.disable({ emitEvent: false });
    else this.timeControl.enable({ emitEvent: false });
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

  protected scrollToTime(): void {
    // 候補を絞らず、現在の入力付近から選べるようにする。
    requestAnimationFrame(() => {
      const panel = this.autocomplete()?.panel.nativeElement as HTMLElement | undefined;
      if (this.timePattern.test(this.time())) {
        panel?.querySelector(`[data-time="${this.time()}"]`)?.scrollIntoView({ block: 'center' });
      }
    });
  }

  private update(value: string): void {
    this.value.set(value);
    if (this.timeControl.value !== this.time()) {
      this.timeControl.setValue(this.time(), { emitEvent: false });
    }
    this.onChange(value);
    this.onTouched();
  }
}
