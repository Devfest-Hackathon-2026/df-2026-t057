import { test, expect } from '@playwright/test';

test.use({ timezoneId: 'Asia/Tokyo' });

test('compact settings preserve changes on failure and retry only the unsaved section', async ({
  page,
}) => {
  await page.goto('/settings');
  const save = page.getByRole('button', { name: '変更を保存', exact: true });
  await expect(save).toBeDisabled();
  const minutes = page.getByLabel('分', { exact: true });
  await minutes.fill('0');
  await minutes.blur();
  await expect(save).toBeDisabled();
  await expect(page.getByText('1〜120分で入力してください')).toBeVisible();
  await minutes.fill('17');
  await page.getByRole('button', { name: /通知の演出/ }).click();
  await page.locator('.mode-card.simple').click();
  await page.getByRole('button', { name: /作業の傾向/ }).click();
  await page.getByLabel('表示名', { exact: true }).fill('架空の利用者');
  await page.getByLabel('得意・苦手、作業の進め方').fill('最初に構成を考えたい。');
  await page.getByRole('button', { name: /作業の傾向/ }).click();
  let settingsCalls = 0;
  let profileCalls = 0;
  let release!: () => void;
  const pending = new Promise<void>((resolve) => (release = resolve));
  await page.route('**/api/settings', async (route) => {
    settingsCalls++;
    await pending;
    await route.continue();
  });
  await page.route('**/api/profile', async (route) => {
    profileCalls++;
    if (profileCalls === 1)
      await route.fulfill({ status: 503, json: { error: '作業の傾向を保存できませんでした。' } });
    else await route.continue();
  });
  await save.click();
  await expect(page.getByRole('button', { name: '保存中…' })).toBeDisabled();
  await expect(minutes).toBeDisabled();
  await expect(page.getByText('設定を保存しました', { exact: true })).not.toBeVisible();
  release();
  await expect(page.getByRole('alert')).toContainText('作業の傾向を保存できませんでした。');
  await expect(page.getByText('設定を保存しました', { exact: true })).not.toBeVisible();
  await expect(minutes).toHaveValue('17');
  await page.getByRole('button', { name: /作業の傾向/ }).click();
  await expect(page.getByLabel('表示名', { exact: true })).toHaveValue('架空の利用者');
  await save.click();
  await expect(page.getByText('設定を保存しました', { exact: true })).toBeVisible();
  await expect(save).toBeDisabled();
  expect(settingsCalls).toBe(1);
  expect(profileCalls).toBe(2);
  await page.reload();
  await expect(minutes).toHaveValue('17');
  await expect(page.getByRole('button', { name: /通知の演出 シンプル/ })).toBeVisible();
  await page.getByRole('button', { name: /作業の傾向/ }).click();
  await expect(page.getByLabel('表示名', { exact: true })).toHaveValue('架空の利用者');
  await expect(page.getByLabel('得意・苦手、作業の進め方')).toHaveValue('最初に構成を考えたい。');
});

test('completed tasks and actual rewards appear once in the activity calendar', async ({
  page,
}) => {
  await page.goto('/records');
  await expect(page.locator('.records-count')).toHaveText(/0\s*件/);
  await expect(page.getByText('まだ完了した課題はありません。', { exact: false })).toBeVisible();
  for (const id of ['record-a', 'record-b']) {
    const added = await page.request.post('/api/tasks', {
      data: {
        id,
        title: `確認用課題 ${id}`,
        notes: '',
        minutes: 10,
        deadline: new Date(Date.now() + 86400000).toISOString(),
        url: '',
      },
    });
    expect(added.ok()).toBe(true);
    expect((await page.request.post(`/api/tasks/${id}/complete`)).ok()).toBe(true);
  }
  await page.request.post('/api/tasks/record-a/complete');
  await page.reload();
  await expect(page.locator('.records-count')).toHaveText(/2\s*件/);
  await expect(page.locator('.completion-reward strong')).toContainText('＋0秒');
  await expect(page.locator('.records-grass button')).toHaveCount(84);
  await expect(page.locator('.records-grass button.multiple')).toHaveCount(1);
  await expect(page.locator('.selected-activity')).toContainText('2件完了 · 視聴時間 ＋0秒');
  await expect(page.locator('.records-ledger-row').filter({ hasText: '課題を登録' })).toHaveCount(
    2,
  );
  await expect(page.locator('.records-ledger-row').first()).toContainText('確認用課題 record-b');
  await page.locator('.records-grass button').first().click();
  await expect(page.locator('.selected-activity')).toContainText('0件完了 · 視聴時間 ＋0秒');
  await page.screenshot({ path: 'test-results/records-accumulation-desktop.png', fullPage: true });
});

test('chosen layouts fit desktop and mobile with expanded settings', async ({ page }) => {
  const errors: string[] = [];
  page.on('pageerror', (error) => errors.push(error.message));
  for (const width of [1440, 390, 320]) {
    await page.setViewportSize({ width, height: 1000 });
    await page.goto('/settings');
    await expect(page.getByRole('heading', { name: '設定', exact: false })).toBeVisible();
    await page.screenshot({ path: `test-results/settings-compact-${width}.png`, fullPage: true });
    for (const name of [/^視聴ルール /, /^夜の区切り /, /通知の演出/, /作業の傾向/, /Google連携/])
      await page.getByRole('button', { name }).first().click();
    await expect(page.getByText('現在の環境ではGoogle連携は未設定です。')).toBeVisible();
    await expect(
      page.getByRole('button', { name: 'Googleカレンダーを連携', exact: true }),
    ).toBeDisabled();
    await expect(page.getByRole('button', { name: /Google連携 未連携/ })).toBeVisible();
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(
      true,
    );
    await page.screenshot({ path: `test-results/settings-expanded-${width}.png`, fullPage: true });
    await page.goto('/records');
    await expect(page.locator('.records-grass button')).toHaveCount(84);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(
      true,
    );
    await page.screenshot({
      path: `test-results/records-accumulation-${width}.png`,
      fullPage: true,
    });
  }
  expect(errors).toEqual([]);
});

test('current work rewards keep seconds, deferred status and the self-management records', async ({
  page,
}) => {
  await page.route('**/api/state', async (route) => {
    const response = await route.fetch();
    const state = await response.json();
    state.ledger.push({
      id: 'work:fixture',
      kind: 'work',
      seconds: 75,
      workSeconds: 375,
      deferred: true,
      at: new Date().toISOString(),
    });
    state.ledger.push({
      id: 'sleep:fixture',
      kind: 'sleep',
      seconds: 300,
      at: new Date().toISOString(),
    });
    await route.fulfill({ response, json: state });
  });
  await page.goto('/records');
  await expect(page.locator('.completion-reward strong')).toContainText('＋1分15秒');
  const work = page.locator('.records-ledger-row').filter({ hasText: '朝に受け取り' });
  await expect(work).toContainText('作業 6分15秒');
  await expect(work.locator('.records-credit')).toHaveText('＋1分15秒');
  await expect(page.locator('.records-ledger-row').filter({ hasText: '睡眠の報酬' })).toContainText(
    '＋5分',
  );
  await page.locator('.records-practice > summary').click();
  await expect(page.getByText('予定から5分以内に開始', { exact: false })).toBeVisible();
});

test('sleep settings remain editable from the compact list', async ({ page }) => {
  await page.goto('/settings');
  await page.locator('mat-expansion-panel-header').filter({ hasText: '夜の区切り' }).click();
  await page.getByLabel('就寝', { exact: true }).fill('23:00');
  await page.getByLabel('起床', { exact: true }).fill('06:30');
  await page.getByRole('button', { name: '就寝の予定を保存', exact: true }).click();
  await expect(page.getByText('就寝の予定を保存しました', { exact: true })).toBeVisible();
  await page.reload();
  await page.locator('mat-expansion-panel-header').filter({ hasText: '夜の区切り' }).click();
  await expect(page.getByLabel('就寝', { exact: true })).toHaveValue('23:00');
  await expect(page.getByLabel('起床', { exact: true })).toHaveValue('06:30');
  await page.getByRole('button', { name: '外す', exact: true }).click();
  await expect(page.getByText('就寝の予定を外しました', { exact: true })).toBeVisible();
});
