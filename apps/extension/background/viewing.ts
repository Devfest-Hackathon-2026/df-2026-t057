// 視聴の区切りと報告。複数のタブから同時に操作されても順番に処理する。
//
// - 区切りは1つだけ。始めたタブ（または引き継いだタブ）が持ち主になり、報告と終了は持ち主だけが行う。
// - 報告は先に chrome.storage.local へ書いてから送る。送れなければ残し、次の操作や同期のときに同じ受付の ID で送り直す。
import type { Viewing } from '@dopagaki/contract';
import { z } from 'zod';
import type { Request } from '../lib/protocol';
import {
  enqueue,
  failureAction,
  forAccount,
  remove,
  type PendingReport,
} from '../lib/report-queue';
import { isShortsUrl, isYouTubeUrl } from '../lib/shorts';
import { api } from './api';
import { connectionVersion, getConnection } from './connection';
import { ExtensionError, failureOf } from './errors';
import { getState, patchState } from './store';

const OWNER_KEY = 'viewingOwner';
const PENDING_KEY = 'viewingPending';

const ownerSchema = z.object({ tabId: z.number().int(), sessionId: z.string() });
export type ViewingOwner = z.infer<typeof ownerSchema>;

const pendingSchema = z.array(
  z.object({
    id: z.string(),
    seconds: z.number().int().min(1).max(65),
    sessionId: z.string().optional(),
    accountId: z.string(),
  }),
);

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

async function loadPending(): Promise<PendingReport[]> {
  const parsed = pendingSchema.safeParse(
    (await chrome.storage.local.get(PENDING_KEY))[PENDING_KEY],
  );
  if (!parsed.success) return [];
  return parsed.data.map((item) => ({ ...item, sessionId: item.sessionId }));
}

async function savePending(queue: readonly PendingReport[]): Promise<void> {
  if (queue.length === 0) await chrome.storage.local.remove(PENDING_KEY);
  else await chrome.storage.local.set({ [PENDING_KEY]: queue });
}

interface FlushResult {
  readonly viewing: Viewing | undefined;
  /** 受け付けられずに捨てた報告と、その理由。 */
  readonly dropped: ReadonlyMap<string, unknown>;
}

/** 溜まっている報告を古い順に送る。送り直すべき失敗があれば、そこで止めて投げる。 */
async function flushReports(): Promise<FlushResult> {
  const version = connectionVersion();
  const accountId = (await getConnection())?.accountId;
  // 接続が切れている間（ブラウザーの再起動後など）は、送らずに残しておく。
  // アカウントが切り替わったときは clearViewing で消える。
  if (!accountId) return { viewing: undefined, dropped: new Map() };
  let queue = forAccount(await loadPending(), accountId);
  await savePending(queue);
  let viewing: Viewing | undefined;
  const dropped = new Map<string, unknown>();
  for (const report of [...queue]) {
    try {
      viewing = await api.reportViewing(report.id, report.seconds, report.sessionId);
      queue = remove(queue, report.id);
      await savePending(queue);
    } catch (error) {
      if (failureAction(failureOf(error)) === 'retry') throw error;
      dropped.set(report.id, error);
      queue = remove(queue, report.id);
      await savePending(queue);
    }
  }
  if (viewing) await patchState(version, { viewing });
  return { viewing, dropped };
}

/** 同期の前などに、溜まっている報告を送る。 */
export function flushPendingReports(): Promise<void> {
  return serialize(async () => {
    await flushReports();
  });
}

/**
 * 区切りを始める・引き継ぐのは、前面の Shorts のタブから（利用者が押したときだけ）。
 * 報告と終了は、Shorts から YouTube の別のページへ移った直後にも届くので、YouTube のタブなら受け付ける。
 */
function senderTab(sender: chrome.runtime.MessageSender, kind: 'choose' | 'report'): number {
  const tab = sender.tab;
  const url = sender.url ?? '';
  const allowed = kind === 'choose' ? isShortsUrl(url) && tab?.active === true : isYouTubeUrl(url);
  if (tab?.id === undefined || !allowed) throw new ExtensionError('fromShorts');
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
  { type: 'START_VIEWING' | 'CLAIM_VIEWING' | 'END_VIEWING' | 'REPORT_VIEWING' }
>;

export interface ViewingOutcome {
  readonly ownsViewing: boolean;
}

export function handleViewing(
  request: ViewingRequest,
  sender: chrome.runtime.MessageSender,
): Promise<ViewingOutcome> {
  return serialize(async () => {
    const version = connectionVersion();
    if (!(await getConnection())) throw new ExtensionError('connectFromApp');
    switch (request.type) {
      case 'START_VIEWING': {
        const tabId = senderTab(sender, 'choose');
        await flushReports();
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
        const tabId = senderTab(sender, 'choose');
        await flushReports();
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
        const tabId = senderTab(sender, 'report');
        await requireOwner(tabId, request.sessionId);
        await flushReports();
        const viewing = await api.endViewing(request.sessionId, request.reason);
        await patchState(version, { viewing });
        return { ownsViewing: true };
      }
      case 'REPORT_VIEWING': {
        const tabId = senderTab(sender, 'report');
        await requireOwner(tabId, request.sessionId);
        const accountId = (await getConnection())?.accountId;
        if (!accountId || version !== connectionVersion()) {
          throw new ExtensionError('accountChanged');
        }
        await savePending(
          enqueue(await loadPending(), {
            id: request.receiptId,
            seconds: request.seconds,
            sessionId: request.sessionId,
            accountId,
          }),
        );
        const { dropped } = await flushReports();
        if (dropped.has(request.receiptId)) throw dropped.get(request.receiptId);
        return { ownsViewing: (await getOwner())?.tabId === tabId };
      }
    }
  });
}

/** 持ち主のタブが閉じたら、ほかのタブが引き継げるようにする。 */
export async function forgetTab(tabId: number): Promise<void> {
  if ((await getOwner())?.tabId === tabId) await setOwner(undefined);
}

/** アカウントの切り替え・接続の解除で、前のアカウントの視聴の記録を消す。 */
export async function clearViewing(): Promise<void> {
  await chrome.storage.local.remove(PENDING_KEY);
  await setOwner(undefined);
}
