import type { LedgerEntry } from '../support/api';
import { hoursFromNow } from '../support/api';
import { expect, test } from '../support/fixtures';
import { expandPanel, workspace } from '../support/pages';
import { fulfillProblem } from '../support/routes';

test('viewing policy changes preserve balance, failures and the selected support stage', async ({
  page,
  api,
}) => {
  await page.goto('/settings');
  await expandPanel(page, '視聴ルール');
  await expect(page.getByRole('button', { name: '新しい方式を使う', exact: true })).toHaveCount(0);
  const before = await api.wallet();
  expect(before.balanceSeconds).toBe(600);
  expect(before.policy.mode).toBe('rewards');

  const stage = page.getByRole('combobox', { name: '支援の段階' });
  const limit = page.getByRole('combobox', { name: '1日の視聴上限' });
  await stage.click();
  await page.getByRole('option', { name: '毎日決まった時間', exact: true }).click();
  await expect(page.getByText('毎日、選んだ枠まで補充。作業での追加はなし。')).toBeVisible();
  await limit.click();
  await page.getByRole('option', { name: '20分', exact: true }).click();
  await page.route('**/api/wallet/policy', (route) => fulfillProblem(route, 503, 'internal'));
  await page.getByRole('button', { name: '視聴ルールを保存', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('保存できませんでした');
  await expect(stage).toContainText('毎日決まった時間');
  expect((await api.wallet()).policy.mode).toBe('rewards');

  await page.unroute('**/api/wallet/policy');
  await page.getByRole('button', { name: '視聴ルールを保存', exact: true }).click();
  await expect(page.getByText('視聴時間の設定を保存しました', { exact: true })).toBeVisible();
  let after = await api.wallet();
  expect(after.balanceSeconds).toBe(before.balanceSeconds);
  expect(after.policy.dailyLimitSeconds).toBe(1200);
  expect(after.policy.mode).toBe('fixed');

  await page.reload();
  const header = await expandPanel(page, '視聴ルール');
  await expect(header).toContainText('毎日決まった時間');
  await expect(stage).toContainText('毎日決まった時間');
  await stage.click();
  await page.getByRole('option', { name: '自分で区切る', exact: true }).click();
  await expect(limit).toHaveAttribute('aria-disabled', 'true');
  await page.getByRole('button', { name: '視聴ルールを保存', exact: true }).click();
  await expect.poll(async () => (await api.wallet()).policy.mode).toBe('self');

  await page.getByRole('link', { name: 'カレンダー', exact: true }).click();
  await expect(workspace(page).credit).toHaveText('制限なし');
  after = await api.wallet();
  expect(after.balanceSeconds).toBe(600);
});

test('policy cannot change while a work session is running', async ({ page, api }) => {
  const task = await api.createTask({ title: '作業中の課題', deadline: hoursFromNow(3) });
  await api.startTask(task.id);
  await page.goto('/settings');
  await expandPanel(page, '視聴ルール');
  await expect(page.getByRole('button', { name: '視聴ルールを保存', exact: true })).toBeDisabled();
  await expect(page.getByText('作業を記録してから設定を変更できます。')).toBeVisible();
});

test('deductions are shown once as negative amounts in the history', async ({ page }) => {
  const deduction: LedgerEntry = {
    id: 'viewing-fixture',
    kind: 'viewing',
    seconds: -6600,
    deferred: false,
    at: new Date().toISOString(),
  };
  await page.route('**/api/wallet/ledger**', async (route) => {
    const response = await route.fetch();
    const body = (await response.json()) as { entries: LedgerEntry[] };
    await route.fulfill({ response, json: { ...body, entries: [deduction, ...body.entries] } });
  });
  await page.goto('/records');
  const history = page.getByRole('region', { name: '最近の積み上げ' });
  const row = history.getByRole('listitem').filter({ hasText: '−110分' });
  await expect(row).toHaveCount(1);
  await expect(row).toContainText('視聴');
  await page.reload();
  await expect(row).toHaveCount(1);
});

test('partial work confirmation keeps the task and does not promise unearned minutes', async ({
  page,
  api,
}) => {
  await api.createTask({
    id: 'partial-work',
    title: '構成だけ書く',
    minutes: 60,
    deadline: hoursFromNow(3),
  });
  await page.goto('/tasks');
  await page
    .getByRole('list', { name: '課題一覧' })
    .getByRole('listitem')
    .filter({ hasText: '構成だけ書く' })
    .getByRole('button', { name: 'はじめる', exact: true })
    .click();
  const timer = page.getByRole('region', { name: '作業タイマー' });
  // 1回の作業は25分まで記録する。
  await expect(timer).toContainText('/ 25分');
  const before = await api.wallet();
  const session = await api.workSession();
  expect(session?.taskId).toBe('partial-work');

  await page.route('**/api/work-session/confirm', (route) =>
    fulfillProblem(route, 503, 'internal'),
  );
  await timer.getByRole('button', { name: 'ここまで取り組んだ', exact: true }).click();
  await expect(page.getByText(/作業を保存できませんでした。/)).toBeVisible();
  await expect(timer).toBeVisible();
  await page.unroute('**/api/work-session/confirm');
  await timer.getByRole('button', { name: 'ここまで取り組んだ', exact: true }).click();
  await expect(timer).toBeHidden();
  // 5分に満たない作業では視聴時間は増えない（増えると約束しない）。
  await expect(page.getByText('作業を記録しました', { exact: true })).toBeVisible();

  const after = await api.wallet();
  expect(after.balanceSeconds).toBe(before.balanceSeconds);
  expect((await api.task('partial-work')).status).toBe('pending');
  const ledger = await api.ledger();
  expect(ledger.filter((entry) => entry.kind === 'work')).toHaveLength(1);
  // 記録済みの作業を確定し直しても、何も増えない。
  await api.confirmWork(session?.id ?? '');
  expect(await api.ledger()).toHaveLength(ledger.length);
});
