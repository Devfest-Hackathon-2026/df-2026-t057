import { taskInput } from '../support/api';
import { expect, test } from '../support/fixtures';

test('変更通知はトークンをURLへ出さず、APIで追加された課題を画面へ反映する', async ({
  page,
  api,
  pageErrors,
}) => {
  const streamResponse = page.waitForResponse(
    (response) => new URL(response.url()).pathname === '/api/events',
  );
  await page.goto('/tasks');
  const response = await streamResponse;
  expect(response.status()).toBe(200);
  expect(response.headers()['content-type']).toBe('text/event-stream');
  expect(new URL(response.url()).search).toBe('');
  expect(response.request().headers()['accept']).toBe('text/event-stream');
  const title = '変更通知で届く架空の課題';
  await expect(page.getByRole('heading', { name: 'まだ課題がありません' })).toBeVisible();
  // ページ内の保存処理ではなく、別クライアントから変更する。SSE の通知で表示が追随することを確かめる。
  await api.createTask(taskInput({ title }));
  await expect(
    page.getByRole('list', { name: '課題一覧' }).getByRole('listitem').filter({ hasText: title }),
  ).toBeVisible();
  expect(pageErrors).toEqual([]);
});
