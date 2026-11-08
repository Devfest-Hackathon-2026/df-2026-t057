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
  await tour.getByRole('button', { name: 'チュートリアルを開始', exact: true }).click();

  const card = page.locator('.tour-card');
  await expect(page.getByRole('dialog', { name: 'まずは課題を登録' })).toBeVisible();
  // 強調している要素（課題を登録するボタン）の上に、明るい枠を出す。
  await expect(page.locator('.tour-spot')).toBeVisible();
  await expect(card).toContainText('2 / 10');
  await card.getByRole('button', { name: '戻る', exact: true }).click();
  await expect(card.getByRole('heading')).toHaveText('DOPAGAKIへようこそ');
  await card.getByRole('button', { name: 'チュートリアルを開始', exact: true }).click();
  // 1段ずつ、見出しが変わってから次を押す（遅い環境でクリックを取りこぼさない）。
  for (let step = 0; step < 8; step++) {
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

test('tutorial can be skipped and replayed from the mobile menu', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto('/?tutorial');
  const card = page.locator('.tour-card');
  await expect(card).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(card).toHaveCount(0);
  await page.reload();
  await expect(page.getByRole('heading', { level: 1, name: 'カレンダー' })).toBeVisible();
  await expect(card).toHaveCount(0);

  // スマホの幅では「使い方」はハンバーガーメニューの中にある
  await page.getByRole('button', { name: 'メニューを開く', exact: true }).click();
  await page.getByRole('menuitem', { name: '使い方', exact: true }).click();
  await expect(card).toBeVisible();
  await expect(
    card.getByRole('button', { name: 'チュートリアルを開始', exact: true }),
  ).toBeFocused();
  await card.getByRole('button', { name: 'チュートリアルを開始', exact: true }).click();
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

test('every tutorial step explains itself in three lines or fewer', async ({ page }) => {
  for (const width of [1440, 390]) {
    await page.setViewportSize({ width, height: 900 });
    await page.goto('/calendar?tutorial');
    const card = page.locator('.tour-card');
    await expect(card).toContainText('1 / 10');
    for (let step = 1; step <= 10; step++) {
      await expect(card).toContainText(`${step} / 10`);
      // 説明の段落ごとに、描画された行数（高さ ÷ 行の高さ）を足す。
      const lines = await card.locator('#tour-body p').evaluateAll((paragraphs) =>
        paragraphs.reduce((sum, paragraph) => {
          const lineHeight = Number.parseFloat(getComputedStyle(paragraph).lineHeight);
          return sum + Math.round(paragraph.getBoundingClientRect().height / lineHeight);
        }, 0),
      );
      const title = (await card.getByRole('heading').textContent()) ?? '';
      expect(lines, `${width}px「${title}」`).toBeLessThanOrEqual(3);
      if (step < 10) {
        await card
          .getByRole('button', { name: step === 1 ? 'チュートリアルを開始' : '次へ', exact: true })
          .click();
        await expect(card.getByRole('heading')).not.toHaveText(title);
      }
    }
  }
});
