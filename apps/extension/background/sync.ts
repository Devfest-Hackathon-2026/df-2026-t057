// リソースを API から取り直して、状態に反映する。
import { watchedResources, type WatchedResource } from '../lib/resources';
import type { ExtensionState } from '../lib/schemas';
import { api } from './api';
import { connectionVersion } from './connection';
import { patchState, rememberRevision } from './store';

type Fetcher = (version: number) => Promise<void>;

/** 完了した課題は表示に使わないので持たない。 */
export function tasksPatch(list: Awaited<ReturnType<typeof api.tasks>>): Partial<ExtensionState> {
  return {
    tasks: list.items.filter((task) => task.status !== 'completed'),
    planRevision: list.revision,
    nextSnooze: list.nextSnooze,
  };
}

const fetchers: Record<WatchedResource, Fetcher> = {
  tasks: async (version) => {
    const list = await api.tasks();
    rememberRevision('tasks', list.revision);
    await patchState(version, tasksPatch(list));
  },
  workSession: async (version) => {
    const workSession = await api.workSession();
    await patchState(version, { workSession, workSyncedAt: Date.now() });
  },
  wallet: async (version) => {
    const wallet = await api.wallet();
    rememberRevision('wallet', wallet.revision);
    await patchState(version, { wallet });
  },
  viewing: async (version) => {
    await patchState(version, { viewing: await api.viewing() });
  },
  settings: async (version) => {
    const settings = await api.settings();
    rememberRevision('settings', settings.revision);
    await patchState(version, { settings });
  },
  sleep: async (version) => {
    const sleep = await api.sleep();
    rememberRevision('sleep', sleep.revision);
    await patchState(version, { sleep: sleep.sleep });
  },
  profile: async (version) => {
    const profile = await api.profile();
    rememberRevision('profile', profile.revision);
    await patchState(version, { profile });
  },
};

/** 指定したリソースを取り直す。取れたものは反映し、1つでも失敗したら最初の失敗を投げる。 */
export async function refetch(resources: readonly WatchedResource[]): Promise<void> {
  const version = connectionVersion();
  const results = await Promise.allSettled(
    [...new Set(resources)].map((r) => fetchers[r](version)),
  );
  const failure = results.find((result) => result.status === 'rejected');
  if (failure) throw failure.reason;
}

export function refetchAll(): Promise<void> {
  return refetch(watchedResources);
}
