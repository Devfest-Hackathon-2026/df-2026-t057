// 変更通知（SSE）を受けたときに、どのリソースを取り直すか。
import type { Resource } from '@dopagaki/contract';

/** 拡張が表示に使うリソース。ほか（相談・カレンダーなど）の通知は無視する。 */
export const watchedResources = [
  'tasks',
  'workSession',
  'wallet',
  'viewing',
  'settings',
  'sleep',
] as const satisfies readonly Resource[];
export type WatchedResource = (typeof watchedResources)[number];

export function isWatched(resource: Resource): resource is WatchedResource {
  return (watchedResources as readonly Resource[]).includes(resource);
}

/**
 * 取り直すリソース。知っている版より新しいときだけ取り直す。
 * 視聴できる秒数は残高から決まるので、wallet が変われば viewing も取り直す。
 */
export function resourcesToRefetch(
  event: { resource: Resource; revision: number },
  known: Partial<Record<Resource, number>>,
): WatchedResource[] {
  if (!isWatched(event.resource)) return [];
  const current = known[event.resource];
  if (current !== undefined && event.revision <= current) return [];
  if (event.resource === 'wallet') return ['wallet', 'viewing'];
  return [event.resource];
}
