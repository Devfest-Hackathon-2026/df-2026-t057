import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import {
  test as base,
  chromium,
  expect,
  type BrowserContext,
  type Locator,
  type Page,
  type Worker,
} from '@playwright/test';
import { Api } from './api';

/** ビルドした拡張（global-setup.ts が e2e のサーバーのオリジンでビルドする）。 */
export const extensionPath = fileURLToPath(
  new URL('../../apps/extension/.output/chrome-mv3', import.meta.url),
);

/**
 * Shorts の代わりのページ。canvas の映像を video に流し、本物の動画と同じく再生中（readyState ≥ 2）になる。
 * `autoplay` を付けると開いた直後から再生する（視聴の区切りを選ぶまで拡張が止める）。
 */
export function shortsPage(options: { autoplay: boolean }): string {
  const script = options.autoplay
    ? `<script>
const canvas = document.querySelector('canvas');
const context = canvas.getContext('2d');
setInterval(() => {
  context.fillStyle = 'hsl(' + ((Date.now() / 30) % 360) + ', 45%, 35%)';
  context.fillRect(0, 0, 240, 400);
}, 100);
const video = document.querySelector('video');
video.srcObject = canvas.captureStream(10);
video.play();
</script>`
    : '';
  return `<!doctype html><html lang="ja"><head><meta charset="utf-8"><title>Shorts</title></head>
<body style="background:#151515;color:white"><h1>Shorts DOM fixture</h1>
<canvas width="240" height="400"></canvas>
<video muted ${options.autoplay ? 'autoplay' : ''} style="height:70vh"></video>${script}</body></html>`;
}

interface ExtensionFixtures {
  readonly context: BrowserContext;
  /** 拡張の service worker。 */
  readonly worker: Worker;
  /** 管理画面と同じデモの利用者として API を呼ぶ。 */
  readonly api: Api;
  /** 管理画面（開くと拡張へ接続する）。 */
  readonly app: Page;
}

/**
 * 拡張を読み込んだ Chromium（永続コンテキスト。headless でも拡張が動く）。
 * テストごとに新しいプロファイルと、新しいデモの利用者を使う。
 */
export const test = base.extend<ExtensionFixtures>({
  // eslint-disable-next-line no-empty-pattern -- 永続コンテキストは既定のブラウザーを使わない。
  context: async ({}, use, testInfo) => {
    const profile = await mkdtemp(join(tmpdir(), 'dopagaki-chrome-'));
    const baseURL = testInfo.project.use.baseURL;
    const context = await chromium.launchPersistentContext(profile, {
      channel: 'chromium',
      headless: true,
      args: [`--disable-extensions-except=${extensionPath}`, `--load-extension=${extensionPath}`],
      viewport: { width: 1360, height: 900 },
      locale: 'ja-JP',
      timezoneId: 'Asia/Tokyo',
      ...(baseURL ? { baseURL } : {}),
    });
    try {
      await use(context);
    } finally {
      await context.close();
      await rm(profile, { recursive: true, force: true });
    }
  },
  worker: async ({ context }, use) => {
    const worker = context.serviceWorkers()[0] ?? (await context.waitForEvent('serviceworker'));
    await use(worker);
  },
  api: async ({ context }, use) => {
    const api = new Api(context.request);
    await api.signIn();
    await use(api);
  },
  app: async ({ context, worker, api }, use) => {
    // 拡張が読み込まれ、Cookie が決まってから管理画面を開く（自動で接続する）。
    expect(worker.url()).toMatch(/^chrome-extension:\/\//);
    expect(api).toBeDefined();
    const app = await context.newPage();
    await app.goto('/calendar');
    await expect(app.getByRole('navigation', { name: 'メインメニュー' })).toBeVisible();
    await use(app);
  },
});

export { expect };

/** YouTube への通信を、手元の Shorts のページに差し替える。 */
export async function routeYouTube(
  context: BrowserContext,
  options: { autoplay: boolean },
): Promise<void> {
  await context.route('https://www.youtube.com/**', (route) =>
    route.fulfill({ contentType: 'text/html', body: shortsPage(options) }),
  );
}

/** Shorts のページを開いて前面にし、拡張の画面（Shadow DOM の中）を返す。 */
export async function openShorts(
  context: BrowserContext,
  id: string,
): Promise<{ page: Page; overlay: Locator }> {
  const page = await context.newPage();
  await page.goto(`https://www.youtube.com/shorts/${id}`);
  await page.bringToFront();
  return { page, overlay: page.locator('#dopagaki-extension') };
}

export function videoPaused(page: Page): Promise<boolean> {
  return page.locator('video').evaluate((video: HTMLVideoElement) => video.paused);
}

/** Asia/Tokyo（デモの利用者のタイムゾーン）での「HH:MM」。 */
export function tokyoClock(offsetMs: number): string {
  return new Date(Date.now() + offsetMs).toLocaleTimeString('en-GB', {
    timeZone: 'Asia/Tokyo',
    hour: '2-digit',
    minute: '2-digit',
    hourCycle: 'h23',
  });
}
