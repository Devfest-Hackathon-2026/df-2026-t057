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
    // 開いたときは記録だけで、通知する内容は返さない。聞き直すまでの待ち時間を返す。
    const body = (await opened.json()) as { openId: string; action: string; waitSeconds: number };
    expect(body.action).toBe('wait');
    expect(body.waitSeconds).toBeGreaterThanOrEqual(60);
    expect(body.waitSeconds).toBeLessThanOrEqual(900);

    // まだ時刻ではないので、待ち直させる。
    const checked = await phone.post('/api/shortcut/check', {
      headers: { Authorization: `Shortcut ${key}` },
      data: { openId: body.openId },
    });
    expect(checked.status()).toBe(200);
    expect(((await checked.json()) as { action: string }).action).toBe('wait');

    // 閉じたときは記録だけ。閉じたあとの聞き直しは何もしない。
    const closed = await phone.post('/api/shortcut/close', {
      headers: { Authorization: `Shortcut ${key}` },
    });
    expect(closed.status()).toBe(204);
    const afterClose = await phone.post('/api/shortcut/check', {
      headers: { Authorization: `Shortcut ${key}` },
      data: { openId: body.openId },
    });
    expect(((await afterClose.json()) as { action: string }).action).toBe('none');

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
