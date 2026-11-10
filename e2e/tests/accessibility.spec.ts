import type { Config } from '../support/api';
import { expectAccessibleInBothSchemes } from '../support/a11y';
import { hoursFromNow } from '../support/api';
import { expect, test, tokyoDate } from '../support/fixtures';
import { expectReflow } from '../support/layout';
import { expandPanel, taskAction, workspace } from '../support/pages';

// 主な画面と、開いたダイアログを WCAG 2.2 AA で検査する（ライト・ダーク）。
// 画面は 320px 幅（400% 拡大と同じ）と 720px 幅（200% 拡大と同じ）でも横スクロールを出さない。

test('calendar before the first task', { tag: '@detail' }, async ({ page }) => {
  await page.goto('/calendar');
  await expect(page.getByRole('heading', { name: '予定はまだありません' })).toBeVisible();
  await expectAccessibleInBothSchemes(page);
  await expectReflow(page);
});

test.describe('main screens', () => {
  test.beforeEach(async ({ api }) => {
    await api.createTask({
      title: '検査用のレポート',
      notes: '構成を考える',
      deadline: hoursFromNow(3),
    });
  });

  test('calendar', async ({ page, api }) => {
    const [task] = (await api.tasks()).items;
    // 夜に実行すると3時間後の作業は翌日になるため、作成した予定の日を開く。
    await page.goto(`/calendar?date=${tokyoDate(task?.plan.start ?? '')}`);
    // 日をまたぐ予定は週表示で2日に分かれて出るので、最初の1つを見る。
    await expect(workspace(page).calendar.getByText('検査用のレポート').first()).toBeVisible();
    await expectAccessibleInBothSchemes(page);
    await expectReflow(page);
  });

  test('calendar with a selected task', async ({ page, api }) => {
    const [task] = (await api.tasks()).items;
    await page.goto(`/calendar?task=${task?.id ?? ''}`);
    await expect(
      workspace(page).detail.getByRole('heading', { name: '検査用のレポート' }),
    ).toBeVisible();
    await expectAccessibleInBothSchemes(page);
  });

  test('tasks', async ({ page }) => {
    await page.goto('/tasks');
    await expect(page.getByRole('heading', { name: '検査用のレポート' })).toBeVisible();
    await expectAccessibleInBothSchemes(page);
    await expectReflow(page);
  });

  test('records', async ({ page }) => {
    await page.goto('/records');
    await expect(page.getByRole('group', { name: '日別の完了記録' })).toBeVisible();
    await page
      .getByRole('region', { name: '視聴と作業の振り返り' })
      .getByText('詳しい記録', { exact: true })
      .click();
    await expectAccessibleInBothSchemes(page);
    await expectReflow(page);
  });

  test('settings', async ({ page }) => {
    await page.goto('/settings');
    for (const name of [
      '視聴ルール',
      '夜の区切り',
      '通知の演出',
      '作業の傾向',
      'Google連携',
      'iPhoneのショートカット',
    ]) {
      await expandPanel(page, name);
    }
    await expect(page.getByText('現在の環境ではGoogle連携は未設定です。')).toBeVisible();
    await expectAccessibleInBothSchemes(page);
    await expectReflow(page);
  });

  test('about', async ({ page }) => {
    await page.goto('/about');
    await expect(page.getByRole('heading', { level: 1, name: 'このアプリについて' })).toBeVisible();
    await expectAccessibleInBothSchemes(page);
    await expectReflow(page);
  });

  test('work timer', async ({ page }) => {
    await page.goto('/tasks');
    await page.getByRole('button', { name: 'はじめる', exact: true }).click();
    await expect(page.getByRole('region', { name: '作業タイマー' })).toBeVisible();
    await expectAccessibleInBothSchemes(page);
    await expectReflow(page);
  });
});

test('login', async ({ page }) => {
  const config: Config = { mode: 'live', features: { ai: false, calendar: false } };
  await page.route('**/api/config', (route) => route.fulfill({ json: config }));
  await page.goto('/');
  await expect(page.getByRole('button', { name: 'Googleでログイン', exact: true })).toBeEnabled();
  await expectAccessibleInBothSchemes(page);
  await expectReflow(page);
});

test.describe('dialogs', () => {
  test('task registration dialog', async ({ page }) => {
    await page.goto('/calendar');
    await page.getByRole('button', { name: '課題を登録', exact: true }).click();
    const dialog = page.getByRole('dialog', { name: '課題を登録' });
    await expect(dialog).toBeVisible();
    await dialog.getByText('詳細を追加').click();
    await expectAccessibleInBothSchemes(page);
    await expectReflow(page);
    // 入力の誤りも読み上げられる形で出す。
    await dialog.getByLabel('課題名', { exact: true }).fill('x');
    await dialog.getByLabel('課題名', { exact: true }).fill('');
    await dialog.getByLabel('課題名', { exact: true }).blur();
    await expect(dialog.getByText('課題名を入力してください。')).toBeVisible();
    await expectAccessibleInBothSchemes(page);
  });

  test('task edit dialog', async ({ page, api }) => {
    await api.createTask({ title: '編集を検査する課題', deadline: hoursFromNow(3) });
    await page.goto('/tasks');
    await taskAction(page, '課題を編集');
    const dialog = page.getByRole('dialog', { name: '課題の内容を調整' });
    await dialog.getByRole('radio', { name: '手入力', exact: true }).click();
    await dialog.getByLabel('課題名', { exact: true }).fill('編集後の課題');
    await dialog.getByRole('button', { name: '変更内容を確認', exact: true }).click();
    await expect(dialog.getByRole('heading', { name: '課題の変更案' })).toBeVisible();
    await expectAccessibleInBothSchemes(page);
  });

  test('plan dialog', async ({ page, api }) => {
    await api.createTask({ title: '予定を検査する課題', deadline: hoursFromNow(3) });
    await page.goto('/tasks');
    await taskAction(page, '予定を調整');
    const dialog = page.getByRole('dialog', { name: '予定を調整' });
    await expect(dialog.getByLabel('開始', { exact: true })).toBeVisible();
    await dialog.getByText(/手順（\d+件）/).click();
    await expectAccessibleInBothSchemes(page);
    await expectReflow(page);
  });

  test('credit information', async ({ page }) => {
    await page.goto('/calendar');
    await workspace(page).creditHelp.click();
    const dialog = page.getByRole('dialog', { name: '視聴時間について' });
    await expect(dialog).toBeVisible();
    await dialog.getByText('夜の報酬・支援の段階').click();
    await expectAccessibleInBothSchemes(page);
    await expectReflow(page);
  });

  test('notification preview', async ({ page, api }) => {
    await api.createTask({
      title: '通知を検査する課題',
      why: '提出のため',
      deadline: hoursFromNow(3),
    });
    await page.goto('/calendar');
    await workspace(page).summary.getByRole('button', { name: '通知を試す' }).click();
    const notice = page.getByRole('dialog', { name: '通知プレビュー' });
    await expect(notice.getByRole('heading', { name: 'そろそろ、やるか。' })).toBeVisible();
    await expect(notice.getByText('なぜ：提出のため')).toBeVisible();
    await expectAccessibleInBothSchemes(page);
    await expectReflow(page);
  });

  test('tutorial', async ({ page }) => {
    await page.goto('/calendar?tutorial');
    await page.getByRole('button', { name: 'チュートリアルを開始', exact: true }).click();
    await expect(page.getByRole('dialog', { name: 'Googleカレンダーを連携' })).toBeVisible();
    await expectAccessibleInBothSchemes(page);
  });
});

test('mobile menu', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto('/calendar');
  await page.getByRole('button', { name: 'メニューを開く', exact: true }).click();
  await expect(page.getByRole('menu', { name: 'メインメニュー' })).toBeVisible();
  await expectAccessibleInBothSchemes(page);
});
