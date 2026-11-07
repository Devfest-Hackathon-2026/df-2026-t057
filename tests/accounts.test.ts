import test from 'node:test';
import assert from 'node:assert/strict';
import vm from 'node:vm';
import { build } from 'esbuild';

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: unknown) => void;
  const promise = new Promise<T>((done, fail) => {
    resolve = done;
    reject = fail;
  });
  return { promise, resolve, reject };
}
const json = (body: unknown) => ({
  ok: true,
  headers: { get: () => 'application/json' },
  json: async () => body,
});

test('account switch discards an older state response and clears the signed-out view', async () => {
  const auth = {
    changed: undefined as unknown as (user: unknown) => Promise<void>,
    popup: deferred<void>(),
    loginCalls: 0,
    parameters: {} as Record<string, string>,
  };
  const messages: { type: string; accountId?: string }[] = [];
  const oldRequest = deferred<ReturnType<typeof json>>();
  const started = deferred<void>();
  const result = await build({
    entryPoints: ['web/src/app/api.ts'],
    bundle: true,
    write: false,
    format: 'cjs',
    platform: 'node',
    plugins: [
      {
        name: 'auth-fixture',
        setup(b) {
          b.onResolve({ filter: /^(@angular\/core|firebase\/)/ }, ({ path }) => ({
            path,
            namespace: 'fixture',
          }));
          b.onLoad({ filter: /.*/, namespace: 'fixture' }, ({ path }) => ({
            contents:
              path === '@angular/core'
                ? 'exports.Injectable=()=>x=>x; exports.signal=v=>Object.assign(()=>v,{set:n=>{v=n}});'
                : path === 'firebase/app'
                  ? 'exports.initializeApp=x=>x;'
                  : `exports.getAuth=()=>({});
                     exports.onIdTokenChanged=(_,cb)=>{fixture.changed=cb; queueMicrotask(()=>cb(null))};
                     exports.GoogleAuthProvider=class {setCustomParameters(p){fixture.parameters=p}};
                     exports.signInWithPopup=()=>{fixture.loginCalls++;return fixture.popup.promise};
                     exports.signOut=()=>fixture.changed(null);`,
          }));
        },
      },
    ],
  });
  const module = { exports: {} as any };
  vm.runInNewContext(result.outputFiles[0].text, {
    module,
    exports: module.exports,
    fixture: auth,
    queueMicrotask,
    window: { postMessage: (m: any) => messages.push(m) },
    location: { origin: 'https://app.example' },
    fetch: async (path: string, options: any) => {
      if (path.endsWith('/config')) return json({ mode: 'live', firebase: { apiKey: 'fixture' } });
      if (options.headers.Authorization === 'Bearer a') {
        started.resolve();
        return oldRequest.promise;
      }
      return json({ balanceSeconds: 3600, tasks: [], owner: 'b' });
    },
  });
  const api = new module.exports.Api();
  await api.init();
  const a = auth.changed({ uid: 'a', email: 'a@example.com', getIdToken: async () => 'a' });
  await started.promise;
  await auth.changed({
    uid: 'b',
    email: 'b@example.com',
    displayName: 'B',
    photoURL: 'https://example.com/b.png',
    getIdToken: async () => 'b',
  });
  oldRequest.resolve(json({ balanceSeconds: 9999, tasks: [], owner: 'a' }));
  await a;
  assert.equal(api.state().owner, 'b');
  assert.equal(api.accountEmail(), 'b@example.com');
  assert.equal(api.accountName(), 'B');
  assert.equal(api.accountPhoto(), 'https://example.com/b.png');
  assert.equal(api.loading(), false);
  assert.equal(messages.filter((m) => m.type === 'DOPAGAKI_CONNECT').length, 1);
  assert.equal(messages.find((m) => m.type === 'DOPAGAKI_CONNECT')?.accountId, 'b');
  const login = api.login();
  assert.equal(api.authPending(), true);
  assert.equal(api.busy(), true);
  await api.login();
  assert.equal(auth.loginCalls, 1);
  assert.equal(auth.parameters.prompt, 'select_account');
  auth.popup.resolve();
  await login;
  assert.equal(api.authPending(), false);
  assert.equal(api.busy(), false);

  auth.popup = deferred<void>();
  const cancelled = api.login();
  auth.popup.reject({ code: 'auth/popup-closed-by-user' });
  assert.equal(await cancelled, false);
  assert.equal(api.authPending(), false);
  assert.equal(api.busy(), false);
  assert.match(api.error(), /キャンセル/);
  assert.equal(api.state().owner, 'b');

  await api.logout();
  assert.equal(api.state(), null);
  assert.equal(api.accountEmail(), '');
  assert.equal(api.accountName(), '');
  assert.equal(api.accountPhoto(), '');
  assert.equal(messages.at(-1)?.type, 'DOPAGAKI_DISCONNECT');
});

test('extension account switch removes old notices and rejects an in-flight response', async () => {
  const session: Record<string, any> = {};
  const local: Record<string, any> = {};
  const sent: any[] = [];
  const cleared: string[] = [];
  const accessLevels: string[] = [];
  const held = deferred<ReturnType<typeof json>>();
  const started = deferred<void>();
  let delay = false;
  let listener!: (request: any, sender: any, respond: (reply: any) => void) => void;
  const storage = (data: Record<string, any>) => ({
    get: async (key: string) => ({ [key]: data[key] }),
    set: async (values: any) => {
      Object.assign(data, values);
    },
    remove: async (keys: string | string[]) => {
      for (const k of [keys].flat()) delete data[k];
    },
  });
  const event = { addListener() {} };
  const chrome = {
    storage: {
      session: storage(session),
      local: {
        ...storage(local),
        setAccessLevel: async ({ accessLevel }: { accessLevel: string }) => {
          accessLevels.push(accessLevel);
        },
      },
    },
    runtime: {
      id: 'fixture',
      onMessage: {
        addListener: (cb: any) => {
          listener = cb;
        },
      },
      onInstalled: event,
      onStartup: event,
    },
    alarms: { clear: async () => true, create: async () => {}, onAlarm: event },
    notifications: {
      getAll: async () => ({ 'task:old': {}, unrelated: {} }),
      clear: async (id: string) => {
        cleared.push(id);
      },
      onClicked: event,
      onButtonClicked: event,
    },
    tabs: {
      query: async () => [{ id: 1 }],
      sendMessage: async (_: number, m: any) => {
        sent.push(m);
      },
    },
    action: { setBadgeText: async () => {}, setBadgeBackgroundColor: async () => {} },
  };
  const result = await build({
    entryPoints: ['extension/src/background.ts'],
    bundle: true,
    write: false,
    format: 'cjs',
    platform: 'node',
  });
  vm.runInNewContext(
    result.outputFiles[0].text.replaceAll('__MANAGEMENT_ORIGIN__', 'https://app.example'),
    {
      chrome,
      URL,
      console,
      fetch: async (_url: string, options: any) => {
        if (delay && options.headers.Authorization === 'Bearer a') {
          started.resolve();
          return held.promise;
        }
        return json({
          tasks: [
            {
              id: 'completed',
              title: 'task',
              notes: 'private notes',
              calendarEventId: 'private-calendar-id',
              status: 'completed',
              plan: {},
            },
          ],
          balanceSeconds: options.headers.Authorization === 'Bearer b' ? 7200 : 3600,
          profile: { context: 'private profile' },
          ledger: [{ id: 'private history' }],
          memoryReceipts: { private: true },
        });
      },
    },
  );
  const call = (request: any) =>
    new Promise<any>((resolve) =>
      listener(request, { id: 'fixture', url: 'https://app.example/' }, resolve),
    );
  const connect = (id: string) =>
    call({
      type: 'CONNECT',
      connection: { apiBase: 'https://app.example', auth: `Bearer ${id}`, accountId: id },
    });
  assert.equal((await connect('a')).connected, true);
  assert.deepEqual(accessLevels, ['TRUSTED_CONTEXTS']);
  assert.equal(local.state.profile, undefined);
  assert.equal(local.state.ledger, undefined);
  assert.equal(local.state.memoryReceipts, undefined);
  assert.equal(local.state.tasks[0].notes, undefined);
  assert.equal(local.state.tasks[0].calendarEventId, undefined);
  assert.ok(sent.filter((m) => m.state).every((m) => !('profile' in m.state)));
  local.notifiedPlans = { old: 'start' };
  delay = true;
  const pending = call({ type: 'STATE' });
  await started.promise;
  assert.equal((await connect('b')).state.balanceSeconds, 7200);
  held.resolve(json({ tasks: [], owner: 'Bearer a', balanceSeconds: 9999 }));
  assert.match((await pending).error, /アカウントが切り替わりました/);
  assert.equal(local.state.balanceSeconds, 7200);
  assert.equal(session.connection.accountId, 'b');
  assert.equal(sent.filter((m) => m.state).at(-1).state.balanceSeconds, 7200);
  assert.ok(cleared.includes('task:old'));
  assert.ok(!cleared.includes('unrelated'));
  assert.equal(local.notifiedPlans, undefined);
  local.viewingPending = {
    accountId: 'a',
    id: 'foreign-receipt',
    seconds: 10,
    sessionId: 'a-session',
  };
  await call({ type: 'STATE' });
  assert.equal(local.viewingPending, undefined);
  assert.equal(local.state.balanceSeconds, 7200);
  local.notifiedPlans = { current: 'start' };
  await connect('b');
  assert.deepEqual(local.notifiedPlans, { current: 'start' });
  await call({ type: 'DISCONNECT' });
  assert.equal(local.state, undefined);
  assert.equal(session.connection, undefined);
  assert.equal(sent.at(-1).state, undefined);
});
