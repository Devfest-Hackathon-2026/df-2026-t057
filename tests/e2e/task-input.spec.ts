import { test, expect } from '@playwright/test';
import { AssistantTurn } from '../../shared/models';

test('registration dialog supports conversation and manual input without losing drafts', async ({
  page,
}) => {
  let history: AssistantTurn[] = [];
  await page.route('**/api/config', async (route) => {
    const response = await route.fetch();
    await route.fulfill({ json: { ...(await response.json()), aiEnabled: true } });
  });
  await page.route('**/api/assistant', (route) => route.fulfill({ json: { turns: history } }));
  let release!: () => void;
  const wait = new Promise<void>((resolve) => (release = resolve));
  await page.route('**/api/assistant/messages', async (route) => {
    const request = route.request().postDataJSON();
    expect(request.taskId).toBe('');
    const input = {
      id: 'dialog-created',
      title: '相談で作るレポート',
      notes: '考察をまとめる',
      why: '',
      minutes: 30,
      deadline: new Date(Date.now() + 4 * 3600000).toISOString(),
      url: '',
    };
    const response = await page.request.post('/api/assistant/preview', { data: input });
    const preview = await response.json();
    await wait;
    const turn: AssistantTurn = {
      ...request,
      status: 'ready',
      createdAt: new Date().toISOString(),
      reply: 'このカードで確認してから登録できます。',
      cards: [
        {
          id: 'dialog-card',
          kind: 'create',
          input,
          preview: preview.plan,
          revision: preview.revision,
        },
      ],
    };
    history = [turn];
    await route.fulfill({ json: turn });
  });
  await page.goto('/tasks');
  await page.getByRole('button', { name: '課題を登録', exact: true }).click();
  const dialog = page.getByRole('dialog');
  await dialog.getByLabel('課題名', { exact: true }).fill('手入力の下書き');
  await dialog.getByRole('radio', { name: '相談', exact: true }).click();
  const composer = dialog.getByLabel('相談する', { exact: true });
  await composer.fill('4時間後までにレポート、30分。考察をまとめたい');
  await dialog.getByRole('radio', { name: '手入力', exact: true }).click();
  await expect(dialog.getByLabel('課題名', { exact: true })).toHaveValue('手入力の下書き');
  await dialog.getByRole('radio', { name: '相談', exact: true }).click();
  await expect(composer).toHaveValue('4時間後までにレポート、30分。考察をまとめたい');
  await dialog.getByRole('button', { name: '送信', exact: true }).click();
  await expect(dialog.getByRole('button', { name: '相談中…' })).toBeDisabled();
  await expect(dialog.getByRole('button', { name: '閉じる', exact: true })).toBeDisabled();
  expect((await (await page.request.get('/api/state')).json()).tasks).toHaveLength(0);
  release();
  await expect(dialog.getByRole('button', { name: 'カレンダーに登録 ＋10分' })).toBeVisible();
  await dialog.getByRole('button', { name: 'カレンダーに登録 ＋10分' }).scrollIntoViewIfNeeded();
  await dialog.screenshot({
    path: 'test-results/task-registration-ai.png',
    animations: 'disabled',
  });
  await dialog.getByRole('button', { name: 'カレンダーに登録 ＋10分' }).click();
  await expect(dialog.getByText('現在の予定', { exact: true })).toBeVisible();
  const state = await (await page.request.get('/api/state')).json();
  expect(state.tasks).toHaveLength(1);
  expect(state.tasks[0].id).toBe('dialog-created');
  expect(state.balanceSeconds).toBe(4200);
});

test('task content consultation previews, cancels, rejects stale confirmation and saves without reward', async ({
  page,
}) => {
  await page.request.get('/api/state');
  const original = {
    id: 'edit-task',
    title: '元のレポート',
    notes: '元のメモ',
    why: '提出のため',
    minutes: 30,
    deadline: new Date(Date.now() + 4 * 3600000).toISOString(),
    url: '',
  };
  await page.request.post('/api/tasks', { data: original });
  let history: AssistantTurn[] = [];
  await page.route('**/api/config', async (route) => {
    const response = await route.fetch();
    await route.fulfill({ json: { ...(await response.json()), aiEnabled: true } });
  });
  await page.route('**/api/assistant', (route) => route.fulfill({ json: { turns: history } }));
  await page.route('**/api/assistant/messages', async (route) => {
    const request = route.request().postDataJSON();
    expect(request.taskId).toBe(original.id);
    const response = await page.request.post(`/api/tasks/${original.id}/preview`, {
      data: {
        ...original,
        title: '考察を追加するレポート',
        notes: '比較と考察を追加',
        minutes: 45,
      },
    });
    expect(response.ok()).toBe(true);
    const change = await response.json();
    const turn: AssistantTurn = {
      ...request,
      status: 'ready',
      createdAt: new Date().toISOString(),
      reply: '内容と時間割を確認してください。',
      cards: [{ id: 'content-edit-card', kind: 'update', taskId: original.id, change }],
    };
    history = [turn];
    await route.fulfill({ json: turn });
  });
  await page.goto('/tasks');
  const open = () => page.getByRole('button', { name: '課題を編集' }).first().click();
  await open();
  const dialog = page.getByRole('dialog');
  await expect(dialog.locator('.selected-context')).toContainText(original.title);
  await dialog.getByLabel('相談する', { exact: true }).fill('考察を追加して、45分に変更したい');
  await dialog.getByRole('button', { name: '送信', exact: true }).click();
  await expect(dialog.getByRole('heading', { name: '課題の変更案' })).toBeVisible();
  const unchanged = await (await page.request.get('/api/state')).json();
  expect(unchanged.tasks[0].title).toBe(original.title);
  expect(unchanged.balanceSeconds).toBe(4200);
  await dialog.getByRole('button', { name: '閉じる', exact: true }).click();
  await page.request.post('/api/tasks', {
    data: {
      ...original,
      id: 'another-task',
      title: '別の課題',
      deadline: new Date(Date.now() + 6 * 3600000).toISOString(),
    },
  });
  await page.reload();
  await open();
  await expect(dialog.getByRole('button', { name: '最新の予定で再確認' })).toBeVisible();
  await dialog.getByRole('button', { name: '最新の予定で再確認' }).click();
  await expect(dialog.getByRole('button', { name: 'この内容に変更する' })).toBeVisible();
  await dialog.locator('summary').filter({ hasText: '変更後の時間割' }).click();
  await dialog.screenshot({ path: 'test-results/task-content-ai.png', animations: 'disabled' });
  await page.setViewportSize({ width: 390, height: 844 });
  await dialog.screenshot({
    path: 'test-results/task-content-ai-mobile.png',
    animations: 'disabled',
  });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await page.route('**/api/tasks/edit-task', async (route) => {
    if (route.request().method() === 'PUT')
      await route.fulfill({ status: 503, json: { error: '保存を確認できませんでした' } });
    else await route.continue();
  });
  await dialog.getByRole('button', { name: 'この内容に変更する' }).click();
  await expect(dialog.getByRole('alert')).toContainText('保存を確認できませんでした');
  expect((await (await page.request.get('/api/state')).json()).tasks[0].title).toBe(original.title);
  await page.unroute('**/api/tasks/edit-task');
  await dialog.getByRole('button', { name: 'この内容に変更する' }).click();
  await expect(dialog.getByRole('status')).toContainText('課題の変更を保存しました');
  const after = await (await page.request.get('/api/state')).json();
  expect(after.tasks).toHaveLength(2);
  expect(after.tasks[0].title).toBe('考察を追加するレポート');
  expect(after.tasks[0].minutes).toBe(45);
  expect(after.tasks[0].why).toBe(original.why);
  expect(after.balanceSeconds).toBe(4800);
});

test('manual content edits require a fresh preview after changing the form', async ({ page }) => {
  await page.request.get('/api/state');
  await page.request.post('/api/tasks', {
    data: {
      id: 'manual-edit',
      title: '編集前',
      notes: '',
      minutes: 30,
      deadline: new Date(Date.now() + 4 * 3600000).toISOString(),
      url: '',
    },
  });
  await page.goto('/tasks');
  await page.getByRole('button', { name: '課題を編集' }).click();
  const dialog = page.getByRole('dialog');
  await dialog.getByRole('radio', { name: '手入力', exact: true }).click();
  await dialog.getByLabel('課題名', { exact: true }).fill('手動の変更');
  await dialog.getByRole('button', { name: '変更内容を確認', exact: true }).click();
  await expect(dialog.getByRole('button', { name: 'この内容に変更する' })).toBeVisible();
  await dialog.getByLabel('やること・メモ').fill('確認後にさらに変更');
  await expect(dialog.getByRole('button', { name: 'この内容に変更する' })).toHaveCount(0);
  await dialog.getByRole('button', { name: '変更内容を確認', exact: true }).click();
  await dialog.getByRole('button', { name: 'この内容に変更する' }).click();
  await expect(dialog.getByRole('status')).toContainText('課題の変更を保存しました');
  const state = await (await page.request.get('/api/state')).json();
  expect(state.tasks[0].title).toBe('手動の変更');
  expect(state.tasks[0].notes).toBe('確認後にさらに変更');
  expect(state.balanceSeconds).toBe(4200);
});
