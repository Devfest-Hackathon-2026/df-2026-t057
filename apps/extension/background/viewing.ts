// 視聴の区切りと報告。複数のタブから同時に操作されても順番に処理する。
//
// - 区切りは1つだけ。始めたタブ（または引き継いだタブ）が持ち主になり、報告と終了は持ち主だけが行う。
// - 報告は先に chrome.storage.local へ書いてから送る。送れなければ残し、次の操作や同期のときに同じ受付の ID で送り直す。
import type { Viewing } from '@dopagaki/contract';
import { z } from 'zod';
import type { Request } from '../lib/protocol';
import { enqueue, failureAction } from '../lib/report-queue';
import { isShortsUrl, isYouTubeUrl } from '../lib/shorts';
import { api } from './api';
import { connectionVersion, getConnection } from './connection';
import { ExtensionError, failureOf } from './errors';
import { getState, patchState } from './store';
import { allocate, clearJournal, retainAccount, updateJournal } from './viewing-journal';

const OWNER_KEY = 'viewingOwner';

const ownerSchema = z.object({ tabId: z.number().int(), sessionId: z.string() });
export type ViewingOwner = z.infer<typeof ownerSchema>;

let chain: Promise<unknown> = Promise.resolve();
const ownerListeners = new Set<() => void>();

/** 視聴の操作を1つずつ順に実行する。 */
function serialize<T>(task: () => Promise<T>): Promise<T> {
  const result = chain.then(task);
  chain = result.catch(() => undefined);
  return result;
}

export function onOwnerChange(listener: () => void): void {
  ownerListeners.add(listener);
}

export async function getOwner(): Promise<ViewingOwner | undefined> {
  const parsed = ownerSchema.safeParse((await chrome.storage.session.get(OWNER_KEY))[OWNER_KEY]);
  return parsed.success ? parsed.data : undefined;
}

async function setOwner(owner: ViewingOwner | undefined): Promise<void> {
  if (owner) await chrome.storage.session.set({ [OWNER_KEY]: owner });
  else await chrome.storage.session.remove(OWNER_KEY);
  for (const listener of ownerListeners) listener();
}

interface FlushResult {
  readonly viewing: Viewing | undefined;
  /** 受け付けられずに捨てた報告と、その理由。 */
  readonly dropped: ReadonlyMap<string, unknown>;
}

/** 溜まっている報告を古い順に送る。送り直すべき失敗があれば、そこで止めて投げる。 */
async function flushReports(includeOpen = false): Promise<FlushResult> {
  const version = connectionVersion();
  const accountId = (await getConnection())?.accountId;
  if (!accountId) return { viewing: undefined, dropped: new Map() };
  let viewing = (await getState())?.viewing ?? undefined;
  const dropped = new Map<string, unknown>();
  for (let attempts = 0; attempts < 40; attempts += 1) {
    const report = await updateJournal((journal) => {
      if (version !== connectionVersion()) throw new ExtensionError('accountChanged');
      retainAccount(journal, accountId);
      if (viewing?.reportCursor !== undefined) {
        allocate(
          journal,
          accountId,
          viewing.requiresSession ? viewing.session?.id : undefined,
          viewing.reportCursor,
          includeOpen,
        );
      }
      return journal.reports[0];
    });
    if (!report) break;
    let result: Viewing;
    try {
      result = await api.reportViewing(
        report.id,
        report.seconds,
        report.sessionId,
        report.fromSeconds,
      );
    } catch (error) {
      if (version !== connectionVersion() || failureAction(failureOf(error)) === 'retry')
        throw error;
      dropped.set(report.id, error);
      await updateJournal((journal) => {
        if (version !== connectionVersion()) throw new ExtensionError('accountChanged');
        journal.reports = journal.reports.filter((item) => item.id !== report.id);
        const checkpoint = journal.checkpoints.find((item) => item.id === report.meterId);
        if (checkpoint) checkpoint.acknowledged += report.seconds;
      });
      continue;
    }
    if (version !== connectionVersion()) throw new ExtensionError('accountChanged');
    viewing = result;
    const accepted =
      report.fromSeconds === undefined ? report.seconds : (result.acceptedSeconds ?? 0);
    const ended =
      result.requiresSession &&
      (result.session?.id !== report.sessionId || !!result.session?.endedAt);
    await updateJournal((journal) => {
      if (version !== connectionVersion()) throw new ExtensionError('accountChanged');
      const index = journal.reports.findIndex((item) => item.id === report.id);
      if (index < 0) return;
      const checkpoint = journal.checkpoints.find((item) => item.id === report.meterId);
      if (checkpoint) checkpoint.acknowledged += ended ? report.seconds : accepted;
      if (accepted < report.seconds && !ended && report.fromSeconds !== undefined) {
        journal.reports[index] = {
          ...report,
          id: crypto.randomUUID(),
          fromSeconds: report.fromSeconds + accepted,
          seconds: report.seconds - accepted,
        };
      } else journal.reports.splice(index, 1);
    });
    await patchState(version, { viewing });
    // 時計の許容範囲にまだ入らない区間は残す。次のtick/同期で別受付として続ける。
    if (accepted < report.seconds && !ended) break;
  }
  return { viewing, dropped };
}

/** 同期の前などに、溜まっている報告を送る。 */
export function flushPendingReports(includeOpen = false): Promise<void> {
  return serialize(async () => {
    await flushReports(includeOpen);
  });
}

/**
 * 区切りを始める・引き継ぐのは、前面の Shorts のタブから（利用者が押したときだけ）。
 * 報告と終了は、Shorts から YouTube の別のページへ移った直後にも届くので、YouTube のタブなら受け付ける。
 */
async function senderTab(
  sender: chrome.runtime.MessageSender,
  kind: 'choose' | 'report',
): Promise<number> {
  const tab = sender.tab;
  const url = sender.url ?? '';
  if (tab?.id === undefined || !isYouTubeUrl(url)) throw new ExtensionError('fromShorts');
  if (kind === 'choose') {
    // sender.url は SPA 遷移前の URL の場合がある。現在のタブをブラウザーから取得する。
    const current = await chrome.tabs.get(tab.id);
    if (!current.active || !isShortsUrl(current.url ?? '')) throw new ExtensionError('fromShorts');
  }
  return tab.id;
}

/** 区切りを使うルールでは、持ち主のタブで、同じ区切りのときだけ報告・終了を受け付ける。 */
async function requireOwner(tabId: number, sessionId: string | undefined): Promise<void> {
  const state = await getState();
  if (state?.viewing && !state.viewing.requiresSession) return;
  const owner = await getOwner();
  if (owner?.tabId !== tabId || owner.sessionId !== sessionId) {
    throw new ExtensionError('claimFirst');
  }
}

type ViewingRequest = Extract<
  Request,
  {
    type:
      'START_VIEWING' | 'CLAIM_VIEWING' | 'END_VIEWING' | 'REPORT_VIEWING' | 'CHECKPOINT_VIEWING';
  }
>;

export interface ViewingOutcome {
  readonly ownsViewing: boolean;
  readonly checkpoint?: { meterId: string; acknowledgedSeconds: number };
}

export function handleViewing(
  request: ViewingRequest,
  sender: chrome.runtime.MessageSender,
): Promise<ViewingOutcome> {
  if (request.type === 'CHECKPOINT_VIEWING') return checkpointViewing(request, sender);
  return serialize(async () => {
    const version = connectionVersion();
    if (!(await getConnection())) throw new ExtensionError('connectFromApp');
    switch (request.type) {
      case 'START_VIEWING': {
        const tabId = await senderTab(sender, 'choose');
        await flushReports(true);
        const viewing = await api.startViewing(
          request.sessionId,
          request.seconds,
          request.extendFrom,
        );
        if (version !== connectionVersion()) throw new ExtensionError('accountChanged');
        if (viewing.session) await setOwner({ tabId, sessionId: viewing.session.id });
        await patchState(version, { viewing });
        return { ownsViewing: true };
      }
      case 'CLAIM_VIEWING': {
        const tabId = await senderTab(sender, 'choose');
        await flushReports(true);
        const viewing = await api.viewing();
        await patchState(version, { viewing });
        const session = viewing.session;
        if (session?.id !== request.sessionId || session.endedAt) {
          throw new ExtensionError('sessionEnded');
        }
        await setOwner({ tabId, sessionId: session.id });
        return { ownsViewing: true };
      }
      case 'END_VIEWING': {
        const tabId = await senderTab(sender, 'report');
        await requireOwner(tabId, request.sessionId);
        await flushReports(true);
        const viewing = await api.endViewing(request.sessionId, request.reason);
        await patchState(version, { viewing });
        return { ownsViewing: true };
      }
      case 'REPORT_VIEWING': {
        const tabId = await senderTab(sender, 'report');
        await requireOwner(tabId, request.sessionId);
        const accountId = (await getConnection())?.accountId;
        if (!accountId || version !== connectionVersion()) {
          throw new ExtensionError('accountChanged');
        }
        await updateJournal((journal) => {
          if (version !== connectionVersion()) throw new ExtensionError('accountChanged');
          retainAccount(journal, accountId);
          journal.reports = enqueue(journal.reports, {
            id: request.receiptId,
            seconds: request.seconds,
            sessionId: request.sessionId,
            accountId,
          });
        });
        const { dropped } = await flushReports(true);
        if (dropped.has(request.receiptId)) throw dropped.get(request.receiptId);
        return { ownsViewing: (await getOwner())?.tabId === tabId };
      }
    }
  });
}

/** 持ち主のタブが閉じたら、ほかのタブが引き継げるようにする。 */
export async function forgetTab(tabId: number): Promise<void> {
  await sealCheckpoints(tabId);
  if ((await getOwner())?.tabId === tabId) await setOwner(undefined);
  // 受付済みの計測はタブの寿命と無関係に再送する。失敗時もjournalに残る。
  await flushPendingReports().catch(() => undefined);
}

async function sealCheckpoints(tabId?: number, documentId?: string): Promise<void> {
  await updateJournal((journal) => {
    for (const checkpoint of journal.checkpoints) {
      if (
        (tabId === undefined || checkpoint.tabId === tabId) &&
        (documentId === undefined || checkpoint.documentId === documentId)
      )
        checkpoint.closed = true;
    }
  });
}

/** ページのポート切断やブラウザー再起動でも、所有権を変えずに保存済み区間を回収する。 */
export async function closeViewingDocument(sender?: chrome.runtime.MessageSender): Promise<void> {
  if (sender && (sender.tab?.id === undefined || !isYouTubeUrl(sender.url ?? ''))) return;
  await sealCheckpoints(sender?.tab?.id, sender?.documentId);
  await flushPendingReports().catch(() => undefined);
}

/** アカウントの切り替え・接続の解除で、前のアカウントの視聴の記録を消す。 */
export async function clearViewing(): Promise<void> {
  await clearJournal();
  await setOwner(undefined);
}

/** HTTPの待機列とは別に、各tickの累積計測を先に永続化する。 */
async function checkpointViewing(
  request: Extract<Request, { type: 'CHECKPOINT_VIEWING' }>,
  sender: chrome.runtime.MessageSender,
): Promise<ViewingOutcome> {
  const version = connectionVersion();
  const tabId = await senderTab(sender, 'report');
  await requireOwner(tabId, request.sessionId);
  const accountId = (await getConnection())?.accountId;
  if (!accountId) throw new ExtensionError('connectFromApp');
  if (accountId !== request.accountId) throw new ExtensionError('accountChanged');
  const savedAcknowledgement = await updateJournal((journal) => {
    if (version !== connectionVersion()) throw new ExtensionError('accountChanged');
    retainAccount(journal, accountId);
    const now = Date.now();
    journal.checkpoints = journal.checkpoints.filter(
      (item) =>
        !(item.closed && item.acknowledged >= item.total && item.updatedAt < now - 86400000),
    );
    let checkpoint = journal.checkpoints.find((item) => item.id === request.meterId);
    if (checkpoint && (checkpoint.tabId !== tabId || checkpoint.sessionId !== request.sessionId))
      throw new ExtensionError('untrustedSender');
    if (!checkpoint) {
      for (const item of journal.checkpoints) if (item.tabId === tabId) item.closed = true;
      checkpoint = {
        id: request.meterId,
        accountId,
        tabId,
        sessionId: request.sessionId,
        total: 0,
        queued: 0,
        acknowledged: 0,
        closed: false,
        updatedAt: now,
        documentId: sender.documentId,
      };
      journal.checkpoints.push(checkpoint);
    }
    const advanced = request.totalSeconds > checkpoint.total;
    checkpoint.total = Math.max(checkpoint.total, request.totalSeconds);
    checkpoint.closed = request.closed || (checkpoint.closed && !advanced);
    checkpoint.updatedAt = now;
    return checkpoint.acknowledged;
  });
  if (request.flush) await flushPendingReports(true);
  const acknowledgedSeconds = request.flush
    ? await updateJournal(
        (journal) =>
          journal.checkpoints.find((item) => item.id === request.meterId)?.acknowledged ?? 0,
      )
    : savedAcknowledgement;
  if (version !== connectionVersion()) throw new ExtensionError('accountChanged');
  return {
    ownsViewing: (await getOwner())?.tabId === tabId,
    checkpoint: { meterId: request.meterId, acknowledgedSeconds },
  };
}
