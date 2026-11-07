import { test, expect } from '@playwright/test';

test('bedtime is saved, skips a morning greeting on the same day, and can be removed', async ({
  page,
}) => {
  await page.goto('/');
  await page.getByRole('button', { name: 'サンプル3件で試す' }).click();
  await page.getByRole('button', { name: '設定を開く', exact: true }).click();
  await page.getByLabel('就寝').fill('23:30');
  await page.getByLabel('起床').fill('07:00');
  await page.getByRole('button', { name: '就寝の予定を保存', exact: true }).click();
  await expect(page.getByText('就寝の予定を保存しました')).toBeVisible();
  await expect(page.getByText('現在：23:30 就寝・07:00 起床')).toBeVisible();

  const morning = page.waitForResponse((r) => r.url().endsWith('/api/sleep/morning'));
  await page.reload();
  const result = await (await morning).json();
  expect(result.morning).toBeNull();
  await expect(page.getByText('おはよう')).not.toBeVisible();

  await page.getByRole('button', { name: '外す', exact: true }).click();
  await expect(page.getByText('就寝の予定を外しました')).toBeVisible();
  await expect(page.getByText('現在：23:30 就寝')).not.toBeVisible();
});
