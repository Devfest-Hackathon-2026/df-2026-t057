// 課題の開始時刻の通知、就寝前の声かけ、朝のあいさつ。
// 通知は chrome.alarms で各課題の `plan.start` に鳴らす。Shorts を見ていれば画面の上に、そうでなければデスクトップ通知で出す。
import type { CompanionMoment } from '@dopagaki/contract';
import { companionFallbacks } from '@dopagaki/messages';
import { z } from 'zod';
import { MANAGEMENT_ORIGIN } from '../lib/config';
import { copy, rewardDescription } from '../lib/copy';
import { shownReplySchema, type TabCommand } from '../lib/protocol';
import {
  alarmTime,
  inNight,
  isDue,
  nextTaskToNotify,
  pruneNotified,
  shouldPrefetchLine,
  type NotifiedPlans,
} from '../lib/schedule';
import type { ExtensionState, ExtensionTask } from '../lib/schemas';
import { isShortsUrl } from '../lib/shorts';
import { appUrl, safeHttpUrl } from '../lib/urls';
import { api } from './api';
import { connectionVersion } from './connection';
import { getState, patchState } from './store';
import { refetch } from './sync';

export const TASK_ALARM = 'task-start';
const NOTIFIED_KEY = 'notifiedPlans';
const LINES_KEY = 'noticeLines';
const BEDTIME_KEY = 'bedtimeShown';
const MORNING_KEY = 'morningCheckedAt';
const MORNING_INTERVAL_MS = 30 * 60_000;

const recordSchema = z.record(z.string(), z.string());

async function loadRecord(
  area: chrome.storage.StorageArea,
  key: string,
): Promise<Record<string, string>> {
  const parsed = recordSchema.safeParse((await area.get(key))[key]);
  return parsed.success ? parsed.data : {};
}

/** 相棒の一言。API が使えなければ決まった文にする。 */
export async function companionLine(moment: CompanionMoment, taskId?: string): Promise<string> {
  try {
    return (await api.companionLine(moment, taskId)) || companionFallbacks[moment];
  } catch {
    return companionFallbacks[moment];
  }
}

const lineKey = (task: ExtensionTask) => `${task.id}@${task.plan.start}`;

async function prefetchNoticeLine(task: ExtensionTask): Promise<void> {
  const lines = await loadRecord(chrome.storage.session, LINES_KEY);
  if (lines[lineKey(task)]) return;
  const line = await companionLine('notice', task.id);
  // 次の課題の分だけを持つ（古い一言を溜め込まない）。
  await chrome.storage.session.set({ [LINES_KEY]: { [lineKey(task)]: line } });
}

async function noticeLine(task: ExtensionTask): Promise<string> {
  const lines = await loadRecord(chrome.storage.session, LINES_KEY);
  return lines[lineKey(task)] ?? (await companionLine('notice', task.id));
}

/** 次に通知する課題の開始時刻にアラームを合わせる。状態が変わるたびに呼ぶ。 */
export async function scheduleTaskAlarm(state: ExtensionState | undefined): Promise<void> {
  await chrome.alarms.clear(TASK_ALARM);
  if (!state) return;
  const notified: NotifiedPlans = await loadRecord(chrome.storage.local, NOTIFIED_KEY);
  const next = nextTaskToNotify(state.tasks, notified);
  if (!next) return;
  const now = Date.now();
  await chrome.alarms.create(TASK_ALARM, { when: alarmTime(next, now) });
  if (shouldPrefetchLine(next, now)) void prefetchNoticeLine(next).catch(() => undefined);
}

/** 前面のウィンドウで Shorts を開いていれば、そのタブに表示を頼む。表示できたら true。 */
async function showOnActiveShorts(command: TabCommand): Promise<boolean> {
  const [tab] = await chrome.tabs.query({ active: true, lastFocusedWindow: true });
  if (tab?.id === undefined || !tab.url || !isShortsUrl(tab.url)) return false;
  try {
    const reply: unknown = await chrome.tabs.sendMessage(tab.id, command);
    const parsed = shownReplySchema.safeParse(reply);
    return parsed.success && parsed.data.shown;
  } catch {
    // ページの移動中などで届かないことがある。デスクトップ通知に切り替える。
    return false;
  }
}

/** アラームが鳴った。開始時刻を迎えた課題を知らせる。 */
export async function notifyDueTask(): Promise<void> {
  await refetch(['tasks', 'settings', 'wallet']).catch(() => undefined);
  const state = await getState();
  if (!state) return;
  const notified = await loadRecord(chrome.storage.local, NOTIFIED_KEY);
  const task = nextTaskToNotify(state.tasks, notified);
  if (!task || !isDue(task, Date.now())) {
    await scheduleTaskAlarm(state);
    return;
  }
  const line = await noticeLine(task);
  const shown = await showOnActiveShorts({ type: 'SHOW_NOTICE', task, state, line });
  if (!shown) {
    await chrome.notifications.create(`task:${task.id}`, {
      type: 'basic',
      iconUrl: chrome.runtime.getURL('icon-128.png'),
      title: copy.notificationTitle,
      message: `${task.title}\n${line || rewardDescription(state.wallet)}`,
      buttons: [{ title: copy.notificationStart }, { title: copy.notificationSnooze }],
      priority: 2,
      requireInteraction: true,
    });
  }
  await chrome.storage.local.set({
    [NOTIFIED_KEY]: { ...pruneNotified(state.tasks, notified), [task.id]: task.plan.start },
  });
  await scheduleTaskAlarm(state);
}

/** 夜の時間帯に Shorts を見ていたら、一晩に一度だけ静かに声をかける。何も止めない。 */
export async function remindBedtime(state: ExtensionState | undefined): Promise<void> {
  const night = state?.sleep?.night;
  if (!state || !night || state.workSession || !inNight(night, Date.now())) return;
  const shownFor: unknown = (await chrome.storage.local.get(BEDTIME_KEY))[BEDTIME_KEY];
  if (shownFor === night.start) return;
  const [tab] = await chrome.tabs.query({ active: true, lastFocusedWindow: true });
  if (!tab?.url || !isShortsUrl(tab.url)) return;
  const line = await companionLine('night');
  if (await showOnActiveShorts({ type: 'SHOW_NIGHT', state, line })) {
    await chrome.storage.local.set({ [BEDTIME_KEY]: night.start });
  }
}

/** 起きたあと、管理画面を開かなくても前の夜を精算する（30分に1回まで）。 */
export async function greetMorning(state: ExtensionState | undefined): Promise<void> {
  const night = state?.sleep?.night;
  if (!state?.sleep || inNight(night, Date.now())) return;
  const checkedAt: unknown = (await chrome.storage.local.get(MORNING_KEY))[MORNING_KEY];
  if (typeof checkedAt === 'number' && Date.now() - checkedAt < MORNING_INTERVAL_MS) return;
  await chrome.storage.local.set({ [MORNING_KEY]: Date.now() });
  const version = connectionVersion();
  const morning = await api.settleMorning();
  if (!morning) return;
  await refetch(['wallet', 'viewing', 'sleep']).catch(() => undefined);
  if (version !== connectionVersion()) return;
  const gains = [
    morning.kept ? copy.morningBonus(morning.bonusSeconds) : '',
    morning.releasedSeconds ? copy.morningReleased(morning.releasedSeconds) : '',
  ].filter(Boolean);
  const line = await companionLine('morning');
  await chrome.notifications.create('morning', {
    type: 'basic',
    iconUrl: chrome.runtime.getURL('icon-128.png'),
    title: copy.morningTitle,
    message: [line, ...gains].join('\n'),
  });
}

export async function openApp(query?: Readonly<Record<string, string>>): Promise<void> {
  await chrome.tabs.create({ url: appUrl(MANAGEMENT_ORIGIN, query) });
}

async function startFromNotification(taskId: string): Promise<void> {
  const version = connectionVersion();
  try {
    const workSession = await api.startTask(taskId);
    await patchState(version, { workSession, workSyncedAt: Date.now() });
    void refetch(['tasks']).catch(() => undefined);
    const task = (await getState())?.tasks.find((item) => item.id === taskId);
    const url = task ? safeHttpUrl(task.url) : undefined;
    if (url) await chrome.tabs.create({ url });
    else await openApp();
  } catch {
    await openApp();
  }
}

export function listenNotificationClicks(): void {
  chrome.notifications.onClicked.addListener((id) => {
    if (id === 'morning') void openApp();
    if (id.startsWith('task:')) void openApp({ notification: id.slice(5) });
    void chrome.notifications.clear(id);
  });
  chrome.notifications.onButtonClicked.addListener((id, index) => {
    if (!id.startsWith('task:')) return;
    const taskId = id.slice(5);
    void chrome.notifications.clear(id);
    if (index === 1) void openApp({ snooze: taskId });
    else void startFromNotification(taskId);
  });
}

/** アカウントの切り替え・接続の解除で、前のアカウントの通知と記録を消す。 */
export async function clearNotifications(): Promise<void> {
  await chrome.storage.local.remove([NOTIFIED_KEY, BEDTIME_KEY, MORNING_KEY]);
  await chrome.storage.session.remove(LINES_KEY);
  await chrome.alarms.clear(TASK_ALARM);
  const all = await chrome.notifications.getAll();
  await Promise.all(
    Object.keys(all)
      .filter((id) => id.startsWith('task:') || id === 'morning')
      .map((id) => chrome.notifications.clear(id)),
  );
}
