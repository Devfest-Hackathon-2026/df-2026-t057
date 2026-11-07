import { test, expect } from '@playwright/test';
import { AssistantTurn } from '../../shared/models';

test.use({ timezoneId: 'Asia/Tokyo' });
const day = (value: string) =>
  new Intl.DateTimeFormat('sv-SE', {
    timeZone: 'Asia/Tokyo',
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
  }).format(new Date(value));

test('calendar selection, routes and chat preserve the chosen task and draft', async ({ page }) => {
  const errors: string[] = [];
  page.on('pageerror', (e) => errors.push(e.message));
  await page.request.get('/api/state');
  const deadline = new Date(Date.now() + 2 * 86400000);
  deadline.setUTCHours(15, 45, 0, 0);
  for (const [index, id] of ['calendar-a', 'calendar-b'].entries()) {
    await page.request.post('/api/tasks', {
      data: {
        id,
        title: '同じ名前のレポート',
        notes: '架空の確認用課題',
        minutes: 30,
        deadline: new Date(+deadline + index * 86400000).toISOString(),
        url: '',
      },
    });
  }
  const state = await (await page.request.get('/api/state')).json();
  const chosen = state.tasks.find((t: { id: string }) => t.id === 'calendar-b');
  let history: AssistantTurn[] = [];
  let release!: () => void;
  const wait = new Promise<void>((resolve) => (release = resolve));
  await page.route('**/api/config', async (route) => {
    const r = await route.fetch();
    await route.fulfill({ json: { ...(await r.json()), aiEnabled: true } });
  });
  await page.route('**/api/assistant', (route) => route.fulfill({ json: { turns: history } }));
  await page.route('**/api/assistant/messages', async (route) => {
    const input = route.request().postDataJSON();
    expect(input.taskId).toBe('calendar-b');
    await wait;
    const turn: AssistantTurn = {
      ...input,
      status: 'ready',
      createdAt: new Date().toISOString(),
      reply: '選択した課題の予定を確認できます。',
      cards: [{ id: 'selected-card', kind: 'task', taskId: input.taskId }],
    };
    history = [turn];
    await route.fulfill({ json: turn });
  });
  await page.goto('/tasks');
  await expect(
    page.getByRole('navigation', { name: 'メインメニュー' }).getByRole('link'),
  ).toHaveCount(3);
  await page.locator('.task-row').nth(1).getByRole('link', { name: 'カレンダーで見る' }).click();
  await expect(page).toHaveURL(new RegExp(`task=calendar-b.*date=${day(chosen.plan.start)}`));
  await expect(page.getByRole('region', { name: '選択した課題' })).toContainText(
    '同じ名前のレポート',
  );
  await page.getByRole('radio', { name: '日', exact: true }).click();
  await page.reload();
  await expect(page.getByRole('radio', { name: '日', exact: true })).toBeChecked();
  await page.screenshot({
    path: 'test-results/calendar-desktop-detail.png',
    fullPage: true,
    animations: 'disabled',
  });
  await expect(
    page.locator('.calendar-surface .event-name').filter({ hasText: '同じ名前のレポート' }),
  ).toBeVisible();
  await page.getByRole('radio', { name: '相談', exact: true }).click();
  await expect(page.locator('.selected-context')).toContainText('同じ名前のレポート');
  const composer = page.getByLabel('相談する', { exact: true });
  await composer.fill('この予定を見直したい');
  await page.getByRole('radio', { name: '課題の詳細', exact: true }).click();
  await page.getByRole('radio', { name: '相談', exact: true }).click();
  await expect(composer).toHaveValue('この予定を見直したい');
  await page.screenshot({
    path: 'test-results/calendar-desktop-consultation.png',
    fullPage: true,
    animations: 'disabled',
  });
  await page.getByRole('button', { name: '送信', exact: true }).click();
  await expect(page.getByRole('button', { name: '相談中…' })).toBeDisabled();
  release();
  await expect(page.getByText('選択した課題の予定を確認できます。')).toBeVisible();
  const after = await (await page.request.get('/api/state')).json();
  expect(after.planRevision).toBe(state.planRevision);
  expect(after.balanceSeconds).toBe(state.balanceSeconds);
  await page.getByRole('link', { name: '課題', exact: true }).click();
  await page.goBack();
  await expect(page).toHaveURL(/calendar.*task=calendar-b/);
  await expect(page.getByRole('radio', { name: '課題の詳細', exact: true })).toBeChecked();
  expect(errors).toEqual([]);
});

test('mobile switches between calendar and consultation without losing text', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto('/calendar');
  await expect(page.getByRole('region', { name: '予定カレンダー' })).toBeVisible();
  await expect(page.getByRole('radio', { name: '日', exact: true })).toBeChecked();
  await page.screenshot({
    path: 'test-results/calendar-mobile.png',
    fullPage: true,
    animations: 'disabled',
  });
  await page.getByRole('radio', { name: '相談・詳細', exact: true }).click();
  const composer = page.getByLabel('相談する', { exact: true });
  await composer.fill('明日の課題を短く区切りたい');
  await page
    .getByRole('radiogroup', { name: '表示する領域' })
    .getByRole('radio', { name: 'カレンダー', exact: true })
    .click();
  await expect(page.getByRole('region', { name: '予定カレンダー' })).toBeVisible();
  await page.getByRole('radio', { name: '相談・詳細', exact: true }).click();
  await expect(composer).toHaveValue('明日の課題を短く区切りたい');
  await page.screenshot({
    path: 'test-results/calendar-mobile-consultation.png',
    fullPage: true,
    animations: 'disabled',
  });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});

test('manual Google sync waits for confirmation and preserves events on failure', async ({
  page,
}) => {
  await page.request.get('/api/state');
  await page.request.post('/api/tasks', {
    data: {
      id: 'sync-task',
      title: '同期を確認する課題',
      notes: '',
      minutes: 30,
      deadline: new Date(Date.now() + 7200000).toISOString(),
      url: '',
    },
  });
  await page.route('**/api/state', async (route) => {
    const response = await route.fetch();
    await route.fulfill({ json: { ...(await response.json()), calendarConnected: true } });
  });
  let release!: () => void;
  const wait = new Promise<void>((resolve) => (release = resolve));
  let calls = 0;
  await page.route('**/api/calendar/sync', async (route) => {
    expect(route.request().method()).toBe('POST');
    calls++;
    await wait;
    await route.fulfill({
      status: 503,
      json: { error: '同期を確認できませんでした。再試行してください。' },
    });
  });
  await page.goto('/calendar');
  await page.getByRole('button', { name: 'Googleと同期', exact: true }).click();
  await expect(page.getByRole('button', { name: '更新中…', exact: true })).toBeDisabled();
  await expect(page.locator('.next-task')).toContainText('同期を確認する課題');
  release();
  await expect(page.getByRole('alert')).toContainText('同期を確認できませんでした');
  await expect(page.locator('.next-task')).toContainText('同期を確認する課題');
  await expect(page.locator('.credit-number')).toContainText('10');
  expect(calls).toBe(1);
});
