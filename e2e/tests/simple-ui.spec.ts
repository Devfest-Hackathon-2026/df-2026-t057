import { hoursFromNow } from '../support/api';
import { expect, test, tokyoDate } from '../support/fixtures';
import { expectNoHorizontalScroll } from '../support/layout';
import { workspace } from '../support/pages';

test('collapsed task details preserve drafts and are saved only on confirmation', async ({
  page,
  api,
}) => {
  await page.goto('/calendar');
  const home = workspace(page);
  await home.creditHelp.focus();
  await page.keyboard.press('Enter');
  const explanation = page.getByRole('dialog', { name: '視聴時間について' });
  await expect(explanation).toContainText('作業を記録すると、5分につき視聴1分');
  await page.keyboard.press('Escape');
  await expect(explanation).toBeHidden();
  await expect(home.creditHelp).toBeFocused();

  await page.getByRole('button', { name: '課題を登録', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: '課題を登録' });
  const details = dialog.locator('details').filter({ hasText: '詳細を追加' });
  const summary = details.locator('summary');
  await dialog.getByLabel('課題名', { exact: true }).fill('詳細を折りたたんで登録');
  await expect(dialog.getByLabel('やること・メモ')).toBeHidden();
  await summary.focus();
  await page.keyboard.press('Enter');
  await dialog.getByLabel('やること・メモ').fill('構成から書き始める');
  await dialog.getByLabel('目的（任意）').fill('発表に備える');
  await summary.click();
  await expect(dialog.getByLabel('やること・メモ')).toBeHidden();
  await dialog.getByRole('radio', { name: 'AIに相談', exact: true }).click();
  await dialog.getByRole('radio', { name: '手入力', exact: true }).click();
  await summary.click();
  await expect(dialog.getByLabel('やること・メモ')).toHaveValue('構成から書き始める');
  await expect(dialog.getByLabel('目的（任意）')).toHaveValue('発表に備える');

  // 「登録する」を押すまで何も保存しない。
  expect((await api.tasks()).items).toHaveLength(0);
  expect((await api.wallet()).balanceSeconds).toBe(600);
  await dialog.getByRole('button', { name: '登録する', exact: true }).click();
  await expect(dialog).toBeHidden();
  const [task] = (await api.tasks()).items;
  expect(task?.notes).toBe('構成から書き始める');
  expect(task?.why).toBe('発表に備える');
  expect((await api.wallet()).balanceSeconds).toBe(900);
});

test('calendar adapts to mobile while preserving an explicit view choice', async ({
  page,
  api,
}) => {
  const task = await api.createTask({ title: 'レポートの構成を考える', deadline: hoursFromNow(1) });
  // 予定は締切から逆算するので、深夜には開始が翌日になる。その日を開く。
  await page.goto(`/calendar?date=${tokyoDate(task.plan.start)}`);
  const home = workspace(page);
  await expect(page.getByRole('heading', { name: '何から始めましょう？' })).toBeVisible();
  await page.getByRole('button', { name: '予定を見直す', exact: true }).click();
  await expect(home.composer).toHaveValue('予定を見直す');
  await expect(page.getByRole('radio', { name: '週', exact: true })).toBeChecked();

  await page.setViewportSize({ width: 390, height: 844 });
  await expect(page.getByRole('radio', { name: '日', exact: true })).toBeChecked();
  await expect(home.calendar.getByText('レポートの構成を考える')).toBeVisible();
  await expectNoHorizontalScroll(page);

  // 自分で選んだ表示期間は、幅が変わっても保つ。
  await page.getByRole('radio', { name: '週', exact: true }).click();
  await expect(page).toHaveURL(/view=week/);
  await page.setViewportSize({ width: 1440, height: 1080 });
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(page.getByRole('radio', { name: '週', exact: true })).toBeChecked();

  // スマホの幅ではページの移動はハンバーガーメニューから
  await page.getByRole('button', { name: 'メニューを開く', exact: true }).click();
  await page.getByRole('menuitem', { name: '課題', exact: true }).click();
  await page.evaluate(() => document.fonts.ready.then(() => undefined));
  const edit = page.getByRole('button', { name: '課題を編集', exact: true });
  const icon = await edit.locator('mat-icon').boundingBox();
  expect(icon?.width ?? 0).toBeLessThanOrEqual(24);
  // 押せる大きさは 24×24 以上（WCAG 2.5.8）。
  const target = await edit.boundingBox();
  expect(target?.width ?? 0).toBeGreaterThanOrEqual(24);
  expect(target?.height ?? 0).toBeGreaterThanOrEqual(24);
  await expectNoHorizontalScroll(page);

  await page.getByRole('link', { name: '設定を開く', exact: true }).click();
  await expect(page.getByRole('heading', { level: 1, name: '設定' })).toBeVisible();
  await expectNoHorizontalScroll(page);
});
