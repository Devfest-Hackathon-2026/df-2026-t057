// background（service worker）。状態を持つ唯一の場所で、API との通信・通知のアラーム・変更通知の購読を担当する。
import { defineBackground } from 'wxt/utils/define-background';
import { errorReply, ExtensionError } from '../background/errors';
import {
  greetMorning,
  listenNotificationClicks,
  notifyDueTask,
  remindBedtime,
  scheduleTaskAlarm,
  TASK_ALARM,
} from '../background/notifications';
import { broadcast, listenPorts } from '../background/ports';
import { startStream, stopStream, streamHealthy } from '../background/realtime';
import { handleRequest, syncAll } from '../background/router';
import { getState, onStateChange } from '../background/store';
import { forgetTab, onOwnerChange } from '../background/viewing';
import { getConnection } from '../background/connection';
import { extensionCopy } from '@dopagaki/messages';
import { requestSchema } from '../lib/protocol';
import { workSiteScriptId } from '../lib/urls';

/** 変更通知を購読していない間に、取り直す間隔。 */
const SYNC_ALARM = 'sync';
const SYNC_PERIOD_MINUTES = 1;

async function setup(): Promise<void> {
  // 保存した状態は content script から読めないようにする（ポートで必要な分だけ渡す）。
  await chrome.storage.local.setAccessLevel({ accessLevel: 'TRUSTED_CONTEXTS' });
  if (!(await chrome.alarms.get(SYNC_ALARM))) {
    await chrome.alarms.create(SYNC_ALARM, { periodInMinutes: SYNC_PERIOD_MINUTES });
  }
}

async function periodicSync(): Promise<void> {
  if (!(await getConnection())) return;
  if (!streamHealthy()) await syncAll();
  const state = await getState();
  await Promise.allSettled([remindBedtime(state), greetMorning(state)]);
}

export default defineBackground(() => {
  chrome.runtime.onMessage.addListener((message: unknown, sender, sendResponse) => {
    if (sender.id !== chrome.runtime.id) return false;
    const parsed = requestSchema.safeParse(message);
    if (!parsed.success) {
      sendResponse(errorReply(new ExtensionError(extensionCopy.operationUnknown)));
      return false;
    }
    handleRequest(parsed.data, sender).then(sendResponse, (error: unknown) => {
      sendResponse(errorReply(error));
    });
    return true;
  });

  onStateChange((state) => {
    void broadcast(state);
    void scheduleTaskAlarm(state).catch((error: unknown) => {
      console.warn('DOPAGAKI: failed to schedule the notification', error);
    });
    void chrome.action.setBadgeText({ text: state?.workSession ? '▶' : '' });
  });
  onOwnerChange(() => {
    void broadcast();
  });
  listenPorts((count) => {
    if (count > 0) startStream();
    else stopStream();
  });
  listenNotificationClicks();

  chrome.alarms.onAlarm.addListener((alarm) => {
    const run =
      alarm.name === TASK_ALARM
        ? notifyDueTask
        : alarm.name === SYNC_ALARM
          ? periodicSync
          : undefined;
    void run?.().catch((error: unknown) => {
      console.warn(`DOPAGAKI: ${alarm.name} failed`, error);
    });
  });
  chrome.tabs.onRemoved.addListener((tabId) => {
    void forgetTab(tabId);
  });
  // 作業サイトの許可を外されたら、そのサイトに登録したタイマーも外す。
  chrome.permissions.onRemoved.addListener((removed) => {
    const ids = (removed.origins ?? []).map(workSiteScriptId);
    if (ids.length === 0) return;
    void chrome.scripting
      .getRegisteredContentScripts({ ids })
      .then((scripts) =>
        scripts.length > 0
          ? chrome.scripting.unregisterContentScripts({ ids: scripts.map((script) => script.id) })
          : undefined,
      )
      .catch((error: unknown) => {
        console.warn('DOPAGAKI: failed to unregister a work site', error);
      });
  });
  chrome.runtime.onInstalled.addListener(() => {
    void setup();
  });
  chrome.runtime.onStartup.addListener(() => {
    void setup();
  });
  void setup();
});
