import { execFileSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import type { FullConfig } from '@playwright/test';

/**
 * 拡張を e2e のサーバーのオリジン（APP_URL）でビルドする。
 * 拡張は管理画面のオリジンをビルド時に埋め込むので、e2e のポートに合わせて毎回ビルドし直す。
 * `E2E_SKIP_EXTENSION_BUILD=1` で省ける（ビルド済みのものを使う）。
 */
export default function globalSetup(config: FullConfig): void {
  if (process.env['E2E_SKIP_EXTENSION_BUILD']) return;
  const baseURL = config.projects[0]?.use.baseURL;
  if (!baseURL) return;
  execFileSync('pnpm', ['--filter', '@dopagaki/extension', 'build'], {
    cwd: fileURLToPath(new URL('../..', import.meta.url)),
    env: { ...process.env, APP_URL: baseURL },
    stdio: 'inherit',
  });
}
