import { extensionConnection, signIn, disconnectAuth, receiveCredential, authStatus } from './auth';
import { Draft, Morning, State, nightWindow, rewardDescription } from '../../shared/models';
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
  const own = await extensionConnection();
  if (own !== undefined) return own ?? undefined;
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
  return project(await api<State>(path, method, body));
}
// Only what the extension needs leaves the API response.
function project(state: State): ExtensionState {
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
  void remindBedtime(state).catch(() => undefined);
  void greetMorning(state, version).catch(() => undefined);
  return state;
}
// After wake, settle last night (held rewards and the early-bedtime bonus) without opening the app.
async function greetMorning(state: ExtensionState, version: number) {
  if (!state.sleep || nightWindow(state.sleep, state.timeZone)) return;
  const { morningCheckedAt } = await chrome.storage.local.get('morningCheckedAt');
  if (Date.now() - Number(morningCheckedAt ?? 0) < 30 * 60000) return;
  await chrome.storage.local.set({ morningCheckedAt: Date.now() });
  const result = await api<{ state: State; morning: Morning | null }>('/sleep/morning', 'POST');
  if (!result.morning) return;
  await publish(project(result.state), version);
  const gains = [
    result.morning.kept ? `早寝ボーナス ＋${result.morning.bonusSeconds / 60}分` : '',
    result.morning.releasedSeconds ? `昨夜の報酬 ＋${result.morning.releasedSeconds / 60}分` : '',
  ].filter(Boolean);
  const line = (await companionLine('morning')) || 'おはよう。今日の最初の一手、一緒に決めよっか。';
  await chrome.notifications.create('morning', {
    type: 'basic',
    iconUrl: 'icon-128.png',
    title: 'DOPAGAKI · おはよう',
    message: [line, ...gains].join('\n'),
  });
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
  await chrome.storage.local.remove([
    'state',
    'notifiedPlans',
    'lastError',
    'bedtimeShown',
    'morningCheckedAt',
    'viewingOwner',
    'viewingPending',
  ]);
  await chrome.storage.session.remove('noticeLines');
  await chrome.alarms.clear('task-start');
  const notifications = await chrome.notifications.getAll();
  await Promise.all(
    Object.keys(notifications)
      .filter((id) => id.startsWith('task:') || id === 'morning')
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
  (request: Request & { target?: string }, sender, respond: (value: Reply) => void) => {
    if (!request || typeof request !== 'object' || typeof request.type !== 'string' || !trustedPage(sender) || request.target === 'offscreen') return;
    if (request.type === 'AUTH_RESULT') {
      if (sender.url === chrome.runtime.getURL('offscreen.html'))
        void receiveCredential(request.credential);
      return;
    }
    void (async (): Promise<Reply> => {
      if (
        ['AUTH_SIGN_IN', 'AUTH_SIGN_OUT', 'TEST_NOTICE'].includes(request.type) &&
        sender.url !== chrome.runtime.getURL('popup.html')
      )
        throw new Error('拡張のメニューから操作してください。');
      if (request.type === 'AUTH_SIGN_IN') {
        connectionVersion++;
        await clearAccount();
        await signIn();
        return { state: await refresh(), connected: true };
      }
      if (request.type === 'AUTH_SIGN_OUT') {
        await disconnectAuth();
        await replaceConnection();
        return { connected: false };
      }
      if (request.type === 'DIAGNOSTICS')
        return {
          diagnostics: {
            ...(await authStatus()),
            notificationPermission: await chrome.notifications.getPermissionLevel(),
            protocolVersion: 2,
          },
        };
      if (request.type === 'TEST_NOTICE') {
        await chrome.notifications.create('dopagaki-test', {
          type: 'basic',
          iconUrl: 'icon-128.png',
          title: 'DOPAGAKI 接続テスト',
          message: 'この通知が見えたら、設定画面の「見えた」を押してください。',
        });
        return {};
      }
      if (request.type === 'CONNECT') {
        if (
          !trustedManagement(sender) ||
          request.connection.apiBase !== MANAGEMENT_ORIGIN ||
          typeof request.connection.accountId !== 'string' ||
          !request.connection.accountId ||
          (request.connection.auth !== undefined &&
            !/^(Bearer |Demo )\S+$/.test(request.connection.auth))
        )
          throw new Error('接続元を確認してください。');
        const own = await extensionConnection();
        if (own !== undefined) {
          if (!own || own.accountId !== request.connection.accountId)
            throw new Error('拡張で同じGoogleアカウントへ接続してください。');
          return { state: await refresh(), connected: true };
        }
        const current = await connection();
        if (typeof request.connection.auth !== 'string')
          throw new Error('デモの接続情報を確認してください。');
        if (
          !current ||
          current.accountId !== request.connection.accountId ||
          current.auth !== request.connection.auth ||
          current.apiBase !== request.connection.apiBase
        )
          await replaceConnection({ ...request.connection, auth: request.connection.auth });
        return { state: await refresh(), connected: true };
      }
      if (request.type === 'DISCONNECT') {
        if (!trustedManagement(sender)) throw new Error('接続元を確認してください。');
        const own = await extensionConnection();
        if (own && own.accountId !== request.accountId) return {};
        await disconnectAuth();
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
        const state = await refresh();
        const owner = (await chrome.storage.local.get('viewingOwner'))['viewingOwner'] as
          { tabId: number } | undefined;
        return { state, connected: true, viewingOwnerTabId: owner?.tabId, tabId: sender.tab?.id };
      }
      if (['START_VIEWING', 'CLAIM_VIEWING', 'END_VIEWING', 'VIEWING'].includes(request.type))
        return changeViewing(request as ViewingRequest, sender);
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
          {
            const current = await apiState('/state');
            state = await apiState('/session/pause', 'PUT', { paused: !current.session?.pausedAt });
          }
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
      message: `${task.title}\n${line || rewardDescription(state)}`,
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
  if (id === 'morning') void openApp();
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
