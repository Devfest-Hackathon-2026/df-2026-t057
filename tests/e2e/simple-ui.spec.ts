import { test, expect } from '@playwright/test';

test.use({ locale: 'ja-JP', timezoneId: 'Asia/Tokyo' });

test('collapsed task details preserve drafts and are saved only on confirmation', async ({
  page,
}) => {
  await page.goto('/calendar');
  await page.evaluate(async () => {
    await document.fonts.ready;
  });
  const emptyIcon = page.locator('.calendar-empty > .material-symbols-rounded');
  expect(await emptyIcon.evaluate((el) => el.getBoundingClientRect().width)).toBeLessThanOrEqual(
    32,
  );
  const help = page.getByRole('button', { name: '視聴時間について', exact: true });
  await help.focus();
  await page.keyboard.press('Enter');
  const explanation = page.getByRole('dialog', { name: '視聴時間について' });
  await expect(explanation).toContainText('作業を記録すると、5分につき視聴1分');
  await page.keyboard.press('Escape');
  await expect(explanation).not.toBeVisible();
  await expect(help).toBeFocused();

  await page.getByRole('button', { name: '課題を登録', exact: true }).click();
  const dialog = page.getByRole('dialog');
  const details = dialog.locator('.optional-fields');
  await dialog.screenshot({ path: 'test-results/simple-task-input.png', animations: 'disabled' });
  await dialog.getByLabel('課題名', { exact: true }).fill('詳細を折りたたんで登録');
  await expect(dialog.getByLabel('やること・メモ')).not.toBeVisible();
  await details.locator('summary').focus();
  await page.keyboard.press('Enter');
  await dialog.getByLabel('やること・メモ').fill('構成から書き始める');
  await dialog.getByLabel('目的（任意）').fill('発表に備える');
  await details.locator('summary').click();
  await dialog.getByRole('radio', { name: 'AIに相談', exact: true }).click();
  await dialog.getByRole('radio', { name: '手入力', exact: true }).click();
  await details.locator('summary').click();
  await expect(dialog.getByLabel('やること・メモ')).toHaveValue('構成から書き始める');
  await details.locator('summary').click();
  const before = await (await page.request.get('/api/state')).json();
  expect(before.tasks).toHaveLength(0);
  expect(before.balanceSeconds).toBe(600);
  await dialog.getByRole('button', { name: '登録する', exact: true }).click();
  await expect(dialog).not.toBeVisible();
  const after = await (await page.request.get('/api/state')).json();
  expect(after.tasks).toHaveLength(1);
  expect(after.tasks[0].notes).toBe('構成から書き始める');
  expect(after.tasks[0].why).toBe('発表に備える');
  expect(after.balanceSeconds).toBe(900);
});

test('calendar adapts to mobile while preserving an explicit view choice', async ({ page }) => {
  await page.request.get('/api/state');
  await page.request.post('/api/tasks', {
    data: {
      id: 'simple-ui-task',
      title: 'レポートの構成を考える',
      notes: '',
      minutes: 30,
      deadline: new Date(Date.now() + 3600000).toISOString(),
      url: '',
    },
  });
  await page.goto('/calendar');
  await expect(page.getByRole('heading', { name: '何から始めましょう？' })).toBeVisible();
  await page.getByRole('button', { name: '予定を見直す', exact: true }).click();
  await expect(page.getByLabel('相談する', { exact: true })).toHaveValue('予定を見直す');
  await expect(page.getByRole('radio', { name: '週', exact: true })).toBeChecked();
  await page.screenshot({
    path: 'test-results/simple-calendar-desktop.png',
    fullPage: true,
    animations: 'disabled',
  });
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(page.getByRole('radio', { name: '日', exact: true })).toBeChecked();
  await expect(page.locator('.calendar-surface .event-name')).toContainText(
    'レポートの構成を考える',
  );
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await page.screenshot({
    path: 'test-results/simple-calendar-mobile.png',
    fullPage: true,
    animations: 'disabled',
  });
  await page.getByRole('radio', { name: '週', exact: true }).click();
  await page.setViewportSize({ width: 1440, height: 1080 });
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(page.getByRole('radio', { name: '週', exact: true })).toBeChecked();
  await page.getByRole('link', { name: '課題', exact: true }).click();
  await page.evaluate(async () => {
    await document.fonts.ready;
  });
  const editIcon = page
    .getByRole('button', { name: '課題を編集', exact: true })
    .locator('.material-symbols-rounded');
  expect(await editIcon.evaluate((el) => el.getBoundingClientRect().width)).toBeLessThanOrEqual(24);
  await page.screenshot({
    path: 'test-results/simple-tasks-mobile.png',
    fullPage: true,
    animations: 'disabled',
  });
  await page.getByRole('button', { name: '設定を開く', exact: true }).click();
  await page.screenshot({
    path: 'test-results/simple-settings-mobile.png',
    fullPage: true,
    animations: 'disabled',
  });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});
