import { Draft, State, nightWindow } from '../../shared/models';
import {
  Connection,
  ExtensionState,
  ExtensionTask,
  MANAGEMENT_ORIGIN,
  Moment,
  Reply,
  Request,
} from './protocol';

let connectionVersion = 0;

async function connection(): Promise<Connection | undefined> {
  return (await chrome.storage.session.get('connection'))['connection'] as Connection | undefined;
}
async function api<T>(path: string, method = 'GET', body?: unknown): Promise<T> {
  const version = connectionVersion;
  await connectionChange;
  const config = await connection();
  if (version !== connectionVersion) throw new Error('アカウントが切り替わりました。');
  if (!config) throw new Error('管理画面を開いて接続してください。');
  const response = await fetch(`${config.apiBase}/api${path}`, {
    method,
    headers: { Authorization: config.auth, 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const result = await response.json();
  if (version !== connectionVersion)
    throw new Error('アカウントが切り替わりました。管理画面から再接続してください。');
  if (!response.ok)
    throw new Error(result.error ?? '通信に失敗しました。管理画面から再接続できます。');
  return result as T;
}
async function apiState(path: string, method = 'GET', body?: unknown): Promise<ExtensionState> {
  const state = await api<State>(path, method, body);
  return {
    balanceSeconds: state.balanceSeconds,
    settings: state.settings,
    session: state.session,
    nextDraft: state.nextDraft,
    sleep: state.sleep && { bedtime: state.sleep.bedtime, wake: state.sleep.wake },
    timeZone: state.profile.timeZone,
    tasks: state.tasks.map(({ id, title, why, minutes, url, status, plan }) => ({
      id,
      title,
      why,
      minutes,
      url,
      status,
      plan,
    })),
  };
}
async function publish(state: ExtensionState, version = connectionVersion) {
  if (version !== connectionVersion) return;
  await chrome.storage.local.set({ state, lastError: '' });
  await chrome.alarms.clear('task-start');
  if (version !== connectionVersion) return;
  const notified = ((await chrome.storage.local.get('notifiedPlans'))['notifiedPlans'] ??
    {}) as Record<string, string>;
  const next = state.tasks
    .filter((t) => t.status === 'pending' && notified[t.id] !== t.plan.start)
    .sort((a, b) => Date.parse(a.plan.start) - Date.parse(b.plan.start))[0];
  if (next) {
    await chrome.alarms.create('task-start', {
      when: Math.max(Date.now() + 1000, Date.parse(next.plan.start)),
    });
    if (Date.parse(next.plan.start) - Date.now() < 5 * 60000) void prefetchNotice(next);
  }
  const tabs = await chrome.tabs.query({});
  if (version !== connectionVersion) return;
  await Promise.allSettled(
    tabs
      .filter((t) => t.id)
      .map((t) => chrome.tabs.sendMessage(t.id!, { type: 'STATE_UPDATED', state })),
  );
  await chrome.action.setBadgeText({ text: state.session ? '▶' : '' });
  await chrome.action.setBadgeBackgroundColor({ color: '#2858eb' });
}
async function refresh() {
  const version = connectionVersion;
  const state = await apiState('/state');
  await publish(state, version);
  void remindBedtime(state).catch(() => undefined);
  return state;
}
// Once a night, a quiet bedtime reminder on top of Shorts. Nothing is blocked.
async function remindBedtime(state: ExtensionState) {
  const night = state.sleep && nightWindow(state.sleep, state.timeZone);
  if (!night || state.session) return;
  const { bedtimeShown } = await chrome.storage.local.get('bedtimeShown');
  if (bedtimeShown === night.key) return;
  const [tab] = await chrome.tabs.query({ active: true, lastFocusedWindow: true });
  if (!tab?.id || !tab.url?.startsWith('https://www.youtube.com/shorts/')) return;
  const line = (await companionLine('night')) || '';
  const response = await chrome.tabs
    .sendMessage(tab.id, { type: 'SHOW_NIGHT', state, line })
    .catch(() => undefined);
  if (response?.shown) await chrome.storage.local.set({ bedtimeShown: night.key });
}
// One short remark from the companion; empty when the API is unavailable.
async function companionLine(moment: Moment, taskId?: string) {
  try {
    return (await api<{ line: string }>('/companion/line', 'POST', { moment, taskId })).line;
  } catch {
    return '';
  }
}
// The notice line is fetched a few minutes ahead so the notice never waits on Gemini.
async function prefetchNotice(task: ExtensionTask) {
  const key = `${task.id}@${task.plan.start}`;
  const lines = ((await chrome.storage.session.get('noticeLines'))['noticeLines'] ?? {}) as Record<
    string,
    string
  >;
  if (lines[key]) return;
  const line = await noticeLine(task);
  if (line) await chrome.storage.session.set({ noticeLines: { [key]: line } });
}
async function noticeLine(task: ExtensionTask) {
  const lines = ((await chrome.storage.session.get('noticeLines'))['noticeLines'] ?? {}) as Record<
    string,
    string
  >;
  return lines[`${task.id}@${task.plan.start}`] || companionLine('notice', task.id);
}
async function openApp(path = '') {
  const config = await connection();
  await chrome.tabs.create({ url: (config?.apiBase ?? MANAGEMENT_ORIGIN) + '/' + path });
}
function trustedManagement(sender: chrome.runtime.MessageSender): boolean {
  return !!sender.url && new URL(sender.url).origin === MANAGEMENT_ORIGIN;
}
function trustedPage(sender: chrome.runtime.MessageSender): boolean {
  return sender.id === chrome.runtime.id;
}

async function clearAccount() {
  await chrome.storage.local.remove(['state', 'notifiedPlans', 'lastError']);
  await chrome.alarms.clear('task-start');
  const notifications = await chrome.notifications.getAll();
  await Promise.all(
    Object.keys(notifications)
      .filter((id) => id.startsWith('task:'))
      .map((id) => chrome.notifications.clear(id)),
  );
  const tabs = await chrome.tabs.query({});
  await Promise.allSettled(
    tabs.filter((t) => t.id).map((t) => chrome.tabs.sendMessage(t.id!, { type: 'STATE_UPDATED' })),
  );
  await chrome.action.setBadgeText({ text: '' });
}

let connectionChange = Promise.resolve();
function replaceConnection(next?: Connection): Promise<void> {
  connectionVersion++;
  const change = connectionChange.then(async () => {
    const previous = await connection();
    await chrome.storage.session.remove('connection');
    if (!next || next.accountId !== previous?.accountId) await clearAccount();
    if (next) await chrome.storage.session.set({ connection: next });
  });
  // Return failures to the caller while allowing the next connection attempt to run.
  connectionChange = change.catch(() => {});
  return change;
}

chrome.runtime.onMessage.addListener(
  (request: Request, sender, respond: (value: Reply) => void) => {
    if (!trustedPage(sender)) return;
    void (async (): Promise<Reply> => {
      if (request.type === 'CONNECT') {
        if (
          !trustedManagement(sender) ||
          request.connection.apiBase !== MANAGEMENT_ORIGIN ||
          typeof request.connection.accountId !== 'string' ||
          !request.connection.accountId ||
          !/^(Bearer |Demo )\S+$/.test(request.connection.auth)
        )
          throw new Error('接続元を確認してください。');
        await replaceConnection(request.connection);
        return { state: await refresh(), connected: true };
      }
      if (request.type === 'DISCONNECT') {
        if (!trustedManagement(sender)) throw new Error('接続元を確認してください。');
        await replaceConnection();
        return { connected: false };
      }
      if (request.type === 'OPEN_APP') {
        await openApp(request.path);
        return {};
      }
      if (request.type === 'CLOSE_TAB') {
        if (sender.tab?.id) await chrome.tabs.remove(sender.tab.id);
        return {};
      }
      if (request.type === 'STATE') {
        if (!(await connection())) return { connected: false };
        return { state: await refresh(), connected: true };
      }
      if (request.type === 'LINE')
        return { line: await companionLine(request.moment, request.taskId) };
      if (request.type === 'DRAFT')
        return {
          draft: await api<Draft>(
            `/tasks/${encodeURIComponent(request.taskId)}/draft?adjust=${request.adjust}`,
          ),
        };
      if (request.type === 'ADJUST')
        return {
          draft: await api<Draft>('/draft/adjust', 'POST', {
            draft: request.draft,
            instruction: request.instruction,
          }),
        };
      let state: ExtensionState;
      switch (request.type) {
        case 'START':
          state = await apiState(`/tasks/${encodeURIComponent(request.taskId)}/start`, 'POST');
          break;
        case 'COMPLETE':
          state = await apiState(`/tasks/${encodeURIComponent(request.taskId)}/complete`, 'POST');
          break;
        case 'PAUSE':
          state = await apiState('/session/pause', 'POST');
          break;
        case 'COMMIT':
          state = await apiState('/draft/commit', 'POST', request.draft);
          break;
        case 'VIEWING': {
          if (
            !sender.tab?.active ||
            !sender.url ||
            !new URL(sender.url).pathname.startsWith('/shorts/') ||
            new URL(sender.url).hostname !== 'www.youtube.com'
          )
            throw new Error('視聴中のShorts以外は計測しません。');
          state = await apiState('/viewing', 'POST', {
            id: request.id,
            seconds: request.seconds,
          });
          break;
        }
      }
      await publish(state);
      return { state };
    })()
      .then(respond)
      .catch(async (error) => {
        const text = error instanceof Error ? error.message : '通信に失敗しました。';
        await chrome.storage.local.set({ lastError: text });
        respond({ error: text });
      });
    return true;
  },
);

async function notify() {
  const state = await refresh();
  const notified = ((await chrome.storage.local.get('notifiedPlans'))['notifiedPlans'] ??
    {}) as Record<string, string>;
  const task = state.tasks
    .filter((t) => t.status === 'pending' && notified[t.id] !== t.plan.start)
    .sort((a, b) => Date.parse(a.plan.start) - Date.parse(b.plan.start))[0];
  if (!task || Date.parse(task.plan.start) > Date.now() + 1000) return;
  const line = await noticeLine(task);
  const tabs = await chrome.tabs.query({ active: true, lastFocusedWindow: true });
  let shown = false;
  if (tabs[0]?.id && tabs[0].url?.startsWith('https://www.youtube.com/shorts/')) {
    try {
      const response = await chrome.tabs.sendMessage(tabs[0].id, {
        type: 'SHOW_NOTICE',
        task,
        state,
        line,
      });
      shown = response?.shown === true;
    } catch {
      /* The tab may be navigating. Fall back to a desktop notification. */
    }
  }
  if (!shown)
    await chrome.notifications.create(`task:${task.id}`, {
      type: 'basic',
      iconUrl: 'icon-128.png',
      title: 'DOPAGAKI · はじめる時間です',
      message: `${task.title}\n${line || '完了で視聴時間 ＋20分'}`,
      buttons: [{ title: '作業をはじめる' }, { title: 'スヌーズの予定を確認' }],
      priority: 2,
      requireInteraction: true,
    });
  notified[task.id] = task.plan.start;
  await chrome.storage.local.set({ notifiedPlans: notified });
  await publish(state);
}
chrome.alarms.onAlarm.addListener((alarm) => {
  if (!['task-start', 'sync'].includes(alarm.name)) return;
  void (alarm.name === 'task-start' ? notify() : refresh()).catch(async (error) => {
    await chrome.storage.local.set({ lastError: error.message });
  });
});
chrome.notifications.onClicked.addListener((id) => {
  if (id.startsWith('task:')) void openApp(`?notification=${encodeURIComponent(id.slice(5))}`);
});
chrome.notifications.onButtonClicked.addListener((id, index) => {
  if (!id.startsWith('task:')) return;
  const taskId = id.slice(5);
  if (index === 1) {
    void openApp(`?snooze=${encodeURIComponent(taskId)}`);
    return;
  }
  void apiState(`/tasks/${encodeURIComponent(taskId)}/start`, 'POST')
    .then(async (state) => {
      await publish(state);
      const task = state.tasks.find((t) => t.id === taskId);
      if (task?.url) await chrome.tabs.create({ url: task.url });
      else await openApp();
    })
    .catch(() => openApp());
});
async function setup() {
  await chrome.storage.local.setAccessLevel({ accessLevel: 'TRUSTED_CONTEXTS' });
  await chrome.alarms.create('sync', { periodInMinutes: 0.5 });
}
chrome.runtime.onInstalled.addListener(() => {
  void setup();
});
chrome.runtime.onStartup.addListener(() => {
  void setup();
});
void setup();
