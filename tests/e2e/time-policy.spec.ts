import { test, expect } from '@playwright/test';

test('viewing policy changes preserve balance, failures and the selected support stage', async ({
  page,
}) => {
  await page.goto('/settings');
  const before = await (await page.request.get('/api/state')).json();
  expect(before.balanceSeconds).toBe(600);
  await page.getByRole('combobox', { name: '支援の段階' }).click();
  await page.getByRole('option', { name: '自分で決めた固定枠', exact: true }).click();
  await page.getByRole('combobox', { name: '1日の視聴上限' }).click();
  await page.getByRole('option', { name: '20分', exact: true }).click();
  await page.route('**/api/time-policy', (route) =>
    route.fulfill({
      status: 503,
      contentType: 'application/json',
      body: JSON.stringify({ error: '保存できませんでした' }),
    }),
  );
  await page.getByRole('button', { name: '視聴ルールを保存', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('保存できませんでした');
  await expect(page.getByRole('combobox', { name: '支援の段階' })).toContainText(
    '自分で決めた固定枠',
  );
  expect((await (await page.request.get('/api/state')).json()).timePolicy.mode).toBe('rewards');
  await page.unroute('**/api/time-policy');
  await page.getByRole('button', { name: '視聴ルールを保存', exact: true }).click();
  await expect(page.getByText('視聴時間の設定を保存しました', { exact: true })).toBeVisible();
  let after = await (await page.request.get('/api/state')).json();
  expect(after.balanceSeconds).toBe(before.balanceSeconds);
  expect(after.timePolicy.dailyLimitSeconds).toBe(1200);
  expect(after.timePolicy.mode).toBe('fixed');
  await page.reload();
  await expect(page.getByRole('combobox', { name: '支援の段階' })).toContainText(
    '自分で決めた固定枠',
  );
  await page.getByRole('combobox', { name: '支援の段階' }).click();
  await page.getByRole('option', { name: '自分で管理する', exact: true }).click();
  await expect(page.getByRole('combobox', { name: '1日の視聴上限' })).toBeDisabled();
  await page.getByRole('button', { name: '視聴ルールを保存', exact: true }).click();
  await expect
    .poll(async () => (await (await page.request.get('/api/state')).json()).timePolicy.mode)
    .toBe('self');
  await page.getByRole('link', { name: 'カレンダー', exact: true }).click();
  await expect(page.locator('.credit-number')).toContainText('制限なし');
  after = await (await page.request.get('/api/state')).json();
  expect(after.balanceSeconds).toBe(600);
});

test('partial work confirmation keeps the task and does not promise unearned minutes', async ({
  page,
}) => {
  await page.request.get('/api/state');
  await page.request.post('/api/tasks', {
    data: {
      id: 'partial-work',
      title: '構成だけ書く',
      notes: '',
      minutes: 60,
      deadline: new Date(Date.now() + 3 * 3600000).toISOString(),
      url: '',
    },
  });
  await page.goto('/tasks');
  await page.locator('.task-row').getByRole('button', { name: 'はじめる', exact: true }).click();
  const timer = page.getByRole('region', { name: '作業タイマー' });
  await expect(timer).toContainText('/ 25分');
  const before = await (await page.request.get('/api/state')).json();
  await page.route('**/api/session/confirm', (route) =>
    route.fulfill({
      status: 503,
      contentType: 'application/json',
      body: JSON.stringify({ error: '作業を保存できませんでした' }),
    }),
  );
  await timer.getByRole('button', { name: 'ここまで取り組んだ', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('作業を保存できませんでした');
  await expect(timer).toBeVisible();
  await page.unroute('**/api/session/confirm');
  await timer.getByRole('button', { name: 'ここまで取り組んだ', exact: true }).click();
  await expect(timer).not.toBeVisible();
  await expect(page.getByText('作業を記録しました', { exact: true })).toBeVisible();
  const after = await (await page.request.get('/api/state')).json();
  expect(after.balanceSeconds).toBe(before.balanceSeconds);
  expect(after.tasks[0].status).toBe('pending');
  expect(after.ledger.filter((e: { kind: string }) => e.kind === 'work')).toHaveLength(1);
  await page.request.post('/api/session/confirm', { data: { id: before.session.id } });
  expect((await (await page.request.get('/api/state')).json()).ledger.length).toBe(
    after.ledger.length,
  );
});
