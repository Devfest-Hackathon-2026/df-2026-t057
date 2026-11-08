// ビルド時の設定（管理画面のオリジンと拡張のバージョン）を環境変数から決める。
// wxt.config.ts から読み込む。値が不正ならビルドを止める（配布物に誤った設定を入れない）。

export const DEFAULT_APP_URL = 'http://127.0.0.1:4200';

/** `APP_URL` からオリジン（`https://example.com`）を取り出す。パスなどは捨てる。 */
export function resolveManagementOrigin(appUrl: string | undefined): string {
  const value = appUrl?.trim() ? appUrl.trim() : DEFAULT_APP_URL;
  let url: URL;
  try {
    url = new URL(value);
  } catch {
    throw new Error(`APP_URL must be an absolute URL: ${value}`);
  }
  if (url.protocol !== 'https:' && url.protocol !== 'http:') {
    throw new Error(`APP_URL must use http or https: ${value}`);
  }
  return url.origin;
}

const VERSION = /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/;

/**
 * Chrome の manifest の version に使える `X.Y.Z` を返す。
 * `RELEASE_TAG`（先頭の `v` は外す）があればそれを、無ければ package.json の version を使う。
 */
export function resolveVersion(releaseTag: string | undefined, packageVersion: string): string {
  const version = releaseTag ? releaseTag.replace(/^v/, '') : packageVersion;
  if (
    !VERSION.test(version) ||
    version.split('.').some((part) => Number(part) > 65535) ||
    version === '0.0.0'
  ) {
    throw new Error(
      `Extension version must be X.Y.Z, with each number between 0 and 65535 (not all zero): ${version}`,
    );
  }
  return version;
}
