// 管理画面から受け取った接続（Authorization とアカウント）。chrome.storage.session に置き、ディスクに残さない。
//
// - 同じアカウントで Authorization だけ変わった（Firebase の ID トークンの更新）ときは、値を差し替えるだけで状態は消さない。
// - アカウントが変わった・接続を解除したときは、版（version）を上げて、前のアカウントのデータをすべて消す。
//   版を上げると、通信中の結果は捨てられる（前のアカウントの結果を新しいアカウントの表示に混ぜない）。
import { z } from 'zod';

const connectionSchema = z.object({
  authorization: z.string().min(1).max(4096),
  accountId: z.string().min(1).max(128),
});
export type Connection = z.infer<typeof connectionSchema>;

const KEY = 'connection';
/**
 * 最後に接続したアカウント（chrome.storage.local）。Authorization はブラウザーを閉じると消えるが、
 * 同じアカウントでつなぎ直したときに、保存した状態や送れていない視聴の報告を消さないために覚えておく。
 */
const ACCOUNT_KEY = 'accountId';
let cached: Connection | undefined;
let loaded = false;
let version = 0;
let queue: Promise<unknown> = Promise.resolve();

export function connectionVersion(): number {
  return version;
}

export async function getConnection(): Promise<Connection | undefined> {
  await queue;
  if (!loaded) {
    const stored: unknown = (await chrome.storage.session.get(KEY))[KEY];
    const parsed = connectionSchema.safeParse(stored);
    cached = parsed.success ? parsed.data : undefined;
    loaded = true;
  }
  return cached;
}

export type ConnectResult = 'unchanged' | 'refreshed' | 'switched';

/**
 * 接続を差し替える。`onSwitch` はアカウントが変わったとき（または解除したとき）に、
 * 前のアカウントのデータを消すために呼ぶ。差し替えは1つずつ順に行う。
 */
export function replaceConnection(
  next: Connection | undefined,
  onSwitch: () => Promise<void>,
): Promise<ConnectResult> {
  const change = queue.then(async (): Promise<ConnectResult> => {
    loaded = false;
    const previous = await loadUnqueued();
    const storedAccount: unknown = (await chrome.storage.local.get(ACCOUNT_KEY))[ACCOUNT_KEY];
    const previousAccount = previous?.accountId ?? storedAccount;
    if (next && previousAccount === next.accountId) {
      if (previous?.authorization === next.authorization) return 'unchanged';
      cached = next;
      await chrome.storage.session.set({ [KEY]: next });
      return 'refreshed';
    }
    version += 1;
    cached = undefined;
    await chrome.storage.session.remove(KEY);
    await chrome.storage.local.remove(ACCOUNT_KEY);
    await onSwitch();
    if (next) {
      cached = next;
      await chrome.storage.session.set({ [KEY]: next });
      await chrome.storage.local.set({ [ACCOUNT_KEY]: next.accountId });
    }
    return 'switched';
  });
  queue = change.catch(() => undefined);
  return change;
}

async function loadUnqueued(): Promise<Connection | undefined> {
  const stored: unknown = (await chrome.storage.session.get(KEY))[KEY];
  const parsed = connectionSchema.safeParse(stored);
  cached = parsed.success ? parsed.data : undefined;
  loaded = true;
  return cached;
}
