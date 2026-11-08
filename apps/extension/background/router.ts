// content script・ポップアップからのメッセージを処理する。メッセージは呼び出し元（background.ts）で検証済み。
import { authStatus, extensionConnection, signIn, receiveCredential, disconnectAuth } from './auth';
import { MANAGEMENT_ORIGIN } from '../lib/config';
import { messagesFor } from '../lib/locale';
import type { OkReply, Request } from '../lib/protocol';
import { isShortsUrl } from '../lib/shorts';
import { api } from './api';
import { connectionVersion, getConnection, replaceConnection } from './connection';
import { ExtensionError } from './errors';
import {
  clearNotifications,
  companionLine,
  greetMorning,
  openApp,
  remindBedtime,
} from './notifications';
import { streamHealthy, restartStream } from './realtime';
import { clearState, getState, patchState } from './store';
import { refetch, refetchAll, tasksPatch } from './sync';
import { clearViewing, flushPendingReports, getOwner, handleViewing } from './viewing';

const AUTHORIZATION = /^(Bearer|Demo) \S+$/;

function fromManagement(sender: chrome.runtime.MessageSender): boolean {
  if (!sender.url || sender.tab?.id === undefined) return false;
  try {
    return new URL(sender.url).origin === MANAGEMENT_ORIGIN;
  } catch {
    return false;
  }
}

/** 前のアカウントのデータをすべて消す。 */
export async function clearAccount(): Promise<void> {
  await clearState();
  await clearViewing();
  await clearNotifications();
  await chrome.action.setBadgeText({ text: '' });
}

/** 溜まっている報告を送り、すべてのリソースを取り直す。 */
export async function syncAll(): Promise<void> {
  await flushPendingReports().catch(() => undefined);
  await refetchAll();
}

async function stateReply(
  sender: chrome.runtime.MessageSender,
  extra: Partial<OkReply> = {},
): Promise<OkReply> {
  const state = await getState();
  const owner = await getOwner();
  return {
    ok: true,
    connected: true,
    ...(state ? { state } : {}),
    ownsViewing: owner !== undefined && owner.tabId === sender.tab?.id,
    ...extra,
  };
}

export async function handleRequest(
  request: Request,
  sender: chrome.runtime.MessageSender,
): Promise<OkReply> {
  switch (request.type) {
    case 'CONNECT': {
      if (
        !fromManagement(sender) ||
        new URL(request.origin).origin !== MANAGEMENT_ORIGIN ||
        (request.authorization !== undefined && !AUTHORIZATION.test(request.authorization))
      ) {
        throw new ExtensionError('untrustedSender');
      }
      const own = await extensionConnection();
      const next =
        request.authorization?.startsWith('Demo ') && own === undefined
          ? { authorization: request.authorization, accountId: request.accountId }
          : own;
      if (next?.accountId !== request.accountId) throw new ExtensionError('connectFromPopup');
      const result = await replaceConnection(next, clearAccount);
      if (result !== 'unchanged') restartStream();
      // 状態は消さずに取り直す（トークンの更新・ブラウザーの再起動後の再接続）。
      // アカウントが変わったときは、前のアカウントのデータを消してから取り直す。
      if (result !== 'unchanged' || !(await getState())) await syncAll();
      return stateReply(sender);
    }
    case 'AUTH_RESULT': {
      if (sender.url !== chrome.runtime.getURL('offscreen.html'))
        throw new ExtensionError('untrustedSender');
      await receiveCredential(request.credential);
      return { ok: true, connected: false };
    }
    case 'SIGN_IN': {
      if (sender.url !== chrome.runtime.getURL('popup.html'))
        throw new ExtensionError('untrustedSender');
      await replaceConnection(undefined, clearAccount);
      restartStream();
      await signIn();
      const own = await extensionConnection();
      if (!own) throw new ExtensionError('connectFromApp');
      await replaceConnection(own, clearAccount);
      restartStream();
      await syncAll();
      return stateReply(sender);
    }
    case 'TEST_NOTICE': {
      if (sender.url !== chrome.runtime.getURL('popup.html'))
        throw new ExtensionError('untrustedSender');
      const copy = messagesFor(await getState()).extension;
      await chrome.notifications.create('setup-test', {
        type: 'basic',
        iconUrl: chrome.runtime.getURL('icon-128.png'),
        title: copy.testNoticeTitle,
        message: copy.testNoticeMessage,
      });
      return stateReply(sender);
    }
    case 'DIAGNOSE': {
      if (!fromManagement(sender)) throw new ExtensionError('untrustedSender');
      const connection = await getConnection();
      return {
        ok: true,
        connected: !!connection,
        ...(connection ? { accountId: connection.accountId } : {}),
        notifications: await chrome.notifications.getPermissionLevel(),
      };
    }
    case 'DISCONNECT': {
      if (!fromManagement(sender)) throw new ExtensionError('untrustedSender');
      if (request.accountId) {
        const own = await authStatus();
        const accountId = own.live ? own.accountId : (await getConnection())?.accountId;
        if (accountId !== request.accountId) return { ok: true, connected: false };
      }
      await disconnectAuth();
      await replaceConnection(undefined, clearAccount);
      restartStream();
      return { ok: true, connected: false };
    }
    case 'GET_STATE': {
      if (!(await getConnection())) return { ok: true, connected: false };
      if (request.refresh || !streamHealthy() || !(await getState())) await syncAll();
      // Shorts を開いたときに、夜の声かけと朝の精算も確かめる（どちらも回数を抑えてある）。
      if (sender.url && isShortsUrl(sender.url)) {
        const state = await getState();
        void remindBedtime(state).catch(() => undefined);
        void greetMorning(state).catch(() => undefined);
      }
      return stateReply(sender);
    }
    case 'START_TASK': {
      const version = connectionVersion();
      const workSession = await api.startTask(request.taskId);
      await patchState(version, { workSession, workSyncedAt: Date.now() });
      await refetch(['tasks']);
      return stateReply(sender);
    }
    case 'COMPLETE_TASK': {
      const work = await api.completeTask(request.taskId);
      await refetch(['tasks', 'workSession', 'wallet', 'viewing']);
      return stateReply(sender, work ? { work } : {});
    }
    case 'PAUSE_WORK':
    case 'RESUME_WORK': {
      const version = connectionVersion();
      const workSession =
        request.type === 'PAUSE_WORK' ? await api.pauseWork() : await api.resumeWork();
      await patchState(version, { workSession, workSyncedAt: Date.now() });
      return stateReply(sender);
    }
    case 'CONFIRM_WORK': {
      const work = await api.confirmWork(request.sessionId);
      await refetch(['tasks', 'workSession', 'wallet', 'viewing']);
      return stateReply(sender, { work });
    }
    case 'GET_DRAFT':
      return { ok: true, connected: true, draft: await api.draft(request.taskId, request.mode) };
    case 'ADJUST_DRAFT':
      return {
        ok: true,
        connected: true,
        draft: await api.adjustDraft(request.draft, request.instruction),
      };
    case 'COMMIT_DRAFT': {
      const version = connectionVersion();
      const list = await api.commitDraft(request.draft);
      await patchState(version, tasksPatch(list));
      return stateReply(sender);
    }
    case 'START_VIEWING':
    case 'CLAIM_VIEWING':
    case 'END_VIEWING':
    case 'REPORT_VIEWING': {
      const outcome = await handleViewing(request, sender);
      return stateReply(sender, { ownsViewing: outcome.ownsViewing });
    }
    case 'COMPANION_LINE':
      return {
        ok: true,
        connected: true,
        line: await companionLine(request.moment, request.taskId),
      };
    case 'OPEN_APP':
      await openApp();
      return { ok: true, connected: !!(await getConnection()) };
    case 'CLOSE_TAB': {
      // 夜の声かけの「Shortsを閉じて寝る」だけで使う。Shorts 以外のタブは閉じない。
      if (sender.tab?.id === undefined || !sender.url || !isShortsUrl(sender.url)) {
        throw new ExtensionError('fromShorts');
      }
      await chrome.tabs.remove(sender.tab.id);
      return { ok: true, connected: true };
    }
  }
}
