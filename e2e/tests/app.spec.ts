import { hoursFromNow } from '../support/api';
import { expect, test } from '../support/fixtures';
import { expectNoHorizontalScroll } from '../support/layout';
import { workspace } from '../support/pages';
import { fulfillProblem, Gate } from '../support/routes';

/** `YYYY-MM-DDTHH:mm` に分を足す（タイムゾーンに依らず、文字列の時刻のまま計算する）。 */
function addMinutes(localInput: string, minutes: number): string {
  const shifted = new Date(Date.parse(`${localInput}:00Z`) + minutes * 60_000);
  return shifted.toISOString().slice(0, 16);
}

test('registration, confirmation, timer and completion', async ({ page, pageErrors }) => {
  await page.goto('/calendar');
  const home = workspace(page);
  await expect(home.credit).toHaveText('10分');

  await page.getByRole('button', { name: '課題を登録', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: '課題を登録' });
  await dialog.getByRole('button', { name: '明日', exact: true }).click();
  await dialog.getByLabel('課題名', { exact: true }).fill('演習レポート');
  await dialog.getByLabel('必要時間（分）').fill('30');
  await dialog.getByRole('button', { name: '登録する', exact: true }).click();
  await expect(dialog).toBeHidden();
  await expect(home.credit).toHaveText('15分');

  await home.summary.getByRole('button', { name: '通知を試す' }).click();
  const notice = page.getByRole('dialog', { name: '通知プレビュー' });
  await expect(notice.getByRole('heading', { name: 'そろそろ、やるか。' })).toBeVisible();
  await notice.getByRole('button', { name: '10分スヌーズ・予定を確認' }).click();
  const snooze = page.getByRole('dialog', { name: 'スヌーズ後の予定を確認' });
  await expect(snooze.getByText('保存するまで、現在の予定は変わりません。')).toBeVisible();
  await expect(notice).toBeHidden();
  await snooze.getByRole('button', { name: 'キャンセル', exact: true }).click();
  await expect(snooze).toBeHidden();

  // 次の課題を選ぶと詳細が開き、そこから予定を調整できる。
  await home.summary.getByRole('button', { name: '演習レポート' }).click();
  const detail = page.getByRole('region', { name: '選択した課題' });
  await expect(detail.getByRole('heading', { name: '演習レポート' })).toBeVisible();
  await detail.getByRole('button', { name: '予定・手順を調整', exact: true }).click();
  const adjust = page.getByRole('dialog', { name: '予定を調整' });
  const start = await adjust.getByLabel('開始', { exact: true }).inputValue();
  await adjust.getByLabel('終了', { exact: true }).fill(addMinutes(start, 10));
  await adjust.getByRole('button', { name: 'この予定に変更', exact: true }).click();
  await expect(adjust).toBeHidden();

  await page.getByRole('link', { name: '課題', exact: true }).click();
  const row = page
    .getByRole('list', { name: '課題一覧' })
    .getByRole('listitem')
    .filter({ hasText: '演習レポート' });
  await expect(row).toContainText('予定 10分');
  await expect(row).toContainText('見積もり 30分');
  await row.getByRole('button', { name: 'はじめる', exact: true }).click();
  const timer = page.getByRole('region', { name: '作業タイマー' });
  await expect(timer).toContainText('/ 10分');
  await timer.getByRole('button', { name: '一時停止', exact: true }).click();
  await expect(timer.getByRole('button', { name: '再開', exact: true })).toBeVisible();
  await page.reload();
  await expect(timer.getByRole('button', { name: '再開', exact: true })).toBeVisible();
  await expect(timer).toContainText('一時停止中');
  await timer.getByRole('button', { name: '課題を完了', exact: true }).click();
  await expect(timer).toBeHidden();

  await page.getByRole('link', { name: 'カレンダー', exact: true }).click();
  // 数秒の作業では視聴時間は増えない（5分ごとに1分）。
  await expect(home.credit).toHaveText('15分');
  expect(pageErrors).toEqual([]);
});

test('snooze commit, settings and responsive layout', async ({ page, api }) => {
  await page.goto('/calendar');
  const home = workspace(page);
  await page.getByRole('button', { name: 'サンプル3件で試す', exact: true }).click();
  await expect.poll(async () => (await api.tasks()).items.length).toBe(3);
  for (const title of ['レポートの構成をまとめる', '演習問題を解く', '見直して提出する']) {
    await expect(home.calendar.getByText(title)).toBeVisible();
  }
  await expect(home.credit).toHaveText('25分');

  await home.summary.getByRole('button', { name: '通知を試す' }).click();
  await page.getByRole('button', { name: '10分スヌーズ・予定を確認' }).click();
  const snooze = page.getByRole('dialog', { name: 'スヌーズ後の予定を確認' });
  await expect(snooze.locator('li.plan-change')).toHaveCount(3);
  const before = await api.tasks();
  await snooze.getByRole('button', { name: 'この予定に変更', exact: true }).click();
  await expect(snooze).toBeHidden();
  const after = await api.tasks();
  expect(after.revision).toBeGreaterThan(before.revision);
  const first = before.items.toSorted((a, b) => a.plan.start.localeCompare(b.plan.start))[0];
  const moved = after.items.find((task) => task.id === first?.id);
  expect(Date.parse(moved?.plan.start ?? '')).toBeGreaterThan(Date.parse(first?.plan.start ?? ''));
  // スヌーズは視聴時間を変えない。
  await expect(home.credit).toHaveText('25分');

  await page.getByRole('link', { name: '設定を開く', exact: true }).click();
  await page.getByLabel('分', { exact: true }).fill('15');
  await page.getByRole('button', { name: '変更を保存', exact: true }).click();
  await expect(page.getByText('設定を保存しました', { exact: true })).toBeVisible();
  await page.getByRole('link', { name: 'カレンダー', exact: true }).click();
  await home.summary.getByRole('button', { name: '通知を試す' }).click();
  const notice = page.getByRole('dialog', { name: '通知プレビュー' });
  await expect(notice.getByRole('button', { name: '15分スヌーズ・予定を確認' })).toBeVisible();
  await notice.getByRole('button', { name: 'あとで', exact: true }).click();
  await expect(notice).toBeHidden();

  await page.setViewportSize({ width: 390, height: 844 });
  await expectNoHorizontalScroll(page);
});

test('failed registration preserves input and loading state', async ({ page, api }) => {
  await page.goto('/calendar');
  await page.getByRole('button', { name: '課題を登録', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: '課題を登録' });
  await dialog.getByLabel('課題名', { exact: true }).fill('入力を残す課題');
  const gate = new Gate();
  await page.route('**/api/tasks', async (route) => {
    if (route.request().method() !== 'POST') {
      await route.fallback();
      return;
    }
    await gate.wait();
    await fulfillProblem(route, 503, 'internal');
  });
  await dialog.getByRole('button', { name: '登録する', exact: true }).click();
  await expect(dialog.getByRole('button', { name: '処理中…', exact: true })).toBeDisabled();
  await expect(dialog.getByLabel('課題名', { exact: true })).toBeDisabled();
  gate.open();
  await expect(dialog.getByRole('alert')).toContainText(
    '問題が発生しました。少し待って再試行してください。',
  );
  await expect(dialog.getByLabel('課題名', { exact: true })).toHaveValue('入力を残す課題');
  await expect(dialog.getByRole('button', { name: '登録する', exact: true })).toBeEnabled();
  expect((await api.tasks()).items).toHaveLength(0);
});

test('notification links wait for tasks and snooze opens the confirmation directly', async ({
  page,
  api,
}) => {
  const task = await api.createTask({ title: '通知リンクの課題', deadline: hoursFromNow(2) });
  await page.route('**/api/tasks', async (route) => {
    if (route.request().method() === 'GET') await new Promise((r) => setTimeout(r, 250));
    await route.fallback();
  });
  await page.goto(`/?notification=${task.id}`);
  const notice = page.getByRole('dialog', { name: '通知プレビュー' });
  await expect(notice.getByRole('heading', { name: '通知リンクの課題' })).toBeVisible();
  // 開いたクエリは URL から消える（再読み込みで同じ通知を出さない）。
  await expect(page).not.toHaveURL(/notification=/);

  await page.goto(`/?snooze=${task.id}`);
  await expect(
    page.getByRole('dialog').getByRole('heading', { name: 'スヌーズ後の予定を確認' }),
  ).toBeVisible();
  await expect(notice).toBeHidden();
});
