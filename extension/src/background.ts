import { Draft, State } from '../../shared/models';
import { Connection, MANAGEMENT_ORIGIN, Reply, Request } from './protocol';

async function connection(): Promise<Connection | undefined> {
  return (await chrome.storage.session.get('connection'))['connection'] as Connection | undefined;
}
async function api<T>(path: string, method = 'GET', body?: unknown): Promise<T> {
  const config = await connection();
  if (!config) throw new Error('管理画面を開いて接続してください。');
  const response = await fetch(`${config.apiBase}/api${path}`, {
    method,
    headers: { Authorization: config.auth, 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const result = await response.json();
  if (!response.ok)
    throw new Error(result.error ?? '通信に失敗しました。管理画面から再接続できます。');
  return result as T;
}
async function publish(state: State) {
  await chrome.storage.local.set({ state, lastError: '' });
  await chrome.alarms.clear('task-start');
  const notified = ((await chrome.storage.local.get('notifiedPlans'))['notifiedPlans'] ??
    {}) as Record<string, string>;
  const next = state.tasks
    .filter((t) => t.status === 'pending' && notified[t.id] !== t.plan.start)
    .sort((a, b) => Date.parse(a.plan.start) - Date.parse(b.plan.start))[0];
  if (next)
    await chrome.alarms.create('task-start', {
      when: Math.max(Date.now() + 1000, Date.parse(next.plan.start)),
    });
  const tabs = await chrome.tabs.query({});
  await Promise.allSettled(
    tabs
      .filter((t) => t.id)
      .map((t) => chrome.tabs.sendMessage(t.id!, { type: 'STATE_UPDATED', state })),
  );
  await chrome.action.setBadgeText({ text: state.session ? '▶' : '' });
  await chrome.action.setBadgeBackgroundColor({ color: '#2858eb' });
}
async function refresh() {
  const state = await api<State>('/state');
  await publish(state);
  return state;
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

chrome.runtime.onMessage.addListener(
  (request: Request, sender, respond: (value: Reply) => void) => {
    if (!trustedPage(sender)) return;
    void (async (): Promise<Reply> => {
      if (request.type === 'CONNECT') {
        if (
          !trustedManagement(sender) ||
          request.connection.apiBase !== MANAGEMENT_ORIGIN ||
          !/^(Bearer |Demo )\S+$/.test(request.connection.auth)
        )
          throw new Error('接続元を確認してください。');
        await chrome.storage.session.set({ connection: request.connection });
        return { state: await refresh(), connected: true };
      }
      if (request.type === 'DISCONNECT') {
        if (!trustedManagement(sender)) throw new Error('接続元を確認してください。');
        await chrome.storage.session.remove('connection');
        await chrome.storage.local.remove(['state', 'notifiedPlans']);
        await chrome.alarms.clear('task-start');
        const tabs = await chrome.tabs.query({});
        await Promise.allSettled(
          tabs
            .filter((t) => t.id)
            .map((t) => chrome.tabs.sendMessage(t.id!, { type: 'STATE_UPDATED' })),
        );
        await chrome.action.setBadgeText({ text: '' });
        return { connected: false };
      }
      if (request.type === 'OPEN_APP') {
        await openApp(request.path);
        return {};
      }
      if (request.type === 'STATE') {
        if (!(await connection())) return { connected: false };
        return { state: await refresh(), connected: true };
      }
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
      let state: State;
      switch (request.type) {
        case 'START':
          state = await api<State>(`/tasks/${encodeURIComponent(request.taskId)}/start`, 'POST');
          break;
        case 'COMPLETE':
          state = await api<State>(`/tasks/${encodeURIComponent(request.taskId)}/complete`, 'POST');
          break;
        case 'PAUSE':
          state = await api<State>('/session/pause', 'POST');
          break;
        case 'COMMIT':
          state = await api<State>('/draft/commit', 'POST', request.draft);
          break;
        case 'VIEWING': {
          if (
            !sender.tab?.active ||
            !sender.url ||
            !new URL(sender.url).pathname.startsWith('/shorts/') ||
            new URL(sender.url).hostname !== 'www.youtube.com'
          )
            throw new Error('視聴中のShorts以外は計測しません。');
          state = await api<State>('/viewing', 'POST', {
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
  const tabs = await chrome.tabs.query({ active: true, lastFocusedWindow: true });
  let shown = false;
  if (tabs[0]?.id && tabs[0].url?.startsWith('https://www.youtube.com/shorts/')) {
    try {
      const response = await chrome.tabs.sendMessage(tabs[0].id, {
        type: 'SHOW_NOTICE',
        task,
        state,
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
      message: `${task.title}\n完了で視聴時間 ＋20分`,
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
  void api<State>(`/tasks/${encodeURIComponent(taskId)}/start`, 'POST')
    .then(async (state) => {
      await publish(state);
      const task = state.tasks.find((t) => t.id === taskId);
      if (task?.url) await chrome.tabs.create({ url: task.url });
      else await openApp();
    })
    .catch(() => openApp());
});
async function setup() {
  await chrome.alarms.create('sync', { periodInMinutes: 0.5 });
}
chrome.runtime.onInstalled.addListener(() => {
  void setup();
});
chrome.runtime.onStartup.addListener(() => {
  void setup();
});
void setup();
