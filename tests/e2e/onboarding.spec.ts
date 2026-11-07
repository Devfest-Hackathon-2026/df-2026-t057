import { test, expect } from '@playwright/test';

test('?tutorial walks through the screen and ends on task registration', async ({ page }) => {
  const errors: string[] = [];
  page.on('pageerror', (error) => errors.push(error.message));
  await page.goto('/tasks?tutorial=1');
  const tour = page.getByRole('dialog', { name: 'DOPAGAKIへようこそ' });
  await expect(tour).toBeVisible();
  await expect(page).toHaveURL(/\/calendar$/);
  await tour.getByRole('button', { name: '案内を見る' }).click();
  const card = page.locator('.tour-card');
  await expect(card.getByRole('heading')).toHaveText('まずは課題を1つ登録');
  await expect(page.locator('.tour-spot')).toBeVisible();
  await card.screenshot({ path: 'test-results/tutorial-step.png', animations: 'disabled' });
  await page.screenshot({ path: 'test-results/tutorial.png', animations: 'disabled' });
  await card.getByRole('button', { name: '戻る' }).click();
  await expect(card.getByRole('heading')).toHaveText('DOPAGAKIへようこそ');
  await card.getByRole('button', { name: '案内を見る' }).click();
  // Wait for each step to change before the next click, so a slow runner does not drop a click.
  for (let i = 0; i < 6; i++) {
    const title = await card.getByRole('heading').textContent();
    await card.getByRole('button', { name: '次へ' }).click();
    await expect(card.getByRole('heading')).not.toHaveText(title!);
  }
  await expect(card.getByRole('heading')).toHaveText('準備できました');
  await card.getByRole('button', { name: '課題を登録する' }).click();
  await expect(card).toHaveCount(0);
  await expect(page.getByRole('dialog').getByRole('heading', { name: '課題を登録' })).toBeVisible();
  expect(errors).toEqual([]);
});

test('tutorial can be skipped and replayed from the top bar', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto('/?tutorial');
  const card = page.locator('.tour-card');
  await expect(card).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(card).toHaveCount(0);
  await page.reload();
  await expect(page.locator('.credit-number')).toBeVisible();
  await expect(card).toHaveCount(0);
  await page.getByRole('button', { name: '使い方', exact: true }).click();
  await expect(card).toBeVisible();
  await card.getByRole('button', { name: '案内を見る' }).click();
  await expect(page.locator('.tour-spot')).toBeVisible();
  await page.screenshot({ path: 'test-results/tutorial-mobile.png', animations: 'disabled' });
  const box = await card.boundingBox();
  expect(box!.x).toBeGreaterThanOrEqual(0);
  expect(box!.x + box!.width).toBeLessThanOrEqual(390);
});

test('deadline is chosen from quick days, a calendar and a time list', async ({ page }) => {
  await page.goto('/');
  await page.getByRole('button', { name: '課題を登録', exact: true }).click();
  const dialog = page.getByRole('dialog');
  await dialog.getByLabel('課題名', { exact: true }).fill('締切を選ぶ課題');
  await dialog.getByRole('button', { name: '1週間後', exact: true }).click();
  await expect(dialog.getByRole('button', { name: '1週間後' })).toHaveAttribute(
    'aria-pressed',
    'true',
  );
  await dialog.getByLabel('締切時刻').click();
  await page.getByRole('option', { name: '18:00', exact: true }).click();
  await dialog.getByRole('button', { name: '1時間', exact: true }).click();
  await expect(dialog.getByLabel('必要時間（分）')).toHaveValue('60');
  await expect(dialog.locator('.summary')).toContainText(/18:00 まで（あと[67]日/);
  await dialog.screenshot({ path: 'test-results/deadline-input.png', animations: 'disabled' });
  // Calendar popup.
  await dialog.getByLabel('締切日').click();
  await expect(page.locator('mat-calendar')).toBeVisible();
  // Choosing a day closes the popup (Escape can race the popup's focus on slow CI).
  await page.locator('mat-calendar .mat-calendar-body-selected').click();
  await expect(page.locator('mat-calendar')).toHaveCount(0);
  await dialog.getByRole('button', { name: '登録する', exact: true }).click();
  await expect(dialog).not.toBeVisible();
  const state = await (await page.request.get('/api/state')).json();
  const deadline = new Date(state.tasks[0].deadline);
  const expected = new Date();
  expected.setDate(expected.getDate() + 7);
  expect(deadline.getDate()).toBe(expected.getDate());
  expect([deadline.getHours(), deadline.getMinutes()]).toEqual([18, 0]);
  expect(state.tasks[0].minutes).toBe(60);
});
