import type { Wallet } from '../support/api';
import { hoursFromNow } from '../support/api';
import { expect, test } from '../support/fixtures';
import { expectInsideViewport, expectNoHorizontalScroll } from '../support/layout';
import { workspace } from '../support/pages';

test('next task card starts the earliest plan and keeps a long title readable', async ({
  page,
  api,
}) => {
  const title =
    '発表スライドの構成を作り、研究の背景と実験結果を初めて聞く人にも伝わる順序に整理する';
  await api.createTask({
    id: 'summary-later',
    title: '次に取り組む課題',
    deadline: hoursFromNow(3),
  });
  await api.createTask({ id: 'summary-first', title, deadline: hoursFromNow(2) });
  await page.goto('/calendar');
  const { summary } = workspace(page);
  const next = summary.getByRole('button', { name: title });
  await expect(next).toBeVisible();
  await expect(summary.locator('.next-time')).toContainText('30分');
  // 長い課題名は省略せずに折り返す。
  const style = await next.locator('.next-task-title').evaluate((element) => {
    const computed = getComputedStyle(element);
    return { whiteSpace: computed.whiteSpace, overflow: computed.overflow };
  });
  expect(style.whiteSpace).not.toBe('nowrap');
  expect(style.overflow).toBe('visible');
  for (const width of [390, 320]) {
    await page.setViewportSize({ width, height: 844 });
    await expectNoHorizontalScroll(page);
    await expect(next).toBeVisible();
    expectInsideViewport(page, await next.boundingBox());
  }

  const start = summary.getByRole('button', { name: '次の課題をはじめる', exact: true });
  await start.click();
  await expect.poll(async () => (await api.workSession())?.taskId).toBe('summary-first');
  await expect(start).toBeDisabled();
  await expect(page.getByRole('region', { name: '作業タイマー' })).toContainText(title);
});

test('available credit respects the remaining daily limit and explains the selected support mode', async ({
  page,
}) => {
  let patch: (wallet: Wallet) => Wallet = (wallet) => ({
    ...wallet,
    balanceSeconds: 1440,
    availableSeconds: 90,
    policy: { ...wallet.policy, viewedSeconds: wallet.policy.dailyLimitSeconds - 90 },
  });
  await page.route('**/api/wallet', async (route) => {
    const response = await route.fetch();
    await route.fulfill({ response, json: patch((await response.json()) as Wallet) });
  });
  await page.goto('/calendar');
  const home = workspace(page);
  await expect(home.credit).toHaveText('1分30秒');
  await home.creditHelp.click();
  const dialog = page.getByRole('dialog', { name: '視聴時間について' });
  await expect(dialog).toContainText('今日 視聴できる時間');
  await expect(dialog).toContainText('1分30秒');
  await expect(dialog).toContainText('持っている時間 24分');
  await expect(dialog).toContainText('作業を記録すると、5分につき視聴1分');
  await page.keyboard.press('Escape');

  const previous = patch;
  patch = (wallet) => {
    const base = previous(wallet);
    return { ...base, policy: { ...base.policy, mode: 'fixed' } };
  };
  await page.reload();
  await home.creditHelp.click();
  await expect(dialog).toContainText('自分で決めた固定枠');
  await expect(dialog).not.toContainText('作業を記録すると、5分につき視聴1分');
  await page.keyboard.press('Escape');

  patch = (wallet) => ({ ...wallet, policy: { ...wallet.policy, mode: 'self' } });
  await page.reload();
  await expect(home.credit).toHaveText('制限なし');
  await expect(home.summary).toContainText('視聴の支援');
  await home.creditHelp.click();
  await expect(dialog).toContainText('自分で管理する');
  await expect(dialog).not.toContainText('今日 視聴できる時間');
});

test('credit explanation fits narrow screens and does not change state or a consultation draft', async ({
  page,
  api,
}) => {
  const before = { tasks: await api.tasks(), wallet: await api.wallet() };
  await page.goto('/calendar');
  const home = workspace(page);
  await home.composer.fill('明日の発表について相談したい');
  for (const width of [1440, 390, 320]) {
    await page.setViewportSize({ width, height: 844 });
    await home.creditHelp.click();
    const dialog = page.getByRole('dialog', { name: '視聴時間について' });
    await expect(dialog).toBeVisible();
    expectInsideViewport(page, await dialog.boundingBox());
    await dialog.getByText('夜の報酬・支援の段階').click();
    await expect(dialog).toContainText('翌朝');
    await page.keyboard.press('Escape');
    await expect(dialog).toBeHidden();
    await expect(home.creditHelp).toBeFocused();
  }
  await page.getByRole('radio', { name: '相談・詳細', exact: true }).click();
  await expect(home.composer).toHaveValue('明日の発表について相談したい');
  const after = { tasks: await api.tasks(), wallet: await api.wallet() };
  expect(after.tasks.revision).toBe(before.tasks.revision);
  expect(after.wallet.balanceSeconds).toBe(before.wallet.balanceSeconds);

  // 背景を押しても閉じる。「閉じる」ボタンでも閉じる。
  await home.creditHelp.click();
  await page.locator('.cdk-overlay-backdrop').click({ position: { x: 2, y: 2 } });
  await expect(page.getByRole('dialog', { name: '視聴時間について' })).toBeHidden();
  await home.creditHelp.click();
  await page
    .getByRole('dialog', { name: '視聴時間について' })
    .getByRole('button', { name: '閉じる', exact: true })
    .click();
  await expect(page.getByRole('dialog', { name: '視聴時間について' })).toBeHidden();
});
