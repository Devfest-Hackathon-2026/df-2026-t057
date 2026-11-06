import { test, expect } from '@playwright/test';
import { AssistantTurn } from '../../shared/models';

test('conversation cards require confirmation, preserve edits, and update real task state', async ({
  page,
}) => {
  const errors: string[] = [];
  page.on('pageerror', (e) => errors.push(e.message));
  let history: AssistantTurn[] = [];
  let release!: () => void;
  const ready = new Promise<void>((resolve) => (release = resolve));
  await page.route('**/api/config', async (route) => {
    const response = await route.fetch();
    await route.fulfill({ json: { ...(await response.json()), aiEnabled: true } });
  });
  await page.route('**/api/assistant', (route) => route.fulfill({ json: { turns: history } }));
  await page.route('**/api/assistant/messages', async (route) => {
    const request = route.request().postDataJSON();
    const deadline = new Date(Date.now() + 4 * 3600000).toISOString();
    await ready;
    const turn: AssistantTurn = {
      id: request.id,
      text: request.text,
      status: 'ready',
      createdAt: new Date().toISOString(),
      reply: '内容を確認してから予定へ登録できます。',
      cards: [
        {
          id: 'create-card',
          kind: 'create',
          revision: 1,
          input: {
            id: 'assistant-task',
            title: 'AIレポート',
            notes: '構成と本文をまとめる',
            minutes: 30,
            deadline,
            url: '',
          },
          preview: {
            start: new Date(Date.parse(deadline) - 30 * 60000).toISOString(),
            end: deadline,
            steps: [],
            reason: '予定の候補',
          },
        },
        { id: 'memory-card', kind: 'memory', memory: '30分ずつ区切ると進めやすい' },
      ],
    };
    history = [turn];
    await route.fulfill({ json: turn });
  });
  await page.goto('/');
  await page.getByRole('radio', { name: 'AIに相談', exact: true }).click();
  const composer = page.getByLabel('相談する', { exact: true });
  await composer.fill('4時間後までにレポートを30分で。区切って進めたい');
  await page.getByRole('button', { name: '送信', exact: true }).click();
  await expect(page.getByRole('button', { name: '相談中…', exact: true })).toBeDisabled();
  await expect(composer).toHaveValue('4時間後までにレポートを30分で。区切って進めたい');
  const before = await (await page.request.get('/api/state')).json();
  expect(before.tasks).toHaveLength(0);
  expect(before.balanceSeconds).toBe(3600);
  expect(before.profile.context).toBe('');
  release();
  const card = page.locator('app-assistant-card').filter({ hasText: '予定カード（デモ）' });
  await expect(card).toBeVisible();
  await expect(composer).toHaveValue('');
  await card.getByRole('button', { name: '編集', exact: true }).click();
  await card.getByLabel('課題名', { exact: true }).fill('編集したレポート');
  await card.getByLabel('必要時間（分）').fill('15');
  await card.getByRole('button', { name: '変更内容を確認', exact: true }).click();
  const edited = card;
  await expect(edited).toContainText('15分');
  await edited.getByRole('button', { name: '編集', exact: true }).click();
  await edited.getByLabel('課題名', { exact: true }).fill('取り消す変更');
  await page
    .locator('app-assistant-card')
    .getByRole('button', { name: 'キャンセル', exact: true })
    .click();
  await expect(edited.getByRole('heading')).toHaveText('編集したレポート');
  await edited.getByRole('button', { name: 'カレンダーに登録 ＋10分', exact: true }).click();
  await expect(edited.getByRole('button', { name: '予定・手順を調整', exact: true })).toBeVisible();
  const memory = page.locator('app-assistant-card').filter({ hasText: '覚えておくこと' });
  await memory.getByLabel('覚える内容を確認・編集').fill('25分ずつ区切ると進めやすい');
  await memory.getByRole('button', { name: 'この内容を覚える', exact: true }).click();
  await expect(memory).toContainText('確認済み');
  await expect(page.locator('.known-context')).toHaveText('25分ずつ区切ると進めやすい');
  let state = await (await page.request.get('/api/state')).json();
  expect(state.balanceSeconds).toBe(4200);
  expect(state.tasks).toHaveLength(1);
  expect(state.tasks[0].title).toBe('編集したレポート');
  expect(state.tasks[0].minutes).toBe(15);
  await edited.getByRole('button', { name: '予定・手順を調整', exact: true }).click();
  const dialog = page.getByRole('dialog');
  await expect(dialog.getByLabel('開始', { exact: true })).toBeVisible();
  await dialog.getByRole('button', { name: 'OK・この予定に変更', exact: true }).click();
  await expect(dialog).not.toBeVisible();
  await page.reload();
  await page.getByRole('radio', { name: 'AIに相談', exact: true }).click();
  await expect(edited).toContainText('現在の予定');
  await expect(memory).not.toContainText('30分ずつ区切ると進めやすい');
  await page.locator('.memory-disclosure summary').click();
  await page.getByRole('button', { name: '編集・忘れる内容を選ぶ', exact: true }).click();
  await page.getByLabel('作業の傾向', { exact: true }).fill('');
  await page.getByRole('button', { name: '保存', exact: true }).click();
  state = await (await page.request.get('/api/state')).json();
  expect(state.profile.context).toBe('');
  expect(state.balanceSeconds).toBe(4200);
  expect(state.tasks).toHaveLength(1);
  await page.setViewportSize({ width: 390, height: 844 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  expect(errors).toEqual([]);
});
