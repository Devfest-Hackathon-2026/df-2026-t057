// 変更案を本人が手で直すときの計算（順序の入れ替えと、開始・終了時刻の変更）。
// 結果はサーバーが確定時に検証する。ここでは見た目を整えるだけで、ルールは持たない。
import type { Plan, PlanChange, Step } from '@dopagaki/contract';

const iso = (ms: number) => new Date(ms).toISOString();

function shift(plan: Plan, deltaMs: number): Plan {
  return {
    ...plan,
    start: iso(Date.parse(plan.start) + deltaMs),
    end: iso(Date.parse(plan.end) + deltaMs),
    steps: plan.steps.map((step) => ({
      ...step,
      start: iso(Date.parse(step.start) + deltaMs),
      end: iso(Date.parse(step.end) + deltaMs),
    })),
  };
}

/**
 * `index` の課題を1つ上へ移し、最初の開始時刻から順に隙間なく並べ直す。
 * 各課題の長さと手順の割り当ては変えない。
 */
export function moveUp(changes: readonly PlanChange[], index: number): PlanChange[] {
  const first = changes[0];
  const current = changes[index];
  const previous = changes[index - 1];
  if (index <= 0 || !first || !current || !previous) return [...changes];
  const reordered = [...changes];
  reordered[index - 1] = current;
  reordered[index] = previous;
  let cursor = Date.parse(first.after.start);
  return reordered.map((change) => {
    const after = shift(change.after, cursor - Date.parse(change.after.start));
    cursor = Date.parse(after.end);
    return { ...change, after };
  });
}

/**
 * 開始か終了の時刻を変える。手順は元の比率で伸び縮みさせ、最後の手順は終了時刻に合わせる。
 * 終了が開始より前になる値は受け付けない（undefined を返す）。
 */
export function retime(plan: Plan, field: 'start' | 'end', value: string): Plan | undefined {
  const next = Date.parse(value);
  if (Number.isNaN(next)) return undefined;
  const start = field === 'start' ? next : Date.parse(plan.start);
  const end = field === 'end' ? next : Date.parse(plan.end);
  if (end <= start) return undefined;
  const ratio = (end - start) / Math.max(1, Date.parse(plan.end) - Date.parse(plan.start));
  let cursor = start;
  const steps: Step[] = plan.steps.map((step, index) => {
    const stepEnd =
      index === plan.steps.length - 1
        ? end
        : cursor + Math.round((Date.parse(step.end) - Date.parse(step.start)) * ratio);
    const result = { ...step, start: iso(cursor), end: iso(stepEnd) };
    cursor = stepEnd;
    return result;
  });
  return { ...plan, start: iso(start), end: iso(end), steps, reason: 'manual' };
}

/** `<input type="datetime-local">` に入れる、端末のタイムゾーンでの表記。 */
export function toLocalInput(value: string): string {
  const date = new Date(value);
  return new Date(date.getTime() - date.getTimezoneOffset() * 60_000).toISOString().slice(0, 16);
}

/** `<input type="datetime-local">` の値を UTC の ISO 文字列にする。読めなければ undefined。 */
export function fromLocalInput(value: string): string | undefined {
  if (!value) return undefined;
  const ms = new Date(value).getTime();
  return Number.isNaN(ms) ? undefined : iso(ms);
}
