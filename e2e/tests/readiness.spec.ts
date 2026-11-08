import { expect, test } from '../support/fixtures';
import { expandPanel } from '../support/pages';
import { idempotencyKey } from '../support/api';

test('archiving and restoring keeps completion totals and exports owned records', async ({
  page,
  api,
}) => {
  const task = await api.createTask({ title: 'アーカイブ回帰用の課題' });
  await api.completeTask(task.id);
  await page.goto('/tasks');
  await page.getByRole('radio', { name: '完了', exact: true }).click();
  const row = page.getByRole('listitem').filter({ hasText: task.title });
  await row.getByRole('button', { name: 'アーカイブ', exact: true }).click();
  await expect(row).toHaveCount(0);
  await page.getByRole('button', { name: '一覧を表示', exact: true }).click();
  await expect(page.getByText(task.title, { exact: false })).toBeVisible();
  await page.getByRole('button', { name: '完了一覧へ戻す' }).click();
  await expect(row).toBeVisible();
  await page.goto('/records');
  await expect(page.getByRole('heading', { name: '週ごとの振り返り' })).toBeVisible();
  const result = await page.request.get('/api/records');
  expect(result.ok()).toBe(true);
  expect(((await result.json()) as { totals: { completed: number } }).totals.completed).toBe(1);
  const exported = await page.request.get('/api/account/export');
  expect(exported.ok()).toBe(true);
  const body = await exported.text();
  expect(body).toContain('completionRecords');
  expect(body).not.toContain('receiptHash');
});

test('diagnostics distinguish extension presence from account and notification confirmation', async ({
  page,
}) => {
  await page.goto('/settings');
  await expandPanel(page, '導入と接続');
  await expect(page.getByText('同じアカウントで接続：未確認')).toBeVisible();
  await page.getByRole('button', { name: '接続を診断', exact: true }).click();
  await expect(page.getByRole('button', { name: '接続を診断', exact: true })).toBeEnabled();
  await expect(page.getByRole('button', { name: 'テスト通知を確認した' })).toBeDisabled();
});

test('deletion receipt can be replayed after acceptance and normal writes are fenced', async ({
  page,
}) => {
  await page.goto('/settings');
  const key = idempotencyKey();
  const options = { headers: { 'Idempotency-Key': key }, data: { confirmation: 'DELETE' } };
  const first = await page.request.post('/api/account/deletion', options);
  expect(first.status()).toBe(202);
  const body = (await first.json()) as { receipt: string };
  expect(body.receipt).toHaveLength(64);
  const second = await page.request.post('/api/account/deletion', options);
  expect(second.status(), await second.text()).toBe(202);
  expect(await second.json()).toEqual(body);
  const blocked = await page.request.get('/api/tasks');
  expect(blocked.status()).toBe(403);
  await page.goto('/deletion-status#' + body.receipt);
  await expect(page.getByRole('status')).toContainText('削除を受け付けました');
});
