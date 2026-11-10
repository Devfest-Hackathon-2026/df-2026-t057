import type { LedgerEntry, Records } from '../support/api';
import { hoursFromNow } from '../support/api';
import { expect, test } from '../support/fixtures';
import { expectAccessibleInBothSchemes } from '../support/a11y';
import { expectNoHorizontalScroll } from '../support/layout';
import { expandPanel } from '../support/pages';
import { fulfillProblem, Gate } from '../support/routes';

test('compact settings preserve changes on failure and retry only the unsaved section', async ({
  page,
  api,
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
  await expect(save).toBeEnabled();
  await expect(page.getByText('変更内容はまだ保存されていません')).toBeVisible();
  await expandPanel(page, '通知の演出');
  await page.getByRole('radio', { name: /シンプル/ }).click();
  await expandPanel(page, '作業の傾向');
  await page.getByLabel('表示名', { exact: true }).fill('架空の利用者');
  await page.getByLabel('得意・苦手、作業の進め方').fill('最初に構成を考えたい。');

  let settingsCalls = 0;
  let profileCalls = 0;
  const gate = new Gate();
  await page.route('**/api/settings', async (route) => {
    if (route.request().method() !== 'PUT') {
      await route.fallback();
      return;
    }
    settingsCalls += 1;
    await gate.wait();
    await route.fallback();
  });
  await page.route('**/api/profile', async (route) => {
    if (route.request().method() !== 'PUT') {
      await route.fallback();
      return;
    }
    profileCalls += 1;
    if (profileCalls === 1) await fulfillProblem(route, 503, 'internal');
    else await route.fallback();
  });
  await save.click();
  await expect(page.getByRole('button', { name: '保存中…', exact: true })).toBeDisabled();
  await expect(page.getByText('変更を保存しています…')).toBeVisible();
  gate.open();
  await expect(page.getByRole('alert')).toContainText('作業の傾向を保存できませんでした。');
  await expect(page.getByText('設定を保存しました', { exact: true })).toHaveCount(0);
  await expect(minutes).toHaveValue('17');
  await expect(page.getByLabel('表示名', { exact: true })).toHaveValue('架空の利用者');
  expect((await api.settings()).snoozeMinutes).toBe(17);
  expect((await api.profile()).name).toBe('');

  await save.click();
  await expect(page.getByText('設定を保存しました', { exact: true })).toBeVisible();
  await expect(save).toBeDisabled();
  // 保存できていた設定は送り直さない。
  expect(settingsCalls).toBe(1);
  expect(profileCalls).toBe(2);

  await page.reload();
  await expect(minutes).toHaveValue('17');
  await expect(
    page
      .getByRole('button', { name: /通知の演出\s*シンプル/ })
      .and(page.locator('mat-expansion-panel-header')),
  ).toBeVisible();
  await expandPanel(page, '作業の傾向');
  await expect(page.getByLabel('表示名', { exact: true })).toHaveValue('架空の利用者');
  await expect(page.getByLabel('得意・苦手、作業の進め方')).toHaveValue('最初に構成を考えたい。');
});

test('completed tasks and actual rewards appear once in the activity calendar', async ({
  page,
  api,
}) => {
  await page.goto('/records');
  const activity = page.getByRole('region', { name: '小さな一歩のカレンダー' });
  await expect(activity).toContainText(/過去12週間に完了した課題\s*0\s*件/);
  await expect(page.getByText('まだ完了した課題はありません。', { exact: false })).toBeVisible();
  for (const id of ['record-a', 'record-b']) {
    await api.createTask({
      id,
      title: `確認用課題 ${id}`,
      minutes: 10,
      deadline: hoursFromNow(24),
    });
    await api.completeTask(id);
  }
  // 完了済みの課題を完了し直しても、記録は増えない。
  await api.completeTask('record-a');
  await page.reload();
  await expect(activity).toContainText(/過去12週間に完了した課題\s*2\s*件/);
  await expect(activity).toContainText(/作業で得た視聴時間\s*＋0秒/);
  const days = page.getByRole('group', { name: '日別の完了記録' }).getByRole('button');
  await expect(days).toHaveCount(84);
  const grass = page.getByRole('group', { name: '日別の完了記録' });
  await expect(grass.getByRole('button', { name: /: 2件完了、/ })).toHaveCount(1);
  await expect(grass.getByRole('button', { name: /: [1-9]\d*件完了、/ })).toHaveCount(1);
  await expect(days.last()).toHaveAttribute('aria-pressed', 'true');
  await expect(activity).toContainText('2件完了 · 視聴時間 ＋0秒');

  const history = page.getByRole('region', { name: '最近の積み上げ' });
  const registrations = history.getByRole('listitem').filter({ hasText: '課題を登録' });
  await expect(registrations).toHaveCount(2);
  await expect(history.getByRole('listitem').first()).toContainText('確認用課題 record-b');
  await expect(history.getByRole('listitem').filter({ hasText: 'はじめの視聴時間' })).toContainText(
    '＋10分',
  );

  await days.first().click();
  await expect(activity).toContainText('0件完了 · 視聴時間 ＋0秒');
  await expect(days.first()).toHaveAttribute('aria-pressed', 'true');
  // 矢印キーで日を移動できる（右は1週間後）。
  await days.first().press('ArrowRight');
  await expect(days.nth(7)).toBeFocused();
});

test('chosen layouts fit desktop and mobile with expanded settings', async ({
  page,
  pageErrors,
}) => {
  for (const width of [1440, 390, 320]) {
    await page.setViewportSize({ width, height: 1000 });
    await page.goto('/settings');
    await expect(page.getByRole('heading', { level: 1, name: '設定' })).toBeVisible();
    for (const name of ['視聴ルール', '夜の区切り', '通知の演出', '作業の傾向', 'Google連携']) {
      await expandPanel(page, name);
    }
    await expect(page.getByText('現在の環境ではGoogle連携は未設定です。')).toBeVisible();
    await expect(
      page.getByRole('button', { name: 'Googleカレンダーを連携', exact: true }),
    ).toBeDisabled();
    await expect(
      page
        .getByRole('button', { name: /Google連携\s*未連携/ })
        .and(page.locator('mat-expansion-panel-header')),
    ).toBeVisible();
    await expectNoHorizontalScroll(page);

    await page.goto('/records');
    await expect(
      page.getByRole('group', { name: '日別の完了記録' }).getByRole('button'),
    ).toHaveCount(84);
    await expectNoHorizontalScroll(page);
  }
  expect(pageErrors).toEqual([]);
});

test('work rewards keep seconds, deferred status and the self-management records', async ({
  page,
}) => {
  const at = new Date().toISOString();
  const fixtures: LedgerEntry[] = [
    {
      id: 'work-fixture',
      kind: 'work',
      seconds: 75,
      workSeconds: 375,
      deferred: true,
      at,
      taskId: 'fixture-task',
      taskTitle: '夜に進めた課題',
    },
    { id: 'sleep-fixture', kind: 'sleep', seconds: 300, deferred: false, at },
  ];
  await page.route('**/api/wallet/ledger**', async (route) => {
    const response = await route.fetch();
    const body = (await response.json()) as { entries: LedgerEntry[] };
    await route.fulfill({ response, json: { ...body, entries: [...fixtures, ...body.entries] } });
  });
  await page.goto('/records');
  const history = page.getByRole('region', { name: '最近の積み上げ' });
  const work = history.getByRole('listitem').filter({ hasText: '朝に受け取り' });
  await expect(work).toContainText('夜に進めた課題');
  await expect(work).toContainText('作業 6分15秒');
  await expect(work).toContainText('＋1分15秒');
  await expect(history.getByRole('listitem').filter({ hasText: '睡眠の報酬' })).toContainText(
    '＋5分',
  );
  const review = page.getByRole('region', { name: '視聴と作業の振り返り' });
  await expect(review).toBeVisible();
  await expect(review.getByText('過去7日間に開始した課題はありません。')).toBeVisible();
  await review.getByText('詳しい記録', { exact: true }).click();
  await expect(
    review.getByText('動画の内容は保存しません。記録のない日は数えません。'),
  ).toBeVisible();
});

test('sleep settings remain editable from the compact list', async ({ page, api }) => {
  await page.goto('/settings');
  await expandPanel(page, '夜の区切り');
  await page.getByLabel('就寝', { exact: true }).fill('23:00');
  await page.getByLabel('起床', { exact: true }).fill('06:30');
  await page.getByRole('button', { name: '就寝の予定を保存', exact: true }).click();
  await expect(page.getByText('就寝の予定を保存しました', { exact: true })).toBeVisible();
  expect((await api.sleep()).sleep).toMatchObject({ bedtime: '23:00', wake: '06:30' });
  await page.reload();
  await expandPanel(page, '夜の区切り');
  await expect(page.getByLabel('就寝', { exact: true })).toHaveValue('23:00');
  await expect(page.getByLabel('起床', { exact: true })).toHaveValue('06:30');
  await page.getByRole('button', { name: '外す', exact: true }).click();
  await expect(page.getByText('就寝の予定を外しました', { exact: true })).toBeVisible();
  expect((await api.sleep()).sleep).toBeNull();
});

test('reflection highlights logged work and weekly starts with details available on demand', async ({
  page,
}) => {
  await page.route('**/api/records', async (route) => {
    const response = await route.fetch();
    const body = (await response.json()) as Records;
    await route.fulfill({
      response,
      json: {
        ...body,
        practice: {
          ...body.practice,
          viewingSessions: 3,
          endedSessions: 2,
          extensions: 1,
          startedTasks: 5,
          onTimeStarts: 3,
          missedStarts: 1,
        },
        weeks: [
          { start: '2026-10-19', starts: 0, onTime: 0, snoozes: 0, workSeconds: 0 },
          { start: '2026-10-26', starts: 5, onTime: 3, snoozes: 2, workSeconds: 3000 },
          { start: '2026-11-02', starts: 2, onTime: 1, snoozes: 0, workSeconds: 1500 },
        ],
      },
    });
  });
  await page.route('**/api/wallet', async (route) => {
    const response = await route.fetch();
    const body = (await response.json()) as { policy: object };
    await route.fulfill({
      response,
      json: { ...body, policy: { ...body.policy, workSeconds: 1500, viewedSeconds: 600 } },
    });
  });
  await page.goto('/records');
  const review = page.getByRole('region', { name: '視聴と作業の振り返り' });
  const weekly = page.getByRole('region', { name: '週ごとの振り返り' });
  await expect(review).toContainText('25分');
  await expect(review).toContainText('10分');
  await expect(review).toContainText('3／5件');
  await expect(review.getByText('区切りで選んだ延長 1回')).not.toBeVisible();
  await expect(weekly.getByRole('article')).toHaveCount(2);
  await expect(weekly.getByRole('article').first()).toContainText('11月2日からの週');
  await expect(weekly.getByRole('article').first()).toContainText('1／2回');
  for (const width of [1440, 390, 320]) {
    await page.setViewportSize({ width, height: 1000 });
    await expectNoHorizontalScroll(page);
    await page.screenshot({ path: `test-results/records-reflection-${width}.png`, fullPage: true });
  }
  await review.getByText('詳しい記録', { exact: true }).click();
  await expect(review.getByText('区切りで選んだ延長 1回')).toBeVisible();
  await weekly.getByRole('article').last().getByText('予定変更の記録', { exact: true }).click();
  await expect(weekly.getByText('スヌーズ 2回。予定を調整した回数です。')).toBeVisible();
  await expectAccessibleInBothSchemes(page);
  await review.getByRole('link', { name: '課題一覧を確認' }).click();
  await expect(page).toHaveURL(/\/tasks$/);
});
