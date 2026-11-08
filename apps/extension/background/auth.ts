import { z } from 'zod';
import { initializeApp } from 'firebase/app';
import {
  type Auth,
  GoogleAuthProvider,
  indexedDBLocalPersistence,
  initializeAuth,
  signInWithCredential,
  signOut,
} from 'firebase/auth/web-extension';
import { MANAGEMENT_ORIGIN } from '../lib/config';
import { ExtensionError } from './errors';

let auth: Auth | undefined;
let live = false;
let pending: { resolve: () => void; reject: (error: Error) => void } | undefined;
let initialized = false;
let initialization: Promise<void> | undefined;
export function ensureAuth(): Promise<void> {
  if (initialized) return Promise.resolve();
  initialization ??= (async () => {
    const response = await fetch(`${MANAGEMENT_ORIGIN}/api/config`, { headers: {} });
    if (!response.ok) throw new ExtensionError('authConfigUnavailable');
    const config = z
      .object({
        mode: z.enum(['demo', 'live']),
        firebase: z.record(z.string(), z.string()).optional(),
      })
      .parse(await response.json());
    if (config.mode === 'demo') return;
    live = true;
    if (!config.firebase?.['apiKey']) throw new ExtensionError('authNotConfigured');
    auth ??= initializeAuth(initializeApp(config.firebase, 'extension'), {
      persistence: indexedDBLocalPersistence,
    });
    await auth.authStateReady();
  })()
    .then(() => {
      initialized = true;
    })
    .catch((error: unknown) => {
      initialization = undefined;
      throw error;
    });
  return initialization;
}
export async function extensionConnection() {
  await ensureAuth();
  if (!live) return undefined;
  const user = auth?.currentUser;
  return user ? { accountId: user.uid, authorization: `Bearer ${await user.getIdToken()}` } : null;
}
export async function signIn() {
  await ensureAuth();
  if (!auth) throw new ExtensionError('authUnavailable');
  if (pending) throw new ExtensionError('authInProgress');
  await signOut(auth);
  const result = new Promise<void>((resolve, reject) => {
    pending = { resolve, reject };
  });
  const timeout = setTimeout(() => pending?.reject(new ExtensionError('authTimedOut')), 60000);
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
  const result = z
    .object({
      idToken: z.string().optional(),
      accessToken: z.string().optional(),
      error: z.string().optional(),
    })
    .safeParse(value);
  if (!result.success || result.data.error || !result.data.idToken || !result.data.accessToken) {
    pending.reject(new ExtensionError('authFailed'));
    return;
  }
  const data = result.data;
  try {
    await signInWithCredential(
      auth,
      GoogleAuthProvider.credential(data.idToken ?? null, data.accessToken ?? null),
    );
    pending.resolve();
  } catch {
    pending.reject(new ExtensionError('authFailed'));
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
