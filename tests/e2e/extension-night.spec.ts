import { test, expect, chromium } from '@playwright/test';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { resolve, join } from 'node:path';

test('watching Shorts after bedtime shows one quiet bedtime reminder per night', async ({
  baseURL,
}) => {
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
    await app.goto(baseURL!);
    await expect
      .poll(() =>
        worker.evaluate(
          async () => !!(await chrome.storage.session.get('connection'))['connection'],
        ),
      )
      .toBe(true);
    // A bedtime 30 minutes from now puts the browser inside the night window (it starts an hour earlier).
    await app.evaluate(async () => {
      const clock = (offset: number) => new Date(Date.now() + offset).toTimeString().slice(0, 5);
      await fetch('/api/sleep', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ bedtime: clock(30 * 60000), wake: clock(8.5 * 3600000) }),
      });
    });
    const youtube = await context.newPage();
    await youtube.route('https://www.youtube.com/**', (route) =>
      route.fulfill({
        contentType: 'text/html',
        body: '<html><body><h1>Shorts DOM fixture</h1><video muted style="height:70vh"></video></body></html>',
      }),
    );
    await youtube.goto('https://www.youtube.com/shorts/dopagaki-night');
    await youtube.bringToFront();
    const host = youtube.locator('#dopagaki-extension');
    await expect(host.getByRole('heading', { name: 'そろそろ、寝る準備。' })).toBeVisible({
      timeout: 15000,
    });
    await expect(host.locator('.companion')).toContainText('寝る準備');
    await expect(host.getByText('このまま寝ると、朝に視聴時間 ＋15分')).toBeVisible();
    await host.getByRole('button', { name: 'あと少しだけ', exact: true }).click();
    await expect(host.getByRole('heading', { name: 'そろそろ、寝る準備。' })).not.toBeVisible();
    await youtube.reload();
    await youtube.waitForTimeout(2000);
    await expect(host.getByRole('heading', { name: 'そろそろ、寝る準備。' })).not.toBeVisible();
  } finally {
    await context.close();
    await rm(profile, { recursive: true, force: true });
  }
});
