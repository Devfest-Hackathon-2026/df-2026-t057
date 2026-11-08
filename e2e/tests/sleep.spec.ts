import { expect, test } from '../support/fixtures';
import { expandPanel } from '../support/pages';

test('bedtime is saved, skips a morning greeting on the same day, and can be removed', async ({
  page,
}) => {
  await page.goto('/settings');
  const header = await expandPanel(page, '夜の区切り');
  await expect(header).toContainText('未設定');
  await page.getByLabel('就寝', { exact: true }).fill('23:30');
  await page.getByLabel('起床', { exact: true }).fill('07:00');
  await page.getByRole('button', { name: '就寝の予定を保存', exact: true }).click();
  await expect(page.getByText('就寝の予定を保存しました', { exact: true })).toBeVisible();
  await expect(page.getByText('現在：23:30 就寝・07:00 起床')).toBeVisible();
  await expect(header).toContainText('23:30–07:00');

  // 設定した日の朝は、精算するものがない（あいさつを出さない）。
  const morning = page.waitForResponse(
    (response) =>
      new URL(response.url()).pathname === '/api/sleep/morning' &&
      response.request().method() === 'POST',
  );
  await page.reload();
  const result = (await (await morning).json()) as { morning: unknown };
  expect(result.morning).toBeNull();
  await expect(page.getByText('おはよう')).toHaveCount(0);

  await expandPanel(page, '夜の区切り');
  await page.getByRole('button', { name: '外す', exact: true }).click();
  await expect(page.getByText('就寝の予定を外しました', { exact: true })).toBeVisible();
  await expect(page.getByText('現在：23:30 就寝')).toHaveCount(0);
  await expect(header).toContainText('未設定');
});
