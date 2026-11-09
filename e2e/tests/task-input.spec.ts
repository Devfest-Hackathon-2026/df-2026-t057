import { hoursFromNow, taskInput, withoutId } from '../support/api';
import { expect, test } from '../support/fixtures';
import { expectNoHorizontalScroll } from '../support/layout';
import { enableAi, fulfillProblem, Gate, mockAssistant } from '../support/routes';
import { taskAction } from '../support/pages';

test('registration dialog supports conversation and manual input without losing drafts', async ({
  page,
  api,
}) => {
  await enableAi(page);
  const gate = new Gate();
  const assistant = await mockAssistant(page, async () => {
    const input = taskInput({
      id: 'dialog-created',
      title: '相談で作るレポート',
      notes: '考察をまとめる',
      deadline: hoursFromNow(4),
    });
    const preview = await api.previewNewTask(withoutId(input));
    await gate.wait();
    return {
      reply: 'このカードで確認してから登録できます。',
      cards: [
        {
          kind: 'create',
          id: 'dialog-card',
          input,
          preview: preview.plan,
          revision: preview.revision,
        },
      ],
    };
  });
  await page.goto('/tasks');
  await page.getByRole('button', { name: '課題を登録', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: '課題を登録' });
  await dialog.getByLabel('課題名', { exact: true }).fill('手入力の下書き');
  await dialog.getByRole('radio', { name: 'AIに相談', exact: true }).click();
  const composer = dialog.getByLabel('相談する', { exact: true });
  await composer.fill('4時間後までにレポート、30分。考察をまとめたい');
  await dialog.getByRole('radio', { name: '手入力', exact: true }).click();
  await expect(dialog.getByLabel('課題名', { exact: true })).toHaveValue('手入力の下書き');
  await dialog.getByRole('radio', { name: 'AIに相談', exact: true }).click();
  await expect(composer).toHaveValue('4時間後までにレポート、30分。考察をまとめたい');

  await dialog.getByRole('button', { name: '送信', exact: true }).click();
  await expect(dialog.getByRole('button', { name: '相談中…' })).toBeDisabled();
  await expect(dialog.getByRole('button', { name: '閉じる', exact: true })).toBeDisabled();
  expect((await api.tasks()).items).toHaveLength(0);
  gate.open();

  const register = dialog.getByRole('button', { name: 'カレンダーに登録 ＋5分', exact: true });
  await expect(register).toBeVisible();
  expect(assistant.requests[0]?.taskId).toBeUndefined();
  await register.click();
  await expect(dialog.getByText('現在の予定', { exact: true })).toBeVisible();
  const tasks = (await api.tasks()).items;
  expect(tasks.map((task) => task.id)).toEqual(['dialog-created']);
  expect((await api.wallet()).balanceSeconds).toBe(900);
  await dialog.getByRole('button', { name: '閉じる', exact: true }).click();
  await expect(dialog).toBeHidden();
});

test('task content consultation previews, rejects stale confirmation and saves without reward', async ({
  page,
  api,
}) => {
  const original = taskInput({
    id: 'edit-task',
    title: '元のレポート',
    notes: '元のメモ',
    why: '提出のため',
    deadline: hoursFromNow(4),
  });
  await api.createTask(original);
  const plain = withoutId(original);
  const changed = {
    ...plain,
    title: '考察を追加するレポート',
    notes: '比較と考察を追加',
    minutes: 45,
  };
  await enableAi(page);
  await mockAssistant(page, async (request) => {
    expect(request.taskId).toBe('edit-task');
    const draft = await api.previewTaskEdit('edit-task', changed);
    return {
      reply: '内容と時間割を確認してください。',
      cards: [
        {
          kind: 'update',
          id: 'content-edit-card',
          taskId: 'edit-task',
          input: changed,
          draft,
          revision: draft.baseRevision,
        },
      ],
    };
  });

  await page.goto('/tasks');
  const open = () => taskAction(page, '課題を編集');
  await open();
  const dialog = page.getByRole('dialog', { name: '課題の内容を調整' });
  await expect(dialog.getByText('相談する予定')).toBeVisible();
  await expect(dialog.locator('.selected-context')).toContainText('元のレポート');
  await dialog.getByLabel('相談する', { exact: true }).fill('考察を追加して、45分に変更したい');
  await dialog.getByRole('button', { name: '送信', exact: true }).click();
  await expect(dialog.getByRole('heading', { name: '課題の変更案' })).toBeVisible();
  // 変更案は保存しない。
  expect((await api.task('edit-task')).title).toBe('元のレポート');
  expect((await api.wallet()).balanceSeconds).toBe(900);
  await dialog.getByRole('button', { name: '閉じる', exact: true }).click();

  // ほかの課題が増えると計画の版が変わり、古い変更案はそのまま保存できない。
  await api.createTask({
    ...plain,
    id: 'another-task',
    title: '別の課題',
    deadline: hoursFromNow(6),
  });
  await page.reload();
  await open();
  const recheck = dialog.getByRole('button', { name: '最新の予定で再確認' });
  await expect(recheck).toBeVisible();
  await expect(dialog.getByRole('button', { name: 'この内容に変更する' })).toHaveCount(0);
  await recheck.click();
  const save = dialog.getByRole('button', { name: 'この内容に変更する' });
  await expect(save).toBeVisible();
  await dialog.getByText(/変更後の時間割/).click();
  await expect(dialog.getByText('別の課題')).toBeVisible();
  await page.setViewportSize({ width: 390, height: 844 });
  await expectNoHorizontalScroll(page);

  await page.route('**/api/tasks/edit-task', async (route) => {
    if (route.request().method() === 'PUT') await fulfillProblem(route, 503, 'internal');
    else await route.fallback();
  });
  await save.click();
  await expect(dialog.getByRole('alert')).toContainText('問題が発生しました');
  expect((await api.task('edit-task')).title).toBe('元のレポート');
  await page.unroute('**/api/tasks/edit-task');
  await save.click();
  await expect(dialog.getByRole('status')).toContainText('課題の変更を保存しました');

  const after = await api.task('edit-task');
  expect(after.title).toBe('考察を追加するレポート');
  expect(after.minutes).toBe(45);
  expect(after.why).toBe('提出のため');
  expect((await api.tasks()).items).toHaveLength(2);
  // 編集では増えない（900 ＋ もう1件の登録 300）。
  expect((await api.wallet()).balanceSeconds).toBe(1200);
});

test('manual content edits require a fresh preview after changing the form', async ({
  page,
  api,
}) => {
  await api.createTask({ id: 'manual-edit', title: '編集前', deadline: hoursFromNow(4) });
  await page.goto('/tasks');
  await taskAction(page, '課題を編集');
  const dialog = page.getByRole('dialog', { name: '課題の内容を調整' });
  await dialog.getByRole('radio', { name: '手入力', exact: true }).click();
  await dialog.getByLabel('課題名', { exact: true }).fill('手動の変更');
  await dialog.getByRole('button', { name: '変更内容を確認', exact: true }).click();
  const save = dialog.getByRole('button', { name: 'この内容に変更する' });
  await expect(save).toBeVisible();
  // 確認したあとに入力を変えたら、確認し直すまで保存できない。
  await dialog.getByLabel('やること・メモ').fill('確認後にさらに変更');
  await expect(save).toHaveCount(0);
  await dialog.getByRole('button', { name: '変更内容を確認', exact: true }).click();
  await save.click();
  await expect(dialog.getByRole('status')).toContainText('課題の変更を保存しました');
  const task = await api.task('manual-edit');
  expect(task.title).toBe('手動の変更');
  expect(task.notes).toBe('確認後にさらに変更');
  expect((await api.wallet()).balanceSeconds).toBe(900);
});

test('new AI registrations keep the composer visible and start separate conversations', async ({
  page,
  api,
}) => {
  await page.setViewportSize({ width: 560, height: 760 });
  await enableAi(page);
  let number = 0;
  const scopes: unknown[] = [];
  page.on('request', (request) => {
    if (request.url().endsWith('/api/assistant/turns') && request.method() === 'POST') {
      scopes.push((request.postDataJSON() as { historyTurnIds?: string[] }).historyTurnIds);
    }
  });
  await mockAssistant(page, async () => {
    number++;
    const input = taskInput({
      id: `fresh-${number}`,
      title: `課題${number}`,
      deadline: hoursFromNow(number * 4),
    });
    const preview = await api.previewNewTask(withoutId(input));
    return {
      reply: '登録案です。',
      cards: [
        {
          kind: 'create',
          id: `fresh-card-${number}`,
          input,
          preview: preview.plan,
          revision: preview.revision,
        },
      ],
    };
  });
  await page.goto('/tasks');
  for (const i of [1, 2]) {
    await page.getByRole('button', { name: '課題を登録', exact: true }).click();
    const dialog = page.getByRole('dialog', { name: '課題を登録' });
    await dialog.getByRole('radio', { name: 'AIに相談', exact: true }).click();
    await expect(dialog.getByLabel('課題名', { exact: true })).toHaveCount(0);
    await expect(dialog.locator('.turn')).toHaveCount(0);
    const composer = dialog.getByLabel('相談する', { exact: true });
    const send = dialog.getByRole('button', { name: '送信', exact: true });
    await expect(composer).toBeInViewport({ ratio: 1 });
    await expect(send).toBeInViewport({ ratio: 1 });
    const before = await composer.boundingBox();
    const thread = dialog.getByRole('log');
    await thread.hover();
    await page.mouse.wheel(0, 500);
    await expect.poll(() => thread.evaluate((element) => element.scrollTop)).toBeGreaterThan(0);
    expect((await composer.boundingBox())?.y).toBe(before?.y);
    await composer.fill(`課題${i}を登録したい`);
    await send.click();
    await dialog.getByRole('button', { name: 'カレンダーに登録', exact: false }).click();
    await expect(dialog.getByText('現在の予定', { exact: true })).toBeVisible();
    await dialog.getByRole('button', { name: '閉じる', exact: true }).click();
  }
  expect((await api.tasks()).items.map((task) => task.id)).toEqual(['fresh-1', 'fresh-2']);
  expect(scopes).toEqual([[], []]);
  await expect(page.locator('.task-row mat-icon').filter({ hasText: 'more_horiz' })).toHaveCount(2);
});
