import { expect, test } from '../support/fixtures';
import { expandPanel } from '../support/pages';

test('a Shortcuts key is shown once, answers like the iPhone automation, and can be revoked', async ({
  page,
  playwright,
  baseURL,
  api,
}) => {
  await page.goto('/calendar');
  await page.getByRole('button', { name: 'サンプル3件で試す', exact: true }).click();
  await expect.poll(async () => (await api.tasks()).items.length).toBe(3);
  await page.getByRole('link', { name: '設定を開く', exact: true }).click();
  const header = await expandPanel(page, 'iPhoneのショートカット');
  await expect(header).toContainText('未設定');
  await page.getByRole('button', { name: '接続キーを発行', exact: true }).click();
  const field = page.getByRole('textbox', { name: '接続キー' });
  await expect(field).not.toBeEditable();
  const key = await field.inputValue();
  expect(key).toMatch(/^\S{16,}$/);
  await expect(header).toContainText('接続キー発行済み');

  // ショートカットは Cookie を持たない。接続キーだけで本人を決める。
  const phone = await playwright.request.newContext({ ...(baseURL ? { baseURL } : {}) });
  try {
    const opened = await phone.post('/api/shortcut/open', {
      headers: { Authorization: `Shortcut ${key}` },
    });
    expect(opened.status()).toBe(200);
    const body = (await opened.json()) as { title: string; message: string; url: string };
    expect(body.title).toContain('DOPAGAKI');
    expect(body.message.length).toBeGreaterThan(0);
    expect(body.url).toContain('?notification=');

    // キーは発行したときにだけ表示する。開き直すと見えない。
    await page.reload();
    await expandPanel(page, 'iPhoneのショートカット');
    await expect(page.getByRole('textbox', { name: '接続キー' })).toHaveCount(0);
    await expect(page.getByRole('button', { name: '接続キーを再発行', exact: true })).toBeVisible();

    await page.getByRole('button', { name: '無効にする', exact: true }).click();
    await expect(page.getByText('接続キーを無効にしました', { exact: true })).toBeVisible();
    const revoked = await phone.post('/api/shortcut/open', {
      headers: { Authorization: `Shortcut ${key}` },
    });
    expect(revoked.status()).toBe(401);
  } finally {
    await phone.dispose();
  }
});
