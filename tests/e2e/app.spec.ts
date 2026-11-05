import { test, expect } from '@playwright/test';

test('registration, confirmation, timer and completion', async ({ page }) => {
  const errors: string[] = [];
  page.on('pageerror', (error) => errors.push(error.message));
  await page.goto('/');
  await expect(page.locator('.credit-number')).toContainText('60');
  await page.getByRole('button', { name: '課題を登録', exact: true }).click();
  const dialog = page.getByRole('dialog');
  await dialog.getByLabel('課題名', { exact: true }).fill('演習レポート');
  await dialog.getByLabel('必要時間（分）').fill('30');
  await dialog.getByRole('button', { name: '登録する', exact: true }).click();
  await expect(dialog).not.toBeVisible();
  await expect(page.locator('.credit-number')).toContainText('70');
  await page.getByRole('button', { name: '通知を試す' }).click();
  await expect(page.getByRole('heading', { name: 'そろそろ、やるか。' })).toBeVisible();
  await page.screenshot({ animations: 'disabled', path: 'test-results/notification.png' });
  await page.getByRole('button', { name: '10分スヌーズ・予定を確認' }).click();
  await expect(dialog.getByText('OKを押すまで、現在の予定は変わりません。')).toBeVisible();
  await dialog.getByRole('button', { name: 'キャンセル' }).click();
  await page.locator('.task-row').getByRole('button', { name: 'はじめる' }).click();
  await expect(page.getByRole('region', { name: '作業タイマー' })).toBeVisible();
  await page.getByRole('button', { name: '一時停止', exact: true }).click();
  await expect(page.getByRole('button', { name: '再開', exact: true })).toBeVisible();
  await page.reload();
  await expect(page.getByRole('button', { name: '再開', exact: true })).toBeVisible();
  await page.getByRole('button', { name: '完了 ＋20分', exact: true }).click();
  await expect(page.locator('.credit-number')).toContainText('90');
  await expect(page.locator('.focus-bar')).not.toBeVisible();
  expect(errors).toEqual([]);
});

test('snooze commit, settings and responsive layout', async ({ page }) => {
  await page.goto('/');
  await page.getByRole('button', { name: 'サンプル3件で試す' }).click();
  await expect(page.locator('.task-row')).toHaveCount(3);
  await page.screenshot({
    animations: 'disabled',
    path: 'test-results/dashboard.png',
    fullPage: true,
  });
  await page.getByRole('button', { name: '通知を試す' }).click();
  await page.getByRole('button', { name: '10分スヌーズ・予定を確認' }).click();
  const dialog = page.getByRole('dialog');
  await expect(dialog.locator('.plan-change')).toHaveCount(3);
  await dialog.getByRole('button', { name: 'OK・この予定に変更' }).click();
  await expect(dialog).not.toBeVisible();
  await expect(page.locator('.credit-number')).toContainText('90');
  await page.getByRole('button', { name: '設定', exact: true }).click();
  await page.getByLabel('分', { exact: true }).fill('15');
  await page.getByRole('button', { name: '設定を保存', exact: true }).click();
  await expect(page.getByText('設定を保存しました')).toBeVisible();
  await page.getByRole('button', { name: '今日の課題', exact: true }).click();
  await page.getByRole('button', { name: '通知を試す' }).click();
  await expect(page.getByRole('button', { name: '15分スヌーズ・予定を確認' })).toBeVisible();
  await page
    .locator('.notification-backdrop')
    .getByRole('button', { name: '閉じる', exact: true })
    .click();
  await expect(page.locator('.notification-backdrop')).not.toBeVisible();
  await page.setViewportSize({ width: 390, height: 844 });
  await page.screenshot({
    animations: 'disabled',
    path: 'test-results/mobile.png',
    fullPage: true,
  });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});

test('failed registration preserves input and loading state', async ({ page }) => {
  await page.goto('/');
  await page.getByRole('button', { name: '課題を登録', exact: true }).click();
  const dialog = page.getByRole('dialog');
  await dialog.getByLabel('課題名', { exact: true }).fill('入力を残す課題');
  await page.route('**/api/tasks', async (route) => {
    await new Promise((resolve) => setTimeout(resolve, 350));
    await route.fulfill({
      status: 503,
      contentType: 'application/json',
      body: JSON.stringify({ error: '保存に失敗しました。再試行してください。' }),
    });
  });
  await dialog.getByRole('button', { name: '登録する', exact: true }).click();
  await expect(dialog.getByRole('button', { name: '処理中…', exact: true })).toBeDisabled();
  await expect(dialog.getByRole('alert')).toContainText('保存に失敗');
  await expect(dialog.getByLabel('課題名', { exact: true })).toHaveValue('入力を残す課題');
});

test('notification links wait for state and snooze opens confirmation directly', async ({
  page,
}) => {
  await page.request.get('/api/state');
  await page.request.post('/api/tasks', {
    data: {
      id: 'notice-route',
      title: '通知リンクの課題',
      notes: '',
      minutes: 30,
      deadline: new Date(Date.now() + 2 * 3600000).toISOString(),
      url: '',
    },
  });
  await page.route('**/api/state', async (route) => {
    await new Promise((resolve) => setTimeout(resolve, 250));
    await route.continue();
  });
  await page.goto('/?notification=notice-route');
  await expect(page.locator('.notification-backdrop')).toContainText('通知リンクの課題');
  await page.goto('/?snooze=notice-route');
  await expect(
    page.getByRole('dialog').getByRole('heading', { name: 'スヌーズ後の予定を確認' }),
  ).toBeVisible();
  await expect(page.locator('.notification-backdrop')).not.toBeVisible();
});
