import { test, expect } from '@playwright/test';

test('a Shortcuts key is shown once, answers like the iPhone automation, and can be revoked', async ({
  page,
  playwright,
  baseURL,
}) => {
  await page.goto('/');
  await page.getByRole('button', { name: 'サンプル3件で試す' }).click();
  await page.getByRole('button', { name: '設定を開く', exact: true }).click();
  await page.getByRole('button', { name: /iPhoneのショートカット/ }).click();
  await page.getByRole('button', { name: '接続キーを発行', exact: true }).click();
  const key = await page.getByRole('textbox', { name: '接続キー' }).inputValue();
  expect(key).toMatch(/^[\w-]+\.[a-f0-9]{64}$/);

  // The automation has no cookie: only the key identifies the account.
  const phone = await playwright.request.newContext({ baseURL });
  const opened = await phone.post('/api/shortcut/open', {
    headers: { Authorization: `Shortcut ${key}` },
  });
  expect(opened.status()).toBe(200);
  const body = await opened.json();
  expect(body.title).toContain('DOPAGAKI');
  expect(body.message.length).toBeGreaterThan(0);
  expect(body.url).toContain('?notification=');

  await page.getByRole('button', { name: '無効にする', exact: true }).click();
  await expect(page.getByText('接続キーを無効にしました')).toBeVisible();
  const revoked = await phone.post('/api/shortcut/open', {
    headers: { Authorization: `Shortcut ${key}` },
  });
  expect(revoked.status()).toBe(401);
  await phone.dispose();
});
