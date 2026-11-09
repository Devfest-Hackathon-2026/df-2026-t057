import type { CalendarStatus } from '../support/api';
import { hoursFromNow } from '../support/api';
import { expect, test, tokyoDate } from '../support/fixtures';
import { expectNoHorizontalScroll } from '../support/layout';
import { taskAction, workspace } from '../support/pages';
import { enableAi, fulfillProblem, Gate, mockAssistant } from '../support/routes';

test('calendar selection, routes and chat preserve the chosen task and draft', async ({
  page,
  api,
  pageErrors,
}) => {
  const deadline = Date.now() + 2 * 86_400_000;
  for (const [index, id] of ['calendar-a', 'calendar-b'].entries()) {
    await api.createTask({
      id,
      title: '同じ名前のレポート',
      notes: '架空の確認用課題',
      deadline: new Date(deadline + index * 86_400_000).toISOString(),
    });
  }
  const before = await api.tasks();
  const chosen = before.items.find((task) => task.id === 'calendar-b');
  const balance = (await api.wallet()).balanceSeconds;
  await enableAi(page);
  const gate = new Gate();
  const assistant = await mockAssistant(page, async () => {
    await gate.wait();
    return { reply: '選択した課題の予定を確認できます。', cards: [] };
  });

  await page.goto('/tasks');
  await expect(
    page.getByRole('navigation', { name: 'メインメニュー' }).getByRole('link'),
  ).toHaveCount(4);
  await taskAction(page, 'カレンダーで見る', 1);
  await expect(page).toHaveURL(
    new RegExp(`task=calendar-b.*date=${tokyoDate(chosen?.plan.start ?? '')}`),
  );
  const home = workspace(page);
  await expect(home.detail).toContainText('同じ名前のレポート');
  await expect(page.getByRole('radio', { name: '課題の詳細', exact: true })).toBeChecked();

  await page.getByRole('radio', { name: '日', exact: true }).click();
  await page.reload();
  await expect(page.getByRole('radio', { name: '日', exact: true })).toBeChecked();
  await expect(home.calendar.getByText('同じ名前のレポート')).toBeVisible();

  await page.getByRole('radio', { name: '相談', exact: true }).click();
  await expect(page.getByText('相談する予定')).toBeVisible();
  await expect(page.locator('.selected-context')).toContainText('同じ名前のレポート');
  await home.composer.fill('この予定を見直したい');
  await page.getByRole('radio', { name: '課題の詳細', exact: true }).click();
  await page.getByRole('radio', { name: '相談', exact: true }).click();
  await expect(home.composer).toHaveValue('この予定を見直したい');

  await page.getByRole('button', { name: '送信', exact: true }).click();
  await expect(page.getByRole('button', { name: '相談中…' })).toBeDisabled();
  await expect(home.composer).toHaveValue('この予定を見直したい');
  gate.open();
  await expect(page.getByText('選択した課題の予定を確認できます。')).toBeVisible();
  await expect(home.composer).toHaveValue('');
  expect(assistant.requests.map((request) => request.taskId)).toEqual(['calendar-b']);
  // 相談は何も保存しない。
  expect((await api.tasks()).revision).toBe(before.revision);
  expect((await api.wallet()).balanceSeconds).toBe(balance);

  await page.getByRole('link', { name: '課題', exact: true }).click();
  // 画面の切り替えを待たずに戻ると、1つ前のカレンダーの URL へ戻ってしまう。
  await expect(page).toHaveURL(/\/tasks/);
  await page.goBack();
  await expect(page).toHaveURL(/calendar.*task=calendar-b/);
  await expect(home.detail).toBeVisible();
  await expect(page.getByRole('radio', { name: '課題の詳細', exact: true })).toBeChecked();
  expect(pageErrors).toEqual([]);
});

test('mobile switches between calendar and consultation without losing text', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto('/calendar');
  const home = workspace(page);
  await expect(home.calendar).toBeVisible();
  await expect(page.getByRole('radio', { name: '日', exact: true })).toBeChecked();
  const parts = page.getByRole('radiogroup', { name: '表示する領域' });
  await parts.getByRole('radio', { name: '相談・詳細', exact: true }).click();
  await expect(home.calendar).toBeHidden();
  await home.composer.fill('明日の課題を短く区切りたい');
  await parts.getByRole('radio', { name: 'カレンダー', exact: true }).click();
  await expect(home.calendar).toBeVisible();
  await parts.getByRole('radio', { name: '相談・詳細', exact: true }).click();
  await expect(home.composer).toHaveValue('明日の課題を短く区切りたい');
  await expectNoHorizontalScroll(page);
});

test('manual Google sync waits for confirmation and preserves events on failure', async ({
  page,
  api,
}) => {
  await api.createTask({ title: '同期を確認する課題', deadline: hoursFromNow(2) });
  const connected: CalendarStatus = {
    enabled: true,
    connected: true,
    sync: 'synced',
    failedJobs: 0,
  };
  await page.route('**/api/calendar', (route) => route.fulfill({ json: connected }));
  const gate = new Gate();
  let calls = 0;
  await page.route('**/api/calendar/sync', async (route) => {
    expect(route.request().method()).toBe('POST');
    calls += 1;
    await gate.wait();
    await fulfillProblem(route, 502, 'external.failed');
  });
  await page.goto('/calendar');
  const home = workspace(page);
  await page.getByRole('button', { name: 'Googleと同期', exact: true }).click();
  await expect(page.getByRole('button', { name: '更新中…', exact: true })).toBeDisabled();
  await expect(home.summary).toContainText('同期を確認する課題');
  gate.open();
  await expect(home.calendar.getByRole('alert')).toContainText('同期を確認できませんでした');
  await expect(home.summary).toContainText('同期を確認する課題');
  await expect(home.credit).toHaveText('15分');
  await expect(page.getByRole('button', { name: 'Googleと同期', exact: true })).toBeEnabled();
  expect(calls).toBe(1);
});

for (const colorScheme of ['light', 'dark'] as const) {
  test(`calendar scrolls near now and today's shading preserves grid lines in ${colorScheme}`, async ({
    page,
  }) => {
    await page.clock.setFixedTime(new Date('2026-11-09T18:45:00+09:00'));
    await page.emulateMedia({ colorScheme });
    await page.goto('/calendar?view=week');
    const scroller = page.locator('.calendar-scroll');
    await expect(scroller.locator('[data-time="18:30:00"]').first()).toBeInViewport();
    const alphas = await scroller.locator('[data-date="2026-11-09"]').evaluateAll((elements) =>
      elements.map((element) => {
        const canvas = document.createElement('canvas');
        canvas.width = canvas.height = 1;
        const context = canvas.getContext('2d');
        if (!context) throw new Error('canvas unavailable');
        context.fillStyle = getComputedStyle(element).backgroundColor;
        context.fillRect(0, 0, 1, 1);
        return context.getImageData(0, 0, 1, 1).data[3];
      }),
    );
    expect(alphas.length).toBeGreaterThan(0);
    expect(alphas.every((alpha) => alpha !== undefined && alpha < 255)).toBe(true);
    await page.getByRole('button', { name: '次の期間', exact: true }).click();
    await page.clock.setFixedTime(new Date('2026-11-09T22:45:00+09:00'));
    await page.getByRole('button', { name: '今日', exact: true }).click();
    await expect(scroller.locator('[data-time="22:30:00"]').first()).toBeInViewport();
  });
}
