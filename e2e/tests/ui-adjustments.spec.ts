import { expectAccessibleInBothSchemes } from '../support/a11y';
import { expect, test, tokyoLocalInput } from '../support/fixtures';
import { expectNoHorizontalScroll } from '../support/layout';
import { workspace } from '../support/pages';

test('deadline accepts typed minutes, keeps them after saving and rejects invalid edits', async ({
  page,
  api,
  pageErrors,
}) => {
  await page.goto('/calendar');
  await page.getByRole('button', { name: '課題を登録', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: '課題を登録' });
  await dialog.getByLabel('課題名', { exact: true }).fill('分単位の締切を確認');
  await dialog.getByRole('button', { name: '明日', exact: true }).click();
  await page.evaluate(async () => {
    await document.fonts.ready;
  });
  const arrowWidth = await dialog
    .getByRole('button', { name: '締切時刻の候補を開く' })
    .locator('mat-icon')
    .evaluate((icon) => {
      const range = document.createRange();
      range.selectNodeContents(icon);
      return range.getBoundingClientRect().width;
    });
  expect(arrowWidth).toBeLessThanOrEqual(25);
  const time = dialog.getByRole('combobox', { name: '締切時刻', exact: true });
  await time.fill('12:17');
  await time.press('Tab');
  expect(await time.evaluate((input: HTMLInputElement) => input.validity.valid)).toBe(true);
  await expect(dialog.getByText(/12:17 まで/)).toBeVisible();
  await dialog.getByRole('radio', { name: 'AIに相談', exact: true }).click();
  await dialog.getByRole('radio', { name: '手入力', exact: true }).click();
  await expect(time).toHaveValue('12:17');
  await time.fill('25:61');
  await time.press('Tab');
  await expect(dialog.getByRole('button', { name: '登録する', exact: true })).toBeDisabled();
  await expect(dialog.getByText('00:00〜23:59で入力してください')).toBeVisible();
  await time.fill('12:17');
  await dialog.getByRole('button', { name: '締切時刻の候補を開く' }).click();
  await expect(page.getByRole('option', { name: '12:15', exact: true })).toBeVisible();
  await expectAccessibleInBothSchemes(page);
  await page.getByRole('listbox').screenshot({ path: 'test-results/ui-time-options.png' });
  await page.getByRole('option', { name: '12:30', exact: true }).click();
  await expect(time).toHaveValue('12:30');
  await time.fill('12:17');
  await time.press('Tab');
  await dialog.screenshot({ path: 'test-results/ui-task-input.png', animations: 'disabled' });
  await dialog.getByRole('button', { name: '登録する', exact: true }).click();
  await expect(dialog).toBeHidden();
  const [task] = (await api.tasks()).items;
  expect(tokyoLocalInput(task?.deadline ?? '')).toMatch(/T12:17$/);
  await page.goto('/tasks');
  await page.getByRole('button', { name: '課題を編集', exact: true }).click();
  const edit = page.getByRole('dialog', { name: '課題の内容を調整' });
  await edit.getByRole('radio', { name: '手入力', exact: true }).click();
  const savedTime = edit.getByRole('combobox', { name: '締切時刻', exact: true });
  await expect(savedTime).toHaveValue('12:17');
  await savedTime.fill('24:00');
  await savedTime.press('Tab');
  await expect(edit.getByRole('button', { name: '変更内容を確認', exact: true })).toBeDisabled();
  expect(pageErrors).toEqual([]);
});

test('viewing explanation keeps its header, background and close icon in place', async ({
  page,
}) => {
  await page.setViewportSize({ width: 900, height: 520 });
  await page.goto('/calendar');
  const home = workspace(page);
  await expect(home.summary).toContainText('今日 視聴できる時間');
  await home.creditHelp.click();
  const dialog = page.getByRole('dialog', { name: '視聴時間について' });
  await expect(dialog).toContainText('今日 視聴できる時間');
  const heading = dialog.getByRole('heading', { name: '視聴時間について' });
  const body = dialog.locator('mat-dialog-content');
  // ダイアログ中の背面はaria-hiddenになるため、位置の確認だけはDOMから行う。
  const background = page.locator('.workspace-summary');
  await body.hover();
  const headingTop = (await heading.boundingBox())?.y;
  const backgroundTop = await background.evaluate((element) => element.getBoundingClientRect().top);
  await page.mouse.wheel(0, 300);
  await expect.poll(() => body.evaluate((element) => element.scrollTop)).toBeGreaterThan(0);
  expect((await heading.boundingBox())?.y).toBe(headingTop);
  await body.evaluate((element) => {
    element.scrollTop = element.scrollHeight;
  });
  await page.mouse.wheel(0, 600);
  await expect
    .poll(() => background.evaluate((element) => element.getBoundingClientRect().top))
    .toBe(backgroundTop);
  const close = dialog.getByRole('button', { name: '閉じる', exact: true });
  const centered = await close.evaluate((button) => {
    const icon = button.querySelector('mat-icon');
    if (!icon) return false;
    const bounds = button.getBoundingClientRect();
    const mark = icon.getBoundingClientRect();
    return (
      Math.abs(bounds.x + bounds.width / 2 - mark.x - mark.width / 2) < 1 &&
      Math.abs(bounds.y + bounds.height / 2 - mark.y - mark.height / 2) < 1
    );
  });
  expect(centered).toBe(true);
  await body.evaluate((element) => {
    element.scrollTop = 0;
  });
  await dialog.screenshot({ path: 'test-results/ui-help.png', animations: 'disabled' });
  await close.click();
  await expect(dialog).toBeHidden();
  await expect(page.locator('html')).not.toHaveClass(/cdk-global-scrollblock/);
  await expect(home.creditHelp).toBeFocused();
});

test('structured task details and simplified page headings fit narrow and enlarged layouts', async ({
  page,
  api,
}) => {
  await api.createTask({ title: 'レポートの構成を考える', minutes: 20 });
  await page.goto('/calendar');
  const { summary } = workspace(page);
  await expect(summary.locator('dt')).toHaveText(['開始', '終了', '作業時間']);
  await summary.screenshot({ path: 'test-results/ui-summary.png', animations: 'disabled' });
  for (const width of [390, 320]) {
    await page.setViewportSize({ width, height: 900 });
    await expectNoHorizontalScroll(page);
  }
  for (const [path, title] of [
    ['records', '積み上げの記録'],
    ['about', 'このアプリについて'],
  ] as const) {
    await page.goto('/' + path);
    const header = page.locator('app-page-heading');
    await expect(header.getByRole('heading', { name: title })).toBeVisible();
    await expect(header.locator('p')).toHaveCount(0);
  }
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.goto('/calendar');
  await page.evaluate(() => {
    document.documentElement.style.fontSize = '200%';
  });
  await expectNoHorizontalScroll(page);
  await page.getByRole('button', { name: '課題を登録', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: '課題を登録' });
  expect(await dialog.evaluate((element) => element.scrollWidth <= element.clientWidth)).toBe(true);
});
