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

test('on a phone the header folds the menu into a hamburger button', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto('/calendar');
  await expect(page.getByRole('navigation', { name: 'メインメニュー' })).toBeHidden();
  const button = page.getByRole('button', { name: 'メニューを開く', exact: true });
  await expect(button).toBeVisible();
  await expect(page.getByRole('button', { name: '使い方', exact: true })).toBeHidden();

  await button.click();
  const menu = page.getByRole('menu', { name: 'メインメニュー' });
  await expect(menu.getByRole('menuitem', { name: 'カレンダー' })).toHaveAttribute(
    'aria-current',
    'page',
  );
  await menu.getByRole('menuitem', { name: 'アプリについて' }).click();
  await expect(page).toHaveURL(/\/about$/);
  await expect(menu).toBeHidden();
  await expect(page.getByRole('heading', { level: 1, name: 'このアプリについて' })).toBeVisible();
  await expectNoHorizontalScroll(page);

  // キーボードでも開け、Escで閉じるとボタンにフォーカスが戻る
  await button.focus();
  await page.keyboard.press('Enter');
  await expect(menu).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(menu).toBeHidden();
  await expect(button).toBeFocused();

  await page.setViewportSize({ width: 1440, height: 1080 });
  await expect(button).toBeHidden();
  await expect(page.getByRole('navigation', { name: 'メインメニュー' })).toBeVisible();
});
