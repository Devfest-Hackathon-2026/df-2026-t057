import { expect, test, tokyoDate } from '../support/fixtures';
import { expectInsideViewport } from '../support/layout';

test('?tutorial walks through the screen and ends on task registration', async ({
  page,
  pageErrors,
}) => {
  await page.goto('/tasks?tutorial=1');
  const tour = page.getByRole('dialog', { name: 'DOPAGAKIへようこそ' });
  await expect(tour).toBeVisible();
  await expect(page).toHaveURL(/\/calendar$/);
  await tour.getByRole('button', { name: '案内を見る', exact: true }).click();

  const card = page.locator('.tour-card');
  await expect(page.getByRole('dialog', { name: 'まずは課題を1つ登録' })).toBeVisible();
  // 強調している要素（課題を登録するボタン）の上に、明るい枠を出す。
  await expect(page.locator('.tour-spot')).toBeVisible();
  await expect(card).toContainText('2 / 8');
  await card.getByRole('button', { name: '戻る', exact: true }).click();
  await expect(card.getByRole('heading')).toHaveText('DOPAGAKIへようこそ');
  await card.getByRole('button', { name: '案内を見る', exact: true }).click();
  // 1段ずつ、見出しが変わってから次を押す（遅い環境でクリックを取りこぼさない）。
  for (let step = 0; step < 6; step++) {
    const title = (await card.getByRole('heading').textContent()) ?? '';
    await card.getByRole('button', { name: '次へ', exact: true }).click();
    await expect(card.getByRole('heading')).not.toHaveText(title);
  }
  await expect(card.getByRole('heading')).toHaveText('準備できました');
  await card.getByRole('button', { name: '課題を登録する', exact: true }).click();
  await expect(card).toHaveCount(0);
  await expect(page.getByRole('dialog', { name: '課題を登録' })).toBeVisible();
  expect(pageErrors).toEqual([]);
});

test('tutorial can be skipped and replayed from the top bar', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto('/?tutorial');
  const card = page.locator('.tour-card');
  await expect(card).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(card).toHaveCount(0);
  await page.reload();
  await expect(page.getByRole('heading', { level: 1, name: 'カレンダー' })).toBeVisible();
  await expect(card).toHaveCount(0);

  await page.getByRole('button', { name: '使い方', exact: true }).click();
  await expect(card).toBeVisible();
  await expect(card.getByRole('button', { name: '案内を見る', exact: true })).toBeFocused();
  await card.getByRole('button', { name: '案内を見る', exact: true }).click();
  await expect(page.locator('.tour-spot')).toBeVisible();
  expectInsideViewport(page, await card.boundingBox());
  await card.getByRole('button', { name: '終了', exact: true }).click();
  await expect(card).toHaveCount(0);
});

test('deadline is chosen from quick days, a calendar and a time list', async ({ page, api }) => {
  await page.goto('/calendar');
  await page.getByRole('button', { name: '課題を登録', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: '課題を登録' });
  await dialog.getByLabel('課題名', { exact: true }).fill('締切を選ぶ課題');
  const week = dialog.getByRole('button', { name: '1週間後', exact: true });
  await week.click();
  await expect(week).toHaveAttribute('aria-pressed', 'true');
  await dialog.getByRole('combobox', { name: '締切時刻' }).click();
  await page.getByRole('option', { name: '18:00', exact: true }).click();
  await dialog.getByRole('button', { name: '1時間', exact: true }).click();
  await expect(dialog.getByLabel('必要時間（分）')).toHaveValue('60');
  await expect(dialog.getByRole('button', { name: '1時間', exact: true })).toHaveAttribute(
    'aria-pressed',
    'true',
  );
  await expect(dialog.getByText(/18:00 まで（あと[67]日/)).toBeVisible();

  // カレンダーのポップアップから選んでも、同じ日のままにできる。
  await dialog.getByLabel('締切日').click();
  const picker = page.locator('mat-calendar');
  await expect(picker).toBeVisible();
  await picker.locator('.mat-calendar-body-selected').click();
  await expect(picker).toHaveCount(0);

  await dialog.getByRole('button', { name: '登録する', exact: true }).click();
  await expect(dialog).toBeHidden();
  const [task] = (await api.tasks()).items;
  expect(task?.minutes).toBe(60);
  const expected = new Date(Date.now() + 7 * 86_400_000);
  expect(tokyoDate(task?.deadline ?? '')).toBe(tokyoDate(expected));
  expect(
    new Date(task?.deadline ?? '').toLocaleTimeString('en-GB', {
      timeZone: 'Asia/Tokyo',
      hour: '2-digit',
      minute: '2-digit',
    }),
  ).toBe('18:00');
});

test('on a phone the consultation step switches to the consultation view', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto('/calendar?tutorial');
  const card = page.locator('.tour-card');
  await card.getByRole('button', { name: '案内を見る', exact: true }).click();
  // 狭い画面ではカレンダーと相談の片方しか出ない。「相談」の段階では相談を出して強調する。
  while (!((await card.getByRole('heading').textContent()) ?? '').includes('相談')) {
    const title = (await card.getByRole('heading').textContent()) ?? '';
    await card.getByRole('button', { name: '次へ', exact: true }).click();
    await expect(card.getByRole('heading')).not.toHaveText(title);
  }
  await expect(card.getByRole('heading')).toHaveText('言葉で相談することもできます');
  await expect(page.getByRole('radio', { name: '相談・詳細', exact: true })).toBeChecked();
  await expect(page.locator('[data-tour="assistant"]')).toBeVisible();
  await expect(page.locator('.tour-spot')).toBeVisible();
  // 戻るとカレンダーの段階になり、カレンダーを出す。
  await card.getByRole('button', { name: '戻る', exact: true }).click();
  await expect(page.getByRole('radio', { name: 'カレンダー', exact: true })).toBeChecked();
  await expect(page.locator('.tour-spot')).toBeVisible();
});
