// 計測途中のチェックポイントと送信待ち受付を、1回のstorage書き込みで保存する。
// HTTP待ちの間も新しい計測を保存できるよう、保存だけを直列化する。
import { z } from 'zod';
import { MAX_PENDING_REPORTS, type PendingReport } from '../lib/report-queue';

const KEY = 'viewingJournal';
const reportSchema = z.object({
  id: z.string(),
  seconds: z.number().int().min(1).max(65),
  sessionId: z.string().optional(),
  accountId: z.string(),
  fromSeconds: z.number().int().nonnegative().exactOptional(),
  meterId: z.string().exactOptional(),
});
const checkpointSchema = z.object({
  id: z.string(),
  accountId: z.string(),
  tabId: z.number().int(),
  sessionId: z.string().optional(),
  total: z.number().int().nonnegative(),
  queued: z.number().int().nonnegative(),
  acknowledged: z.number().int().nonnegative(),
  closed: z.boolean(),
  updatedAt: z.number(),
  documentId: z.string().optional(),
});
const schema = z.object({
  accountId: z.string().exactOptional(),
  reports: z.array(reportSchema),
  checkpoints: z.array(checkpointSchema),
  cursors: z.record(z.string(), z.number().int().nonnegative()),
});
export type Checkpoint = z.infer<typeof checkpointSchema>;
export interface Journal {
  accountId?: string;
  reports: PendingReport[];
  checkpoints: Checkpoint[];
  cursors: Record<string, number>;
}

let writes: Promise<unknown> = Promise.resolve();
export function updateJournal<T>(change: (journal: Journal) => T): Promise<T> {
  const pending = writes.then(async () => {
    const stored = await chrome.storage.local.get([KEY, 'viewingPending']);
    const parsed = schema.safeParse(stored[KEY]);
    const legacy = z.array(reportSchema).safeParse(stored['viewingPending']);
    const journal: Journal = parsed.success
      ? {
          ...parsed.data,
          reports: parsed.data.reports.map((report) => ({
            ...report,
            sessionId: report.sessionId,
          })),
        }
      : {
          reports: legacy.success
            ? legacy.data.map((report) => ({ ...report, sessionId: report.sessionId }))
            : [],
          checkpoints: [],
          cursors: {},
        };
    const result = change(journal);
    await chrome.storage.local.set({ [KEY]: journal });
    // 移行完了後に旧形式を消す。途中で止まっても次回はKEYを優先する。
    if (stored['viewingPending'] !== undefined) await chrome.storage.local.remove('viewingPending');
    return result;
  });
  writes = pending.catch(() => undefined);
  return pending;
}

export function clearJournal(): Promise<void> {
  return updateJournal((journal) => {
    journal.reports = [];
    journal.checkpoints = [];
    journal.cursors = {};
    delete journal.accountId;
  });
}

export function scopeKey(sessionId: string | undefined): string {
  return sessionId ?? 'free';
}

export function retainAccount(journal: Journal, accountId: string): void {
  if (journal.accountId !== undefined && journal.accountId !== accountId) journal.cursors = {};
  journal.accountId = accountId;
  journal.reports = journal.reports.filter((report) => report.accountId === accountId);
  journal.checkpoints = journal.checkpoints.filter(
    (checkpoint) => checkpoint.accountId === accountId,
  );
}

export function allocate(
  journal: Journal,
  accountId: string,
  sessionId: string | undefined,
  cursor: number,
  includeOpen = false,
): void {
  const key = scopeKey(sessionId);
  let next = Math.max(cursor, journal.cursors[key] ?? 0);
  for (const checkpoint of journal.checkpoints) {
    if (checkpoint.accountId !== accountId || checkpoint.sessionId !== sessionId) continue;
    if (!includeOpen && !checkpoint.closed) continue;
    while (checkpoint.queued < checkpoint.total && journal.reports.length < MAX_PENDING_REPORTS) {
      const seconds = Math.min(60, checkpoint.total - checkpoint.queued);
      journal.reports.push({
        id: crypto.randomUUID(),
        seconds,
        sessionId,
        accountId,
        fromSeconds: next,
        meterId: checkpoint.id,
      });
      checkpoint.queued += seconds;
      next += seconds;
    }
  }
  journal.cursors[key] = next;
}
