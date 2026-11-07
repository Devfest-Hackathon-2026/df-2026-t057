import { Draft, State, rewardDescription } from '../../shared/models';
import { Connection, ExtensionState, MANAGEMENT_ORIGIN, Reply, Request } from './protocol';

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
    revision: state.revision,
    balanceSeconds: state.balanceSeconds,
    viewingSession: state.viewingSession,
    timePolicy: state.timePolicy
      ? {
          version: state.timePolicy.version,
          mode: state.timePolicy.mode,
          dailyLimitSeconds: state.timePolicy.dailyLimitSeconds,
          viewedSeconds: state.timePolicy.viewedSeconds,
        }
      : undefined,
    workRewardSeconds: (state.ledger ?? [])
      .filter((e) => e.kind === 'work')
      .reduce((sum, e) => sum + e.seconds, 0),
    settings: state.settings,
    session: state.session,
    nextDraft: state.nextDraft,
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
  const owner = (await chrome.storage.local.get('viewingOwner'))['viewingOwner'] as
    { tabId: number; sessionId: string } | undefined;
  const previous = (await chrome.storage.local.get('state'))['state'] as ExtensionState | undefined;
  if (version !== connectionVersion) return;
  if (previous && previous.revision > state.revision) return;
  await chrome.storage.local.set({ state, lastError: '' });
  await chrome.alarms.clear('task-start');
  if (version !== connectionVersion) return;
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
  if (version !== connectionVersion) return;
  await Promise.allSettled(
    tabs
      .filter((t) => t.id)
      .map((t) =>
        chrome.tabs.sendMessage(t.id!, {
          type: 'STATE_UPDATED',
          state,
          viewingOwnerTabId: owner?.tabId,
          tabId: t.id,
        }),
      ),
  );
  await chrome.action.setBadgeText({ text: state.session ? '▶' : '' });
  await chrome.action.setBadgeBackgroundColor({ color: '#2858eb' });
}
async function refresh() {
  const version = connectionVersion;
  const settled = viewingChange.then(() => flushViewing());
  viewingChange = settled.catch(() => {});
  await settled;
  const state = await apiState('/state');
  await publish(state, version);
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

async function clearAccount() {
  await chrome.storage.local.remove([
    'state',
    'notifiedPlans',
    'lastError',
    'viewingOwner',
    'viewingPending',
  ]);
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

type ViewingRequest = Extract<
  Request,
  { type: 'START_VIEWING' | 'CLAIM_VIEWING' | 'END_VIEWING' | 'VIEWING' }
>;
let viewingChange: Promise<unknown> = Promise.resolve();
async function flushViewing(): Promise<ExtensionState | undefined> {
  const version = connectionVersion;
  const pending = (await chrome.storage.local.get('viewingPending'))['viewingPending'] as
    | { id: string; seconds: number; sessionId?: string; accountId: string; policyVersion?: number }
    | undefined;
  if (!pending) return;
  const account = await connection();
  if (version !== connectionVersion) throw new Error('アカウントが切り替わりました。');
  if (pending.accountId !== account?.accountId) {
    await chrome.storage.local.remove('viewingPending');
    return;
  }
  const { accountId: _, policyVersion, ...report } = pending;
  let state: ExtensionState;
  try {
    state = await apiState('/viewing', 'POST', report);
  } catch (error) {
    if (version !== connectionVersion) throw error;
    state = await apiState('/state');
    if ((state.timePolicy?.version ?? 0) <= (policyVersion ?? 0)) throw error;
  }
  if (version !== connectionVersion) throw new Error('アカウントが切り替わりました。');
  await chrome.storage.local.remove('viewingPending');
  return state;
}
function changeViewing(
  request: ViewingRequest,
  sender: chrome.runtime.MessageSender,
): Promise<Reply> {
  const version = connectionVersion;
  const change = viewingChange.then(async (): Promise<Reply> => {
    if (version !== connectionVersion) throw new Error('アカウントが切り替わりました。');
    if (
      !sender.tab?.id ||
      !sender.tab.active ||
      !sender.url ||
      new URL(sender.url).hostname !== 'www.youtube.com' ||
      !new URL(sender.url).pathname.startsWith('/shorts/')
    )
      throw new Error('視聴中のShortsから操作してください。');
    const accountId = (await connection())?.accountId;
    if (!accountId) throw new Error('管理画面から接続してください。');
    let state = await flushViewing();
    let owner = (await chrome.storage.local.get('viewingOwner'))['viewingOwner'] as
      { tabId: number; sessionId: string } | undefined;
    if (request.type === 'START_VIEWING') {
      state = await apiState('/viewing/start', 'POST', {
        id: request.id,
        seconds: request.seconds,
        extendFrom: request.extendFrom,
      });
      if (version !== connectionVersion) throw new Error('アカウントが切り替わりました。');
      owner = { tabId: sender.tab.id, sessionId: state.viewingSession!.id };
      await chrome.storage.local.set({ viewingOwner: owner });
    } else if (request.type === 'CLAIM_VIEWING') {
      state = await apiState('/state');
      if (version !== connectionVersion) throw new Error('アカウントが切り替わりました。');
      if (
        !state.viewingSession ||
        state.viewingSession.id !== request.sessionId ||
        state.viewingSession.endedAt
      )
        throw new Error('この視聴の区切りは終了しています。');
      owner = { tabId: sender.tab.id, sessionId: request.sessionId };
      await chrome.storage.local.set({ viewingOwner: owner });
    } else {
      const cached = (await chrome.storage.local.get('state'))['state'] as
        ExtensionState | undefined;
      if (
        cached?.timePolicy &&
        cached.timePolicy.mode !== 'self' &&
        (owner?.tabId !== sender.tab.id || owner.sessionId !== request.sessionId)
      )
        throw new Error('このタブで続ける場合は、視聴の区切りを引き継いでください。');
      if (request.type === 'END_VIEWING') {
        state = await apiState('/viewing/end', 'POST', {
          id: request.sessionId,
          reason: request.reason,
        });
      } else {
        if (version !== connectionVersion) throw new Error('アカウントが切り替わりました。');
        const report = {
          id: request.id,
          seconds: request.seconds,
          sessionId: request.sessionId,
          accountId,
          policyVersion: cached?.timePolicy?.version,
        };
        await chrome.storage.local.set({ viewingPending: report });
        state = await flushViewing();
      }
    }
    await publish(state!, version);
    return { state, viewingOwnerTabId: owner?.tabId, tabId: sender.tab.id };
  });
  viewingChange = change.catch(() => {});
  return change;
}
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
        const current = await connection();
        if (
          !current ||
          current.accountId !== request.connection.accountId ||
          current.auth !== request.connection.auth ||
          current.apiBase !== request.connection.apiBase
        )
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
      if (request.type === 'STATE') {
        if (!(await connection())) return { connected: false };
        const state = await refresh();
        const owner = (await chrome.storage.local.get('viewingOwner'))['viewingOwner'] as
          { tabId: number } | undefined;
        return { state, connected: true, viewingOwnerTabId: owner?.tabId, tabId: sender.tab?.id };
      }
      if (['START_VIEWING', 'CLAIM_VIEWING', 'END_VIEWING', 'VIEWING'].includes(request.type))
        return changeViewing(request as ViewingRequest, sender);
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
        case 'CONFIRM_WORK':
          state = await apiState('/session/confirm', 'POST', { id: request.id });
          break;
        case 'COMMIT':
          state = await apiState('/draft/commit', 'POST', request.draft);
          break;
        default:
          throw new Error('操作を確認してください。');
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
      message: `${task.title}\n${rewardDescription(state)}`,
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
