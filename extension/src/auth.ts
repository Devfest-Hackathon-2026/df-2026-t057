import { initializeApp } from 'firebase/app';
import {
  Auth,
  GoogleAuthProvider,
  indexedDBLocalPersistence,
  initializeAuth,
  signInWithCredential,
  signOut,
} from 'firebase/auth/web-extension';
import { MANAGEMENT_ORIGIN } from './protocol';

let auth: Auth | undefined;
let live = false;
let pending: { resolve: () => void; reject: (error: Error) => void } | undefined;
let initialized = false;
let initialization: Promise<void> | undefined;
export function ensureAuth(): Promise<void> {
  if (initialized) return Promise.resolve();
  if (!initialization)
    initialization = (async () => {
      const response = await fetch(`${MANAGEMENT_ORIGIN}/api/config`, { headers: {} });
      if (!response.ok)
        throw new Error('接続設定を取得できませんでした。通信が戻ると再試行します。');
      const config = await response.json();
      if (config.mode === 'demo') return;
      if (config.mode !== 'live') throw new Error('接続設定の内容を確認できませんでした。');
      live = true;
      if (!config.firebase?.apiKey) throw new Error('Googleログインが設定されていません。');
      if (!auth)
        auth = initializeAuth(initializeApp(config.firebase, 'extension'), {
          persistence: indexedDBLocalPersistence,
        });
      await auth.authStateReady();
    })()
      .then(() => {
        initialized = true;
      })
      .catch((error) => {
        initialization = undefined;
        throw error;
      });
  return initialization;
}
export async function extensionConnection() {
  await ensureAuth();
  if (!live) return undefined;
  const user = auth?.currentUser;
  return user
    ? { apiBase: MANAGEMENT_ORIGIN, accountId: user.uid, auth: `Bearer ${await user.getIdToken()}` }
    : null;
}
export async function signIn() {
  await ensureAuth();
  if (!auth) throw new Error('この環境では拡張のGoogleログインを利用しません。');
  if (pending) throw new Error('ログインを確認しています。');
  await signOut(auth);
  const result = new Promise<void>((resolve, reject) => {
    pending = { resolve, reject };
  });
  const timeout = setTimeout(
    () => pending?.reject(new Error('ログインを確認できませんでした。もう一度接続してください。')),
    60000,
  );
  try {
    if (!(await chrome.offscreen.hasDocument()))
      await chrome.offscreen.createDocument({
        url: 'offscreen.html',
        reasons: [chrome.offscreen.Reason.IFRAME_SCRIPTING],
        justification: 'Googleアカウントで拡張へ接続するため',
      });
    await chrome.runtime.sendMessage({ target: 'offscreen', type: 'AUTH_OPEN' });
    await result;
  } finally {
    clearTimeout(timeout);
    pending = undefined;
    if (await chrome.offscreen.hasDocument()) await chrome.offscreen.closeDocument();
  }
}
export async function receiveCredential(value: unknown) {
  if (!pending || !auth) return;
  if (!value || typeof value !== 'object') {
    pending.reject(new Error('ログインを完了できませんでした。'));
    return;
  }
  const data = value as { idToken?: unknown; accessToken?: unknown; error?: unknown };
  if (typeof data.error === 'string') {
    pending.reject(new Error('ログインをキャンセルしたか、接続できませんでした。'));
    return;
  }
  if (typeof data.idToken !== 'string' || typeof data.accessToken !== 'string') {
    pending.reject(new Error('認証結果を確認できませんでした。'));
    return;
  }
  try {
    await signInWithCredential(auth, GoogleAuthProvider.credential(data.idToken, data.accessToken));
    pending.resolve();
  } catch {
    pending.reject(new Error('Googleアカウントへの接続に失敗しました。'));
  }
}
export async function disconnectAuth() {
  await ensureAuth();
  if (auth) await signOut(auth);
}
export async function authStatus() {
  await ensureAuth();
  return { accountId: auth?.currentUser?.uid, email: auth?.currentUser?.email ?? '', live };
}
