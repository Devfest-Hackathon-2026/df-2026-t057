// 外から来た URL を開く前の確認と、作業サイトのタイマーの登録 ID。

/** http(s) の URL だけを返す（`javascript:` などは開かない）。 */
export function safeHttpUrl(value: string): string | undefined {
  try {
    const url = new URL(value);
    return url.protocol === 'https:' || url.protocol === 'http:' ? url.href : undefined;
  } catch {
    return undefined;
  }
}

/** 作業サイトとして許可を求める対象（`https://example.com/*`）。http(s) 以外のページでは undefined。 */
export function workSitePattern(pageUrl: string | undefined): string | undefined {
  if (!pageUrl) return undefined;
  const url = safeHttpUrl(pageUrl);
  return url ? `${new URL(url).origin}/*` : undefined;
}

/** 登録する content script の ID。同じサイトを二重に登録しない。 */
export function workSiteScriptId(pattern: string): string {
  return `work-${pattern.replace(/\W/g, '_')}`;
}

/** 管理画面の URL。`query` は通知から開くときの `?notification=<id>` など。 */
export function appUrl(origin: string, query?: Readonly<Record<string, string>>): string {
  const url = new URL('/', origin);
  for (const [key, value] of Object.entries(query ?? {})) url.searchParams.set(key, value);
  return url.href;
}
