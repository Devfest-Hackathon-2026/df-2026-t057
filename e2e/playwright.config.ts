import { defineConfig, devices } from '@playwright/test';

const port = Number(process.env['E2E_PORT'] ?? 4300);
const baseURL = `http://127.0.0.1:${port}`;

// API をデモモードで起動し、ビルドした管理画面も配信させる（本番と同じ同一オリジン構成）。
export default defineConfig({
  testDir: './tests',
  globalSetup: './support/global-setup.ts',
  fullyParallel: true,
  forbidOnly: !!process.env['CI'],
  retries: process.env['CI'] ? 1 : 0,
  // テストごとに別のデモの利用者を使うので、並べて実行できる。CI の端末は2コアで、既定（コア数の半分）では
  // 1つずつしか動かない。待ち時間が長いテストが多いので、3つ並べる。
  workers: process.env['CI'] ? 3 : undefined,
  timeout: 30_000,
  reporter: process.env['CI'] ? [['github'], ['list']] : 'list',
  use: {
    baseURL,
    locale: 'ja-JP',
    timezoneId: 'Asia/Tokyo',
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
  // devices の既定の画面の大きさ（1280×720）ではなく、旧テストと同じ 1440×1080 で開く。
  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'], viewport: { width: 1440, height: 1080 } },
    },
  ],
  ...(process.env['E2E_EXTERNAL_SERVER'] === '1'
    ? {}
    : {
        webServer: {
          command:
            process.env['E2E_USE_BUILD'] === '1' ? '../../dist/api/server' : 'go run ./cmd/server',
          cwd: '../apps/api',
          env: {
            APP_MODE: 'demo',
            APP_URL: baseURL,
            PORT: String(port),
            STATIC_DIR: '../web/dist/web/browser',
          },
          url: `${baseURL}/api/health`,
          reuseExistingServer: !process.env['CI'],
          timeout: 120_000,
        },
      }),
});
