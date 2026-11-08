// 日時と時間の表示。日時はこの端末のタイムゾーンで、画面の言語に合わせて表示する。
import { Pipe, inject, type PipeTransform } from '@angular/core';
import type { Locale } from '@dopagaki/messages';
import { LocaleStore } from '../../core/i18n/locale.store';

interface Formatters {
  time: Intl.DateTimeFormat;
  monthDay: Intl.DateTimeFormat;
  longDate: Intl.DateTimeFormat;
  fullDate: Intl.DateTimeFormat;
}

function formattersFor(intlLocale: string): Formatters {
  return {
    time: new Intl.DateTimeFormat(intlLocale, {
      hour: '2-digit',
      minute: '2-digit',
      hour12: false,
    }),
    monthDay: new Intl.DateTimeFormat(intlLocale, { month: 'numeric', day: 'numeric' }),
    longDate: new Intl.DateTimeFormat(intlLocale, {
      month: 'long',
      day: 'numeric',
      weekday: 'short',
    }),
    fullDate: new Intl.DateTimeFormat(intlLocale, {
      month: 'long',
      day: 'numeric',
      weekday: 'long',
    }),
  };
}

// Intl.DateTimeFormat は作るのが重いので、言語ごとに1組だけ作って使い回す。
const formatters: Record<Locale, Formatters> = {
  ja: formattersFor('ja-JP'),
  en: formattersFor('en-US'),
};

function toDate(value: string | number | Date): Date {
  return value instanceof Date ? value : new Date(value);
}

/** `HH:mm` */
export function formatTime(value: string | number | Date, locale: Locale): string {
  return formatters[locale].time.format(toDate(value));
}

/** `M/d` */
export function formatMonthDay(value: string | number | Date, locale: Locale): string {
  return formatters[locale].monthDay.format(toDate(value));
}

/** `M/d HH:mm` */
export function formatDateTime(value: string | number | Date, locale: Locale): string {
  return `${formatMonthDay(value, locale)} ${formatTime(value, locale)}`;
}

/** 日本語は `M月d日(曜)`、英語は `Sun, November 8` */
export function formatLongDate(value: string | number | Date, locale: Locale): string {
  const format = formatters[locale].longDate;
  if (locale === 'en') return format.format(toDate(value));
  const parts = format.formatToParts(toDate(value));
  const part = (type: Intl.DateTimeFormatPartTypes): string =>
    parts.find((item) => item.type === type)?.value ?? '';
  return `${part('month')}月${part('day')}日(${part('weekday')})`;
}

/** 日本語は `M月d日 曜日`、英語は `Sunday, November 8` */
export function formatFullDate(value: string | number | Date, locale: Locale): string {
  return formatters[locale].fullDate.format(toDate(value));
}

/** 予定の長さ（分、切り上げ）。 */
export function planMinutes(plan: { readonly start: string; readonly end: string }): number {
  return Math.max(1, Math.ceil((Date.parse(plan.end) - Date.parse(plan.start)) / 60_000));
}

/** `YYYY-MM-DD`（この端末の日付）。 */
export function localDateKey(value: string | number | Date): string {
  const date = toDate(value);
  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')}`;
}

// 日時と時間のパイプは、言語を変えたときに表示し直すため pure: false にする（言語は signal から読む）。

@Pipe({ name: 'time', pure: false })
export class TimePipe implements PipeTransform {
  private readonly locale = inject(LocaleStore).locale;

  transform(value: string | number | Date): string {
    return formatTime(value, this.locale());
  }
}

@Pipe({ name: 'monthDay', pure: false })
export class MonthDayPipe implements PipeTransform {
  private readonly locale = inject(LocaleStore).locale;

  transform(value: string | number | Date): string {
    return formatMonthDay(value, this.locale());
  }
}

@Pipe({ name: 'dateTime', pure: false })
export class DateTimePipe implements PipeTransform {
  private readonly locale = inject(LocaleStore).locale;

  transform(value: string | number | Date): string {
    return formatDateTime(value, this.locale());
  }
}

@Pipe({ name: 'longDate', pure: false })
export class LongDatePipe implements PipeTransform {
  private readonly locale = inject(LocaleStore).locale;

  transform(value: string | number | Date): string {
    return formatLongDate(value, this.locale());
  }
}

@Pipe({ name: 'duration', pure: false })
export class DurationPipe implements PipeTransform {
  private readonly shared = inject(LocaleStore).shared;

  transform(seconds: number): string {
    return this.shared().durationLabel(seconds);
  }
}

@Pipe({ name: 'planMinutes' })
export class PlanMinutesPipe implements PipeTransform {
  transform(plan: { readonly start: string; readonly end: string }): number {
    return planMinutes(plan);
  }
}
