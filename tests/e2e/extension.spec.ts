import { test, expect, chromium } from '@playwright/test';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { resolve, join } from 'node:path';

test('unpacked extension connects, counts foreground Shorts and presents a due task', async () => {
  test.setTimeout(60000);
  const profile = await mkdtemp(join(tmpdir(), 'dopagaki-chrome-'));
  const extension = resolve('dist/extension');
  const context = await chromium.launchPersistentContext(profile, {
    channel: 'chromium',
    headless: true,
    args: [`--disable-extensions-except=${extension}`, `--load-extension=${extension}`],
    viewport: { width: 1360, height: 900 },
  });
  try {
    const worker = context.serviceWorkers()[0] ?? (await context.waitForEvent('serviceworker'));
    const app = await context.newPage();
    await app.goto('http://127.0.0.1:4200');
    await expect
      .poll(() =>
        worker.evaluate(
          async () => !!(await chrome.storage.session.get('connection'))['connection'],
        ),
      )
      .toBe(true);
    const youtube = await context.newPage();
    await youtube.route('https://www.youtube.com/**', (route) =>
      route.fulfill({
        contentType: 'text/html',
        body: '<html><body style="background:#151515;color:white"><h1>Shorts DOM fixture</h1><canvas id="canvas" width="240" height="400"></canvas><video muted autoplay style="height:70vh"></video><script>const c=document.querySelector("canvas");const ctx=c.getContext("2d");setInterval(()=>{ctx.fillStyle="hsl("+(Date.now()/30%360)+",45%,35%)";ctx.fillRect(0,0,240,400)},100);const v=document.querySelector("video");v.srcObject=c.captureStream(10);v.play();</script></body></html>',
      }),
    );
    await youtube.goto('https://www.youtube.com/shorts/dopagaki-fixture');
    await youtube.bringToFront();
    await expect
      .poll(
        () =>
          worker.evaluate(
            async () =>
              ((await chrome.storage.local.get('state'))['state'] as { balanceSeconds: number })
                ?.balanceSeconds,
          ),
        { timeout: 20000 },
      )
      .toBeLessThan(3600);
    await app.evaluate(async () => {
      await fetch('/api/tasks', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          id: 'extension-test',
          title: '拡張の動作を確認する',
          notes: '',
          minutes: 30,
          deadline: new Date(Date.now() + 30 * 60000).toISOString(),
          url: '',
        }),
      });
      const { token } = await (await fetch('/api/extension/token', { method: 'POST' })).json();
      window.postMessage(
        { type: 'DOPAGAKI_CONNECT', apiBase: location.origin, auth: token, accountId: token },
        location.origin,
      );
    });
    const host = youtube.locator('#dopagaki-extension');
    await expect(host.getByRole('button', { name: '作業をはじめる', exact: true })).toBeVisible({
      timeout: 15000,
    });
    await youtube.screenshot({
      animations: 'disabled',
      path: 'test-results/extension-notification.png',
    });
    await host.getByRole('button', { name: '10分スヌーズ・予定を確認', exact: true }).click();
    await expect(host.getByRole('heading', { name: 'スヌーズ後の予定を確認' })).toBeVisible();
    await expect(host.getByRole('button', { name: 'OK・この予定に変更' })).toBeVisible();
    await host.getByRole('button', { name: 'キャンセル', exact: true }).click();
    await host.getByRole('button', { name: '作業をはじめる', exact: true }).click();
    await expect(host.getByRole('region', { name: 'DOPAGAKI 作業タイマー' })).toBeVisible();
    await host.getByRole('button', { name: '一時停止', exact: true }).click();
    await expect(host.getByRole('button', { name: '再開', exact: true })).toBeVisible();
    await host.getByRole('button', { name: '完了 ＋20分', exact: true }).click();
    await expect(host.getByText('課題完了！視聴時間 ＋20分')).toBeVisible();
    await expect(host.locator('.timer')).not.toBeVisible();
    await worker.evaluate(async () => {
      const { state } = await chrome.storage.local.get('state');
      const [tab] = await chrome.tabs.query({ url: 'https://www.youtube.com/shorts/*' });
      await chrome.tabs.sendMessage(tab.id!, {
        type: 'STATE_UPDATED',
        state: { ...(state as object), balanceSeconds: 0 },
      });
    });
    await youtube.locator('video').evaluate((video: HTMLVideoElement) => video.play());
    await expect
      .poll(() => youtube.locator('video').evaluate((video: HTMLVideoElement) => video.paused))
      .toBe(true);
    await expect(
      host.getByText('視聴時間がなくなりました。課題を登録・完了して時間を増やしましょう。'),
    ).toBeVisible();
  } finally {
    await context.close();
    await rm(profile, { recursive: true, force: true });
  }
});
