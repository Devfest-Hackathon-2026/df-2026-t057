// 入力欄で使う、この端末のタイムゾーンの `YYYY-MM-DDTHH:mm` と、API の RFC 3339 の変換。

const pad = (value: number): string => String(value).padStart(2, '0');

/** `YYYY-MM-DD` */
export function dateKey(date: Date): string {
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`;
}

/** 日付だけを `days` 日ずらす（時刻は 0:00）。 */
export function addDays(date: Date, days: number): Date {
  return new Date(date.getFullYear(), date.getMonth(), date.getDate() + days);
}

/** ISO の日時を、datetime-local の値（この端末の時刻）にする。 */
export function toLocalInput(iso: string): string {
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return '';
  return `${dateKey(date)}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

/** datetime-local の値を ISO の日時にする。読めなければ undefined。 */
export function fromLocalInput(local: string): string | undefined {
  if (!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}$/.test(local)) return undefined;
  const date = new Date(local);
  return Number.isNaN(date.getTime()) || toLocalInput(date.toISOString()) !== local
    ? undefined
    : date.toISOString();
}

/** 新規入力の既定値：現在より後の次の15分刻み。保存済みの時刻は丸めない。 */
export function defaultDeadline(now: Date = new Date()): string {
  const deadline = new Date(now);
  deadline.setMinutes((Math.floor(now.getMinutes() / 15) + 1) * 15, 0, 0);
  return toLocalInput(deadline.toISOString());
}
