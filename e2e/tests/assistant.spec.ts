import { hoursFromNow, taskInput, withoutId } from '../support/api';
import { expect, test } from '../support/fixtures';
import { expectNoHorizontalScroll } from '../support/layout';
import { workspace } from '../support/pages';
import { enableAi, Gate, mockAssistant } from '../support/routes';

test('conversation cards require confirmation, preserve edits, and update real task state', async ({
  page,
  api,
  pageErrors,
}) => {
  await enableAi(page);
  const gate = new Gate();
  await mockAssistant(page, async () => {
    const input = taskInput({
      id: 'assistant-task',
      title: 'AIレポート',
      notes: '構成と本文をまとめる',
      deadline: hoursFromNow(4),
    });
    // カードの計画と版は、サーバーの計算（保存しないプレビュー）から作る。
    const preview = await api.previewNewTask(withoutId(input));
    await gate.wait();
    return {
      reply: '内容を確認してから予定へ登録できます。',
      cards: [
        {
          kind: 'create',
          id: 'create-card',
          input,
          preview: preview.plan,
          revision: preview.revision,
        },
        { kind: 'memory', id: 'memory-card', memory: '30分ずつ区切ると進めやすい' },
      ],
    };
  });

  await page.goto('/calendar');
  const home = workspace(page);
  await page.getByRole('radio', { name: '相談', exact: true }).click();
  await home.composer.fill('4時間後までにレポートを30分で。区切って進めたい');
  await page.getByRole('button', { name: '送信', exact: true }).click();
  await expect(page.getByRole('button', { name: '相談中…', exact: true })).toBeDisabled();
  await expect(home.composer).toHaveValue('4時間後までにレポートを30分で。区切って進めたい');
  // 返答を待っている間も、カードを押すまでも、何も保存しない。
  expect((await api.tasks()).items).toHaveLength(0);
  expect((await api.wallet()).balanceSeconds).toBe(600);
  expect((await api.profile()).context).toBe('');
  gate.open();

  const card = page.locator('app-assistant-card').filter({ hasText: '予定カード（デモ）' });
  await expect(card).toBeVisible();
  await expect(card).toContainText('登録前');
  await expect(home.composer).toHaveValue('');
  await card.getByRole('button', { name: '編集', exact: true }).click();
  await card.getByLabel('課題名', { exact: true }).fill('編集したレポート');
  await card.getByLabel('必要時間（分）').fill('15');
  await card.getByRole('button', { name: '変更内容を確認', exact: true }).click();
  await expect(card.getByRole('heading')).toHaveText('編集したレポート');
  await expect(card).toContainText('15分');
  // 編集を取り消すと、確認済みの内容に戻る。
  await card.getByRole('button', { name: '編集', exact: true }).click();
  await card.getByLabel('課題名', { exact: true }).fill('取り消す変更');
  await card.getByRole('button', { name: 'キャンセル', exact: true }).click();
  await expect(card.getByRole('heading')).toHaveText('編集したレポート');
  expect((await api.tasks()).items).toHaveLength(0);

  await card.getByRole('button', { name: 'カレンダーに登録 ＋5分', exact: true }).click();
  await expect(card.getByRole('button', { name: '予定・手順を調整', exact: true })).toBeVisible();
  await expect(card).toContainText('現在の予定');

  const memory = page.locator('app-assistant-card').filter({ hasText: '覚えておくこと' });
  await memory.getByLabel('覚える内容を確認・編集').fill('25分ずつ区切ると進めやすい');
  await memory.getByRole('button', { name: 'この内容を覚える', exact: true }).click();
  await expect(memory).toContainText('確認済み');
  await page.getByText('設定・使い方', { exact: true }).click();
  await expect(page.locator('.known-context')).toHaveText('25分ずつ区切ると進めやすい');

  const wallet = await api.wallet();
  expect(wallet.balanceSeconds).toBe(900);
  const tasks = (await api.tasks()).items;
  expect(tasks).toHaveLength(1);
  expect(tasks[0]?.id).toBe('assistant-task');
  expect(tasks[0]?.title).toBe('編集したレポート');
  expect(tasks[0]?.minutes).toBe(15);
  expect((await api.profile()).context).toContain('25分ずつ区切ると進めやすい');

  await card.getByRole('button', { name: '予定・手順を調整', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: '予定を調整' });
  await expect(dialog.getByLabel('開始', { exact: true })).toBeVisible();
  await dialog.getByRole('button', { name: 'この予定に変更', exact: true }).click();
  await expect(dialog).toBeHidden();

  await page.reload();
  await page.getByRole('radio', { name: '相談', exact: true }).click();
  await expect(card).toContainText('現在の予定');
  // 登録済みのカードから、二度登録しない。
  await expect(card.getByRole('button', { name: /カレンダーに登録/ })).toHaveCount(0);

  await page.getByText('設定・使い方', { exact: true }).click();
  await page.getByRole('button', { name: '編集・忘れる内容を選ぶ', exact: true }).click();
  await page.getByLabel('作業の傾向', { exact: true }).fill('');
  await page.getByRole('button', { name: '保存', exact: true }).click();
  await expect.poll(async () => (await api.profile()).context).toBe('');
  expect((await api.wallet()).balanceSeconds).toBe(900);
  expect((await api.tasks()).items).toHaveLength(1);

  await page.setViewportSize({ width: 390, height: 844 });
  await expectNoHorizontalScroll(page);
  expect(pageErrors).toEqual([]);
});
