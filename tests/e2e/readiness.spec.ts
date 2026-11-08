import { test, expect } from '@playwright/test';

test('archiving a completed task keeps the completion calendar and export', async ({ page }) => {
  await page.goto('/tasks');
  await expect(page.getByRole('heading', { name: '課題', exact: true })).toBeVisible();
  const task = {
    id: 'archive-e2e',
    title: '整理しても残す架空課題',
    notes: '',
    minutes: 10,
    deadline: new Date(Date.now() + 86400000).toISOString(),
    url: '',
  };
  expect((await page.request.post('/api/tasks', { data: task })).ok()).toBe(true);
  expect((await page.request.post('/api/tasks/archive-e2e/complete')).ok()).toBe(true);
  expect((await page.request.post('/api/tasks/archive-e2e/archive')).ok()).toBe(true);
  await page.goto('/records');
  await expect(page.locator('.records-count')).toHaveText(/1\s*件/);
  await expect(page.getByRole('region', { name: '保存した作業記録' })).toContainText(task.title);
  const exported = await (await page.request.get('/api/account/export')).json();
  expect(exported.archivedTasks).toHaveLength(1);
  expect(exported.records.completions).toHaveLength(1);
});

test('setup diagnoses the extension without changing data', async ({ page }) => {
  await page.goto('/setup');
  const before = await (await page.request.get('/api/state')).json();
  await page.evaluate(() =>
    window.addEventListener('message', (event) => {
      if (event.source === window && event.data?.type === 'DOPAGAKI_DIAGNOSE')
        window.postMessage(
          {
            type: 'DOPAGAKI_DIAGNOSTICS',
            requestId: event.data.requestId,
            diagnostics: { email: '', notificationPermission: 'granted', protocolVersion: 2 },
          },
          location.origin,
        );
    }),
  );
  await page.getByRole('button', { name: '確認する', exact: true }).click();
  await expect(page.getByRole('status')).toContainText('拡張から応答がありました');
  await page.getByRole('button', { name: '通知が見えた', exact: true }).click();
  await expect(
    page.getByRole('button', { name: '見えたことを確認済み', exact: true }),
  ).toBeVisible();
  const after = await (await page.request.get('/api/state')).json();
  expect(after.balanceSeconds).toBe(before.balanceSeconds);
  expect(after.tasks).toEqual(before.tasks);
});

test('account tools cancel deletion and download owned JSON', async ({ page }) => {
  await page.goto('/settings');
  await page.getByRole('button', { name: 'データとアカウント', exact: true }).click();
  await page.getByRole('button', { name: '退会してデータを削除', exact: true }).click();
  await expect(page.getByRole('dialog')).toContainText('課題・記録・会話');
  await page.getByRole('button', { name: 'キャンセル', exact: true }).click();
  expect((await page.request.get('/api/state')).ok()).toBe(true);
  const download = page.waitForEvent('download');
  await page.getByRole('button', { name: 'データを書き出す（JSON）', exact: true }).click();
  expect((await download).suggestedFilename()).toBe('dopagaki-export.json');
  await expect(
    page.getByRole('status').filter({ hasText: '書き出しが完了しました' }),
  ).toBeVisible();
});

test('calendar conflict confirmation keeps the revision and retry id from the preview', async ({
  page,
}) => {
  await page.clock.install();
  await page.goto('/tasks');
  const input = {
    id: 'conflict-e2e',
    title: '競合確認の架空課題',
    notes: '保持するメモ',
    minutes: 20,
    deadline: new Date(Date.now() + 86400000).toISOString(),
    url: '',
  };
  expect((await page.request.post('/api/tasks', { data: input })).ok()).toBe(true);
  const state = await (await page.request.get('/api/state')).json();
  const baseRevision = state.planRevision;
  state.calendarSync = 'conflict';
  state.calendarConflicts = [
    { taskId: input.id, remote: { ...input, title: 'Google側の架空課題' }, etag: 'remote-v1' },
  ];
  let stateCalls = 0;
  await page.route('**/api/state', (route) => {
    stateCalls++;
    return route.fulfill({ json: state });
  });
  const commits: { revision: number; etag: string; id: string }[] = [];
  await page.route('**/api/tasks/conflict-e2e/calendar-resolution*', async (route) => {
    const body = route.request().postDataJSON();
    if (route.request().url().includes('preview=true'))
      return route.fulfill({
        json: {
          input,
          draft: {
            id: 'preview',
            taskId: input.id,
            baseRevision,
            createdAt: new Date().toISOString(),
            start: state.tasks[0].plan.start,
            changes: [],
            reason: '',
          },
        },
      });
    commits.push(body);
    if (commits.length === 1)
      return route.fulfill({ status: 503, json: { error: '同期を保存できませんでした' } });
    return route.fulfill({ json: { ...state, calendarConflicts: [], calendarSync: 'synced' } });
  });
  await page.goto('/calendar');
  await page.getByRole('button', { name: '内容を比較して解決', exact: true }).click();
  await page.getByRole('button', { name: '変更後を確認', exact: true }).click();
  await expect(page.getByRole('button', { name: 'この内容で確定', exact: true })).toBeVisible();
  const calls = stateCalls;
  state.planRevision++;
  await page.clock.fastForward(31000);
  await expect.poll(() => stateCalls).toBeGreaterThan(calls);
  await page.getByRole('button', { name: 'この内容で確定', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('同期を保存できませんでした');
  await page.getByRole('button', { name: 'この内容で確定', exact: true }).click();
  await expect(page.getByRole('button', { name: '内容を比較して解決', exact: true })).toHaveCount(
    0,
  );
  expect(commits).toHaveLength(2);
  expect(commits[0].revision).toBe(baseRevision);
  expect(commits[0].etag).toBe('remote-v1');
  expect(commits[0].id).toBe(commits[1].id);
});
