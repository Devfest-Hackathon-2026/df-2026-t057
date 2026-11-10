// 権限の初回許可で popup が閉じても、タイマーの登録を background で完了する。
import { YOUTUBE_MATCH } from '../buildconfig/manifest';
import { MANAGEMENT_ORIGIN } from '../lib/config';
import { workSitePattern, workSiteScriptId } from '../lib/urls';
import { ExtensionError } from './errors';

const FILE = 'content-scripts/worksite.js';
const registrations = new Map<string, Promise<void>>();

function covered(pattern: string): boolean {
  return pattern === YOUTUBE_MATCH || pattern === `${MANAGEMENT_ORIGIN}/*`;
}

async function register(pattern: string): Promise<void> {
  if (!(await chrome.permissions.contains({ origins: [pattern] }))) {
    throw new ExtensionError('untrustedSender');
  }
  let pending = registrations.get(pattern);
  if (!pending) {
    pending = (async () => {
      const id = workSiteScriptId(pattern);
      const scripts = await chrome.scripting.getRegisteredContentScripts({ ids: [id] });
      if (scripts.length === 0) {
        await chrome.scripting.registerContentScripts([
          {
            id,
            matches: [pattern],
            js: [FILE],
            runAt: 'document_idle',
            persistAcrossSessions: true,
          },
        ]);
      }
    })().finally(() => registrations.delete(pattern));
    registrations.set(pattern, pending);
  }
  await pending;
}

export async function enableWorkSite(tabId: number): Promise<void> {
  const tab = await chrome.tabs.get(tabId);
  const pattern = workSitePattern(tab.url);
  if (!pattern) throw new ExtensionError('untrustedSender');
  if (covered(pattern)) return;
  await register(pattern);
  await chrome.scripting.executeScript({ target: { tabId }, files: [FILE] });
}

export function listenWorkSites(): void {
  chrome.permissions.onAdded.addListener((permissions) => {
    for (const pattern of permissions.origins ?? []) {
      // サイトごとの許可だけを扱い、包括的な権限付与で全ページへ注入しない。
      if (covered(pattern) || pattern.includes('*://') || pattern.slice(0, -2).includes('*'))
        continue;
      if (workSitePattern(pattern) !== pattern) continue;
      void register(pattern)
        .then(async () => {
          const tabs = await chrome.tabs.query({ url: pattern });
          await Promise.all(
            tabs.flatMap((tab) =>
              tab.id === undefined
                ? []
                : [chrome.scripting.executeScript({ target: { tabId: tab.id }, files: [FILE] })],
            ),
          );
        })
        .catch((error: unknown) => {
          console.warn('DOPAGAKI: failed to enable a work site', error);
        });
    }
  });
}
