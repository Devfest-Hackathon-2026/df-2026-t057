// 拡張が保持する状態（ExtensionState）。background だけが書き換える。
// chrome.storage.local に保存し（content script からは読めない設定にする）、変わるたびに購読者へ知らせる。
import type { Resource } from '@dopagaki/contract';
import { emptyState, extensionStateSchema, type ExtensionState } from '../lib/schemas';
import { connectionVersion } from './connection';

const KEY = 'state';
let state: ExtensionState | undefined;
let loaded = false;
const revisions: Partial<Record<Resource, number>> = {};
const listeners = new Set<(state: ExtensionState | undefined) => void>();

export async function getState(): Promise<ExtensionState | undefined> {
  if (!loaded) {
    const parsed = extensionStateSchema.safeParse((await chrome.storage.local.get(KEY))[KEY]);
    state = parsed.success ? parsed.data : undefined;
    loaded = true;
  }
  return state;
}

/** 変更通知で知った、リソースごとの版。 */
export function knownRevisions(): Readonly<Partial<Record<Resource, number>>> {
  return revisions;
}

export function rememberRevision(resource: Resource, revision: number): void {
  revisions[resource] = Math.max(revisions[resource] ?? -1, revision);
}

export function onStateChange(listener: (state: ExtensionState | undefined) => void): void {
  listeners.add(listener);
}

/**
 * 状態の一部を差し替える。`version` は取得を始めたときの接続の版で、変わっていたら捨てる。
 * 版を持つリソースは、手元より古い結果で上書きしない（遅れて届いたレスポンス対策）。
 */
export async function patchState(version: number, patch: Partial<ExtensionState>): Promise<void> {
  if (version !== connectionVersion()) return;
  const current = (await getState()) ?? emptyState;
  if (version !== connectionVersion()) return;
  const next: ExtensionState = { ...current, ...patch };
  if (patch.tasks && current.planRevision > (patch.planRevision ?? 0)) {
    next.tasks = current.tasks;
    next.planRevision = current.planRevision;
    next.nextSnooze = current.nextSnooze;
  }
  if (patch.wallet && current.wallet && current.wallet.revision > patch.wallet.revision) {
    next.wallet = current.wallet;
  }
  if (patch.settings && current.settings && current.settings.revision > patch.settings.revision) {
    next.settings = current.settings;
  }
  if (patch.profile && current.profile && current.profile.revision > patch.profile.revision) {
    next.profile = current.profile;
  }
  state = next;
  loaded = true;
  await chrome.storage.local.set({ [KEY]: next });
  for (const listener of listeners) listener(next);
}

/** アカウントの切り替え・接続の解除。 */
export async function clearState(): Promise<void> {
  state = undefined;
  loaded = true;
  for (const resource of Object.keys(revisions) as Resource[])
    Reflect.deleteProperty(revisions, resource);
  await chrome.storage.local.remove(KEY);
  for (const listener of listeners) listener(undefined);
}
