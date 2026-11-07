import { test, expect } from '@playwright/test';

test('about page explains the purpose and is reachable from the menu', async ({ page }) => {
  await page.goto('/');
  await page.getByRole('link', { name: 'アプリについて' }).click();
  await expect(page).toHaveURL(/\/about$/);
  await expect(page.getByRole('heading', { name: 'このアプリについて' })).toBeVisible();
  await expect(page.getByRole('heading', { name: '目的' })).toBeVisible();
  await expect(page.getByRole('heading', { name: /禁止ではなく、\s*距離感を。/ })).toBeVisible();
  await page.screenshot({ path: 'test-results/about.png', fullPage: true, animations: 'disabled' });
  await page.setViewportSize({ width: 390, height: 844 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await page.screenshot({
    path: 'test-results/about-mobile.png',
    fullPage: true,
    animations: 'disabled',
  });
  await page.getByRole('button', { name: '課題を登録してみる' }).click();
  await expect(page.getByRole('dialog', { name: '課題を登録' })).toBeVisible();
});
