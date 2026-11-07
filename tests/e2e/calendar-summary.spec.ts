import { test, expect } from '@playwright/test';
import { State } from '../../shared/models';

test.use({ locale: 'ja-JP', timezoneId: 'Asia/Tokyo' });

test('next task card starts the earliest plan and keeps a long title readable', async ({
  page,
}) => {
  await page.request.get('/api/state');
  const title =
    '発表スライドの構成を作り、研究の背景と実験結果を初めて聞く人にも伝わる順序に整理する';
  for (const [index, id] of ['summary-later', 'summary-first'].entries()) {
    await page.request.post('/api/tasks', {
      data: {
        id,
        title: index ? title : '次に取り組む課題',
        notes: '',
        minutes: 30,
        deadline: new Date(Date.now() + (3 - index) * 3600000).toISOString(),
        url: '',
      },
    });
  }
  await page.goto('/calendar');
  const summary = page.getByRole('region', { name: '視聴できる時間と次の課題' });
  await expect(summary.locator('.next-task')).toHaveText(new RegExp(title));
  await expect(summary.locator('.next-task-time')).toContainText('30分');
  const textStyle = await summary.locator('.next-task strong').evaluate((el) => ({
    whiteSpace: getComputedStyle(el).whiteSpace,
    overflow: getComputedStyle(el).overflow,
  }));
  expect(textStyle.whiteSpace).toBe('normal');
  expect(textStyle.overflow).toBe('visible');
  await page.screenshot({
    path: 'test-results/calendar-cards-desktop.png',
    fullPage: true,
    animations: 'disabled',
  });
  for (const width of [390, 320]) {
    await page.setViewportSize({ width, height: 844 });
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(
      true,
    );
    await expect(summary.locator('.next-task')).toBeVisible();
  }
  await page.screenshot({
    path: 'test-results/calendar-cards-mobile.png',
    fullPage: true,
    animations: 'disabled',
  });
  await summary.getByRole('button', { name: '次の課題を開始', exact: true }).click();
  await expect
    .poll(async () => (await (await page.request.get('/api/state')).json()).session?.taskId)
    .toBe('summary-first');
  await expect(summary.getByRole('button', { name: '次の課題を開始', exact: true })).toBeDisabled();
});

test('available credit respects the remaining daily limit and explains the selected support mode', async ({
  page,
}) => {
  const state: State = await (await page.request.get('/api/state')).json();
  state.balanceSeconds = 1440;
  state.timePolicy!.viewedSeconds = state.timePolicy!.dailyLimitSeconds - 90;
  await page.route('**/api/state', (route) => route.fulfill({ json: state }));
  await page.goto('/calendar');
  await expect(page.locator('.credit-number')).toHaveText(/1\s*分\s*30秒/);
  await page.getByRole('button', { name: '視聴時間について', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: '視聴時間について' });
  await expect(dialog.locator('.credit-info-balance')).toContainText('1分30秒');
  await expect(dialog).toContainText('作業を記録すると、5分につき視聴1分');
  await page.keyboard.press('Escape');
  state.timePolicy!.mode = 'fixed';
  await page.reload();
  await page.getByRole('button', { name: '視聴時間について', exact: true }).click();
  await expect(dialog).toContainText('自分で決めた固定枠');
  await expect(dialog).not.toContainText('作業を記録すると、5分につき視聴1分');
  await page.keyboard.press('Escape');
  state.timePolicy!.mode = 'self';
  await page.reload();
  await expect(page.locator('.credit-number')).toHaveText('制限なし');
  await page.getByRole('button', { name: '視聴時間について', exact: true }).click();
  await expect(dialog).toContainText('自分で管理する');
  await expect(dialog).not.toContainText('今日、視聴できる時間');
});

test('credit explanation fits narrow screens and does not change state or a consultation draft', async ({
  page,
}) => {
  const before = await (await page.request.get('/api/state')).json();
  await page.goto('/calendar');
  const composer = page.getByLabel('相談する', { exact: true });
  await composer.fill('明日の発表について相談したい');
  for (const width of [1440, 390, 320]) {
    await page.setViewportSize({ width, height: 844 });
    const help = page.getByRole('button', { name: '視聴時間について', exact: true });
    await help.click();
    const dialog = page.getByRole('dialog', { name: '視聴時間について' });
    await expect(dialog).toBeVisible();
    const box = (await dialog.boundingBox())!;
    expect(box.x).toBeGreaterThanOrEqual(0);
    expect(box.x + box.width).toBeLessThanOrEqual(width);
    expect(box.y + box.height).toBeLessThanOrEqual(844);
    await dialog.locator('summary').click();
    await expect(dialog).toContainText('翌朝');
    await page.screenshot({
      path: `test-results/calendar-help-${width}.png`,
      animations: 'disabled',
    });
    await page.keyboard.press('Escape');
    await expect(help).toBeFocused();
  }
  await page.getByRole('radio', { name: '相談・詳細', exact: true }).click();
  await expect(composer).toHaveValue('明日の発表について相談したい');
  const after = await (await page.request.get('/api/state')).json();
  expect(after.tasks).toEqual(before.tasks);
  expect(after.balanceSeconds).toBe(before.balanceSeconds);
  expect(after.planRevision).toBe(before.planRevision);
  await page.getByRole('button', { name: '視聴時間について', exact: true }).click();
  await page.locator('.cdk-overlay-backdrop').click({ position: { x: 2, y: 2 } });
  await expect(page.getByRole('dialog', { name: '視聴時間について' })).not.toBeVisible();
});
