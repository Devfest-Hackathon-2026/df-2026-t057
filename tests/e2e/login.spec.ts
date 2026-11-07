import { test, expect } from '@playwright/test';

const guestConfig = {
  mode: 'live',
  projectId: 'signed-out-test',
  aiEnabled: false,
  calendarEnabled: false,
};

test('signed-out preview never loads or changes personal data and demo still opens the workspace', async ({
  page,
}) => {
  const apiRequests: string[] = [];
  page.on('request', (request) => {
    if (new URL(request.url()).pathname.startsWith('/api/')) apiRequests.push(request.url());
  });
  await page.route('**/api/config', (route) => route.fulfill({ json: guestConfig }));
  await page.goto('/calendar?date=2026-11-07');
  await expect(page.getByRole('button', { name: 'Googleでログイン', exact: true })).toBeEnabled();
  await expect(page.getByRole('navigation', { name: 'メインメニュー' })).not.toBeVisible();
  await expect(page.locator('.topbar')).not.toBeVisible();
  await page.getByRole('button', { name: '課題完了のサンプルを試す' }).click();
  await expect(page.locator('.sample-reward')).toHaveText('＋5分');
  await expect(page.getByRole('status').filter({ hasText: 'サンプル' })).toContainText('5分');
  await page.getByRole('button', { name: '課題完了のサンプルをもう一度見る' }).click();
  await expect(page.locator('.sample-reward')).toHaveText('作業で ＋5分');
  expect(apiRequests.map((url) => new URL(url).pathname)).toEqual(['/api/config']);
  await expect(page.getByRole('link', { name: 'プライバシーポリシー' })).toHaveAttribute(
    'href',
    '/privacy.html',
  );

  await page.screenshot({ path: 'test-results/login-desktop.png', fullPage: true });
  await page.setViewportSize({ width: 320, height: 844 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await expect(page.getByRole('button', { name: 'Googleでログイン', exact: true })).toBeVisible();
  await page.screenshot({ path: 'test-results/login-mobile.png', fullPage: true });

  await page.unroute('**/api/config');
  await page.reload();
  await expect(page.getByRole('navigation', { name: 'メインメニュー' })).toBeVisible();
  await expect(page.locator('app-login-screen')).not.toBeVisible();
  await expect(page).toHaveURL(/calendar\?date=2026-11-07$/);
});

test('login is disabled during initialization and reports an authentication setup failure', async ({
  page,
}) => {
  let release!: () => void;
  const ready = new Promise<void>((resolve) => (release = resolve));
  await page.route('**/api/config', async (route) => {
    await ready;
    await route.fulfill({ json: guestConfig });
  });
  await page.goto('/');
  await expect(page.getByRole('button', { name: '読み込み中…', exact: true })).toBeDisabled();
  await expect(page.getByRole('status').filter({ hasText: 'ログイン状態を確認' })).toBeVisible();
  release();
  const login = page.getByRole('button', { name: 'Googleでログイン', exact: true });
  await expect(login).toBeEnabled();
  await login.click();
  await expect(page.getByRole('alert')).toContainText('FirebaseのGoogleログイン設定が必要です。');
  await expect(login).toBeEnabled();
  await page.getByRole('button', { name: '閉じる', exact: true }).click();
  await expect(page.getByRole('alert')).not.toBeVisible();
});
