// 変更案を手で直すときの、表示用の組み立て。保存するときにサーバーがすべて検証し直す（重なり・締切など）。
// 値はすべて新しいオブジェクトで返し、元の案を書き換えない。
import type { Draft, Plan, PlanChange, Step } from '@dopagaki/contract';

const ms = (iso: string): number => Date.parse(iso);
const iso = (value: number): string => new Date(value).toISOString();

function withPlan(change: PlanChange, after: Plan): PlanChange {
  return {
    ...change,
    after: { ...after, reason: 'manual' },
    plannedSeconds: Math.max(0, Math.round((ms(after.end) - ms(after.start)) / 1000)),
  };
}

/** 予定の開始か終了を変える。手順は長さの比率を保って並べ直し、最後の手順は新しい終了で終える。 */
export function retimeChange(
  change: PlanChange,
  field: 'start' | 'end',
  value: string,
): PlanChange {
  const previous = change.after;
  const start = field === 'start' ? ms(value) : ms(previous.start);
  const end = field === 'end' ? ms(value) : ms(previous.end);
  const oldDuration = ms(previous.end) - ms(previous.start);
  const count = previous.steps.length;
  let cursor = start;
  const steps: Step[] = previous.steps.map((step, index) => {
    const share =
      oldDuration > 0 ? (ms(step.end) - ms(step.start)) / oldDuration : 1 / Math.max(1, count);
    const stepEnd = index === count - 1 ? end : cursor + Math.round((end - start) * share);
    const result = { title: step.title, start: iso(cursor), end: iso(stepEnd) };
    cursor = stepEnd;
    return result;
  });
  return withPlan(change, { ...previous, start: iso(start), end: iso(end), steps });
}

/** 手順の名前か時刻を変える。手順は隙間なく続くように、となりの手順の境目も合わせる。 */
export function editStep(
  change: PlanChange,
  index: number,
  field: 'title' | 'start' | 'end',
  value: string,
): PlanChange {
  const steps = change.after.steps.map((step) => ({ ...step }));
  const step = steps[index];
  if (!step) return change;
  if (field === 'title') {
    step.title = value;
  } else {
    const time = iso(ms(value));
    step[field] = time;
    const neighbour = field === 'end' ? steps[index + 1] : steps[index - 1];
    if (neighbour) {
      if (field === 'end') neighbour.start = time;
      else neighbour.end = time;
    }
  }
  const first = steps[0];
  const last = steps.at(-1);
  if (!first || !last) return change;
  return withPlan(change, { ...change.after, start: first.start, end: last.end, steps });
}

/** `index` 番目を1つ上へ移し、元の最初の開始から隙間なく詰め直す。それぞれの長さは保つ。 */
export function moveChangeUp(draft: Draft, index: number): Draft {
  const current = draft.changes[index];
  const above = draft.changes[index - 1];
  const head = draft.changes[0];
  if (index <= 0 || !current || !above || !head) return draft;
  const order = [...draft.changes];
  order[index - 1] = current;
  order[index] = above;
  let cursor = ms(head.after.start);
  const changes = order.map((change) => {
    const delta = cursor - ms(change.after.start);
    const shifted: PlanChange =
      delta === 0
        ? change
        : withPlan(change, {
            ...change.after,
            start: iso(ms(change.after.start) + delta),
            end: iso(ms(change.after.end) + delta),
            steps: change.after.steps.map((step) => ({
              ...step,
              start: iso(ms(step.start) + delta),
              end: iso(ms(step.end) + delta),
            })),
          });
    cursor = ms(shifted.after.end);
    return shifted;
  });
  return { ...draft, changes };
}

/** 確定のエラー（`changes/0/after/steps` など）が、何番目の予定のものかを返す。 */
export function changeIndexOf(field: string): number | undefined {
  const match = /^changes\/(\d+)(\/|$)/.exec(field);
  return match?.[1] === undefined ? undefined : Number(match[1]);
}
