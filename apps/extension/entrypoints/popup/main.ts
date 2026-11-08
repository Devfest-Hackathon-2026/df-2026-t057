// ポップアップ。接続の状態、視聴できる時間、作業中の課題か次の課題、作業サイトでのタイマーの許可。
import '@dopagaki/tokens/tokens.css';
import './style.css';
import {
  clockLabel,
  durationLabel,
  extensionCopy,
  timeLabel,
  workRewardMessage,
} from '@dopagaki/messages';
import { send } from '../../content/messaging';
import { MANAGEMENT_ORIGIN } from '../../lib/config';
import type { OkReply, Request } from '../../lib/protocol';
import type { ExtensionState } from '../../lib/schemas';
import { workSitePattern, workSiteScriptId } from '../../lib/urls';
import { workElapsedSeconds } from '../../lib/work-timer';
import { runAction } from '../../ui/action';
import { button } from '../../ui/dom';
import { YOUTUBE_MATCH } from '../../buildconfig/manifest';

function byId<T extends HTMLElement>(id: string, type: new () => T): T {
  const element = document.getElementById(id);
  if (!(element instanceof type)) throw new Error(`#${id} is missing`);
  return element;
}

const balance = byId('balance', HTMLElement);
const focus = byId('focus', HTMLElement);
const workActions = byId('work-actions', HTMLElement);
const status = byId('status', HTMLElement);
const openButton = byId('open', HTMLButtonElement);
const permissionButton = byId('permission', HTMLButtonElement);
byId('balance-label', HTMLElement).textContent = extensionCopy.popupAvailable;
focus.textContent = extensionCopy.popupChecking;
openButton.textContent = extensionCopy.popupOpenApp;
permissionButton.textContent = extensionCopy.popupWorkSite;

let state: ExtensionState | undefined;

function renderBalance(current: ExtensionState): void {
  const mode = current.wallet?.policy.mode;
  const available = current.viewing?.availableSeconds ?? current.wallet?.availableSeconds;
  balance.textContent =
    mode === 'self' || mode === 'reminder'
      ? extensionCopy.popupUnlimited
      : available === undefined
        ? '—'
        : durationLabel(Math.max(0, available));
}

function act(request: Request, after?: (reply: OkReply) => void): void {
  void runAction(workActions, request).then((reply) => {
    if (!reply?.ok) return;
    if (reply.state) render(reply.state);
    after?.(reply);
  });
}

function renderWork(current: ExtensionState): void {
  workActions.replaceChildren();
  const session = current.workSession;
  if (session) {
    const task = current.tasks.find((item) => item.id === session.taskId);
    focus.textContent = extensionCopy.popupWorking(
      task?.title ?? '',
      clockLabel(workElapsedSeconds(session, current.workSyncedAt, Date.now())),
    );
    workActions.append(
      button(session.running ? extensionCopy.pause : extensionCopy.resume, () => {
        act({ type: session.running ? 'PAUSE_WORK' : 'RESUME_WORK' });
      }),
      button(extensionCopy.confirmWork, () => {
        act({ type: 'CONFIRM_WORK', sessionId: session.id }, (reply) => {
          if (reply.work)
            status.textContent = workRewardMessage(reply.work.rewardSeconds, reply.work.deferred);
        });
      }),
      button(
        extensionCopy.complete,
        () => {
          act({ type: 'COMPLETE_TASK', taskId: session.taskId }, (reply) => {
            status.textContent = workRewardMessage(
              reply.work?.rewardSeconds ?? 0,
              reply.work?.deferred ?? false,
            );
          });
        },
        'tonal',
      ),
    );
    return;
  }
  const next = current.tasks
    .filter((task) => task.status === 'pending')
    .sort((a, b) => Date.parse(a.plan.start) - Date.parse(b.plan.start))[0];
  if (!next) {
    focus.textContent = extensionCopy.popupIdle;
    return;
  }
  focus.textContent = extensionCopy.popupNext(next.title, timeLabel(next.plan.start));
  workActions.append(
    button(
      extensionCopy.popupStartNext,
      () => {
        act({ type: 'START_TASK', taskId: next.id });
      },
      'tonal',
    ),
  );
}

function render(current: ExtensionState): void {
  state = current;
  renderBalance(current);
  renderWork(current);
}

setInterval(() => {
  const session = state?.workSession;
  if (!state || !session) return;
  const task = state.tasks.find((item) => item.id === session.taskId);
  focus.textContent = extensionCopy.popupWorking(
    task?.title ?? '',
    clockLabel(workElapsedSeconds(session, state.workSyncedAt, Date.now())),
  );
}, 1000);

openButton.addEventListener('click', () => {
  void send({ type: 'OPEN_APP' }).then(() => {
    window.close();
  });
});

// 許可を求めるのはクリックの直後に行う必要があるので、対象のタブは先に調べておく。
const activeTab = chrome.tabs.query({ active: true, currentWindow: true }).then(([tab]) => tab);
let tabNow: chrome.tabs.Tab | undefined;
void activeTab.then((tab) => {
  tabNow = tab;
});

/** YouTube と管理画面には、もともとタイマー（または管理画面の表示）があるので登録しない。 */
function alreadyCovered(pattern: string): boolean {
  return pattern === YOUTUBE_MATCH || pattern === `${MANAGEMENT_ORIGIN}/*`;
}

permissionButton.addEventListener('click', () => {
  const tab = tabNow;
  const pattern = workSitePattern(tab?.url);
  if (!tab || !pattern) {
    status.textContent = extensionCopy.popupOpenWorkPage;
    return;
  }
  if (alreadyCovered(pattern)) {
    status.textContent = extensionCopy.popupWorkSiteEnabled;
    return;
  }
  void chrome.permissions
    .request({ origins: [pattern] })
    .then(async (granted) => {
      if (!granted) {
        status.textContent = extensionCopy.popupPermissionUnchanged;
        return;
      }
      const id = workSiteScriptId(pattern);
      const file = 'content-scripts/worksite.js';
      const registered = await chrome.scripting.getRegisteredContentScripts({ ids: [id] });
      if (registered.length === 0) {
        await chrome.scripting.registerContentScripts([
          {
            id,
            matches: [pattern],
            js: [file],
            runAt: 'document_idle',
            persistAcrossSessions: true,
          },
        ]);
      }
      if (tab.id !== undefined)
        await chrome.scripting.executeScript({ target: { tabId: tab.id }, files: [file] });
      status.textContent = extensionCopy.popupWorkSiteEnabled;
    })
    .catch(() => {
      status.textContent = extensionCopy.popupWorkSiteFailed;
    });
});

void send({ type: 'GET_STATE', refresh: false }).then((reply) => {
  if (!reply.ok) {
    status.textContent = reply.retryable
      ? extensionCopy.popupConnectFrom(MANAGEMENT_ORIGIN)
      : reply.error;
    focus.textContent = extensionCopy.popupIdle;
    return;
  }
  if (!reply.connected || !reply.state) {
    status.textContent = extensionCopy.popupOpenToConnect;
    focus.textContent = extensionCopy.popupIdle;
    return;
  }
  status.textContent = extensionCopy.popupConnected;
  render(reply.state);
});

const connectButton = button('Googleアカウントで拡張に接続', () => {
  void runAction(workActions, { type: 'SIGN_IN' }).then((reply) => {
    if (reply?.ok && reply.state) render(reply.state);
  });
});
const testButton = button('通知をテスト', () => {
  void runAction(workActions, { type: 'TEST_NOTICE' });
});
document.body.append(connectButton, testButton);
