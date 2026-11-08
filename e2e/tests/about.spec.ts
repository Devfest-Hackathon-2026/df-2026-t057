import { expect, test } from '../support/fixtures';
import { expectNoHorizontalScroll } from '../support/layout';

test('about page explains the purpose and is reachable from the menu', async ({ page }) => {
  await page.goto('/calendar');
  await page
    .getByRole('navigation', { name: 'メインメニュー' })
    .getByRole('link', { name: 'アプリについて' })
    .click();
  await expect(page).toHaveURL(/\/about$/);
  await expect(page).toHaveTitle('このアプリについて — DOPAGAKI');
  await expect(page.getByRole('heading', { level: 1, name: 'このアプリについて' })).toBeVisible();
  await expect(page.getByRole('heading', { name: '目的', exact: true })).toBeVisible();
  await expect(page.getByRole('heading', { name: /禁止ではなく、\s*距離感を。/ })).toBeVisible();
  await expect(
    page.getByRole('navigation', { name: 'メインメニュー' }).getByRole('link', {
      name: 'アプリについて',
    }),
  ).toHaveAttribute('aria-current', 'page');

  await page.setViewportSize({ width: 390, height: 844 });
  await expectNoHorizontalScroll(page);
  await page.getByRole('button', { name: '課題を登録してみる', exact: true }).click();
  await expect(page.getByRole('dialog', { name: '課題を登録' })).toBeVisible();
});
