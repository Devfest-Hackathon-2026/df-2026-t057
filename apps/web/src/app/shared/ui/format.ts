// 日時と時間の表示。日時はこの端末のタイムゾーンで表示する。
import { Pipe, type PipeTransform } from '@angular/core';
import { durationLabel } from '@dopagaki/messages';

const time = new Intl.DateTimeFormat('ja-JP', {
  hour: '2-digit',
  minute: '2-digit',
  hour12: false,
});
const monthDay = new Intl.DateTimeFormat('ja-JP', { month: 'numeric', day: 'numeric' });
const longDate = new Intl.DateTimeFormat('ja-JP', {
  month: 'long',
  day: 'numeric',
  weekday: 'short',
});
const fullDate = new Intl.DateTimeFormat('ja-JP', {
  month: 'long',
  day: 'numeric',
  weekday: 'long',
});

function toDate(value: string | number | Date): Date {
  return value instanceof Date ? value : new Date(value);
}

/** `HH:mm` */
export function formatTime(value: string | number | Date): string {
  return time.format(toDate(value));
}

/** `M/d` */
export function formatMonthDay(value: string | number | Date): string {
  return monthDay.format(toDate(value));
}

/** `M/d HH:mm` */
export function formatDateTime(value: string | number | Date): string {
  return `${formatMonthDay(value)} ${formatTime(value)}`;
}

/** `M月d日(曜)` */
export function formatLongDate(value: string | number | Date): string {
  const parts = longDate.formatToParts(toDate(value));
  const part = (type: Intl.DateTimeFormatPartTypes): string =>
    parts.find((item) => item.type === type)?.value ?? '';
  return `${part('month')}月${part('day')}日(${part('weekday')})`;
}

/** `M月d日 曜日` */
export function formatFullDate(value: string | number | Date): string {
  return fullDate.format(toDate(value));
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

@Pipe({ name: 'time' })
export class TimePipe implements PipeTransform {
  transform(value: string | number | Date): string {
    return formatTime(value);
  }
}

@Pipe({ name: 'monthDay' })
export class MonthDayPipe implements PipeTransform {
  transform(value: string | number | Date): string {
    return formatMonthDay(value);
  }
}

@Pipe({ name: 'dateTime' })
export class DateTimePipe implements PipeTransform {
  transform(value: string | number | Date): string {
    return formatDateTime(value);
  }
}

@Pipe({ name: 'longDate' })
export class LongDatePipe implements PipeTransform {
  transform(value: string | number | Date): string {
    return formatLongDate(value);
  }
}

@Pipe({ name: 'duration' })
export class DurationPipe implements PipeTransform {
  transform(seconds: number): string {
    return durationLabel(seconds);
  }
}

@Pipe({ name: 'planMinutes' })
export class PlanMinutesPipe implements PipeTransform {
  transform(plan: { readonly start: string; readonly end: string }): number {
    return planMinutes(plan);
  }
}
