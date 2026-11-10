import type { Config } from '../support/api';
import { expect, test } from '../support/fixtures';
import { expectNoHorizontalScroll } from '../support/layout';
import { expectAccessibleInBothSchemes } from '../support/a11y';

/** Firebase の設定がない live の環境（ログインしていない人に見える画面）。 */
const signedOutConfig: Config = { mode: 'live', features: { ai: false, calendar: false } };

test(
  'signed-out preview never loads personal data and demo still opens the workspace',
  { tag: '@smoke' },
  async ({ page }) => {
    const apiRequests: string[] = [];
    page.on('request', (request) => {
      const { pathname } = new URL(request.url());
      if (pathname.startsWith('/api/')) apiRequests.push(pathname);
    });
    await page.route('**/api/config', (route) => route.fulfill({ json: signedOutConfig }));
    await page.goto('/calendar?date=2026-11-07');

    await expect(page.getByRole('button', { name: 'Googleでログイン', exact: true })).toBeEnabled();
    await expect(
      page.getByRole('heading', { level: 1, name: 'やることを、次の楽しみに。' }),
    ).toBeVisible();
    await expect(page.getByRole('navigation', { name: 'メインメニュー' })).toHaveCount(0);
    await expect(page.getByRole('banner')).toHaveCount(0);

    await expect(page.getByRole('img', { name: 'DOPAGAKI', exact: true })).toBeVisible();
    await expect(page.getByText('課題を進めた分だけ、Shortsを楽しめる。')).toBeVisible();
    await expect(page.getByRole('button')).toHaveCount(1);
    // ログイン前は、個人のデータを読みに行かない。
    expect(apiRequests).toEqual(['/api/config']);
    await page.setViewportSize({ width: 320, height: 844 });
    await expectNoHorizontalScroll(page);
    await expect(page.getByRole('button', { name: 'Googleでログイン', exact: true })).toBeVisible();

    await expectAccessibleInBothSchemes(page);

    // デモの設定に戻すと、同じ URL のままワークスペースが開く。
    await page.unroute('**/api/config');
    await page.setViewportSize({ width: 1440, height: 1080 });
    await page.reload();
    await expect(page.getByRole('navigation', { name: 'メインメニュー' })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Googleでログイン', exact: true })).toHaveCount(
      0,
    );
    await expect(page).toHaveURL(/\/calendar\?date=2026-11-07$/);
  },
);
