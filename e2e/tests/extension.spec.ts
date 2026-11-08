import type { Viewing } from '../support/api';
import { expectAccessibleInBothSchemes } from '../support/a11y';
import { hoursFromNow } from '../support/api';
import { expect, openShorts, routeYouTube, test, videoPaused } from '../support/extension';

/** service worker の中で使う chrome API の一部（e2e は拡張の型を持たない）。 */
interface ChromeLike {
  readonly tabs: { query(query: { url: string }): Promise<{ id?: number }[]> };
  readonly scripting: {
    executeScript(options: {
      target: { tabId: number };
      func: () => Promise<boolean>;
    }): Promise<{ result?: boolean }[]>;
  };
  readonly storage: { local: { get(key: string): Promise<unknown> } };
}
declare const chrome: ChromeLike;

/** 拡張の画面の検査範囲（Shorts の代わりのページそのものは検査しない）。 */
const overlayOnly = { include: ['#dopagaki-extension'] } as const;

test('connects automatically, counts foreground Shorts and keeps one viewing session across tabs', async ({
  context,
  worker,
  api,
  app,
  baseURL,
}) => {
  test.setTimeout(150_000);
  await routeYouTube(context, { autoplay: true });
  // 拡張の service worker が送る視聴の報告を見張る。最初の1回はネットワークの失敗にする。
  const reportKeys: string[] = [];
  let failNext = false;
  let failedKey: string | undefined;
  await context.route('**/api/viewing/reports', async (route) => {
    const key = (await route.request().headerValue('idempotency-key')) ?? '';
    reportKeys.push(key);
    if (failNext) {
      failNext = false;
      failedKey = key;
      await route.abort('internetdisconnected');
      return;
    }
    await route.fallback();
  });

  const { page: youtube, overlay } = await openShorts(context, 'dopagaki-fixture');
  // 管理画面から自動で接続し、区切りを選ぶまで動画を止める。
  const choose = overlay.getByRole('button', { name: /^5分にする/ });
  await expect(choose).toBeVisible({ timeout: 20_000 });
  await expect(overlay.getByRole('dialog', { name: '視聴の区切り' })).toContainText(
    '今回は、何分見ますか',
  );
  await expect.poll(() => videoPaused(youtube)).toBe(true);
  await expectAccessibleInBothSchemes(youtube, overlayOnly);

  await choose.click();
  const bar = overlay.getByRole('region', { name: '視聴タイマー' });
  await expect(bar).toBeVisible();
  await expect.poll(() => videoPaused(youtube)).toBe(false);
  await expect
    .poll(async () => (await api.viewing()).balanceSeconds, { timeout: 45_000 })
    .toBeLessThan(600);
  const started = await api.viewing();
  const sessionId = started.session?.id ?? '';
  expect(started.session?.plannedSeconds).toBe(300);
  expect(sessionId).not.toBe('');

  // 報告が届かなければ動画を止めて知らせ、再接続したら同じ受付の ID で送り直す（二重に減らさない）。
  failNext = true;
  const reconnect = overlay.getByRole('button', { name: '再接続して続ける', exact: true });
  await expect(reconnect).toBeVisible({ timeout: 45_000 });
  await expect.poll(() => videoPaused(youtube)).toBe(true);
  const balanceAfterFailure = (await api.viewing()).balanceSeconds;
  await reconnect.click();
  await expect.poll(() => reportKeys.filter((key) => key === failedKey).length).toBe(2);
  await expect(bar).toBeVisible();
  const retried = await api.viewing();
  expect(retried.session?.id).toBe(sessionId);
  expect(retried.balanceSeconds).toBeLessThan(balanceAfterFailure);

  // ほかのタブは新しい区切りを始めず、今の区切りを引き継ぐ。
  const { page: other, overlay: otherOverlay } = await openShorts(context, 'other-fixture');
  await otherOverlay.getByRole('button', { name: /このタブで続ける/ }).click();
  await expect(otherOverlay.getByRole('region', { name: '視聴タイマー' })).toBeVisible();
  await youtube.bringToFront();
  await overlay.getByRole('button', { name: /このタブで続ける/ }).click();
  await expect(bar).toBeVisible();
  expect((await api.viewing()).session?.id).toBe(sessionId);
  await other.close();

  await youtube.bringToFront();
  // 再接続の直後でも、すぐに終えられる（送り直しの待ち時間を利用者に見せない）。
  await bar.getByRole('button', { name: 'ここで終える', exact: true }).click();
  await expect(overlay.getByText('接続できませんでした', { exact: false })).toHaveCount(0);
  await expect.poll(async () => (await api.viewing()).session?.endReason).toBe('stop');
  await expect(overlay.getByText('決めたところで、視聴を終えました。')).toBeVisible();
  await expect.poll(() => videoPaused(youtube)).toBe(true);

  // 終えたあとに再生すると、もう一度区切りを決める（延長は今の区切りから続ける）。
  await youtube.locator('video').evaluate((video: HTMLVideoElement) => video.play());
  await overlay.getByRole('button', { name: '残り枠からもう5分', exact: true }).click();
  await expect.poll(async () => (await api.viewing()).session?.extendFrom).toBe(sessionId);
  const extended = await api.viewing();
  expect(extended.session?.id).not.toBe(sessionId);
  expect(extended.session?.endedAt).toBeUndefined();

  // 拡張が保存した状態は、ページに入る content script からは読めない。
  const blocked = await worker.evaluate(async (origin) => {
    const [tab] = await chrome.tabs.query({ url: `${origin}/*` });
    const [result] = await chrome.scripting.executeScript({
      target: { tabId: tab?.id ?? -1 },
      func: async () => {
        try {
          await chrome.storage.local.get('state');
          return false;
        } catch {
          return true;
        }
      },
    });
    return result?.result;
  }, baseURL ?? '');
  expect(blocked).toBe(true);
  expect(app.url()).toContain('/calendar');

  // 今日の残りが無くなったら、動画を止めて使い切ったことを知らせる。
  await context.route(/\/api\/viewing(\/reports)?$/, async (route) => {
    const response = await route.fetch();
    const viewing = (await response.json()) as Viewing;
    await route.fulfill({ response, json: { ...viewing, availableSeconds: 0 } });
  });
  await expect(
    overlay.getByText('今日の視聴枠を使い切りました。休憩や次の作業へ移りましょう。'),
  ).toBeVisible({ timeout: 45_000 });
  await expect.poll(() => videoPaused(youtube)).toBe(true);
});

test('presents a due task with a companion line and runs the work timer', async ({
  context,
  api,
  app,
}) => {
  test.setTimeout(120_000);
  await routeYouTube(context, { autoplay: false });
  const { page: youtube, overlay } = await openShorts(context, 'dopagaki-notice');
  expect(app.url()).toContain('/calendar');
  // 開始時刻を迎えた課題（締切30分前に30分の作業）。
  const task = await api.createTask({ title: '拡張の動作を確認する', deadline: hoursFromNow(0.5) });

  const notice = overlay.getByRole('dialog', { name: '課題をはじめる時間です' });
  await expect(notice.getByRole('heading', { name: 'そろそろ、やるか。' })).toBeVisible({
    timeout: 30_000,
  });
  await expect(notice.getByRole('heading', { name: '拡張の動作を確認する' })).toBeVisible();
  // デモには AI がないので、相棒の一言は決まった文になる。
  await expect(notice).toContainText('一緒に始めよっか');
  await expect(notice.getByRole('button', { name: '作業をはじめる', exact: true })).toBeFocused();
  await expectAccessibleInBothSchemes(youtube, overlayOnly);

  // スヌーズの案は確認するだけ。OK を押すまで予定は変わらない。
  await notice.getByRole('button', { name: '10分スヌーズ・予定を確認', exact: true }).click();
  const draft = overlay.getByRole('dialog', { name: '変更する予定を確認' });
  await expect(draft.getByRole('heading', { name: 'スヌーズ後の予定を確認' })).toBeVisible();
  await expect(draft.getByRole('button', { name: 'OK・この予定に変更' })).toBeVisible();
  await expectAccessibleInBothSchemes(youtube, overlayOnly);
  await draft.getByRole('button', { name: 'キャンセル', exact: true }).click();
  expect((await api.task(task.id)).plan.start).toBe(task.plan.start);

  await notice.getByRole('button', { name: '作業をはじめる', exact: true }).click();
  const timer = overlay.getByRole('region', { name: 'DOPAGAKI 作業タイマー' });
  await expect(timer).toBeVisible();
  await expect.poll(async () => (await api.workSession())?.taskId).toBe(task.id);
  await timer.getByRole('button', { name: '一時停止', exact: true }).click();
  await expect(timer.getByRole('button', { name: '再開', exact: true })).toBeVisible();
  await expect.poll(async () => (await api.workSession())?.running).toBe(false);
  await timer.getByRole('button', { name: '再開', exact: true }).click();
  await expect(timer.getByRole('button', { name: '一時停止', exact: true })).toBeVisible();
  await timer.getByRole('button', { name: '課題を完了', exact: true }).click();
  await expect(overlay.getByRole('status').filter({ hasText: '作業を記録しました' })).toBeVisible();
  await expect(timer).toBeHidden();
  expect((await api.task(task.id)).status).toBe('completed');

  // 次の課題のスヌーズを確定すると、予定が後ろへずれる。
  const next = await api.createTask({ title: 'スヌーズする課題', deadline: hoursFromNow(0.5) });
  await expect(notice.getByRole('heading', { name: 'スヌーズする課題' })).toBeVisible({
    timeout: 30_000,
  });
  await notice.getByRole('button', { name: '10分スヌーズ・予定を確認', exact: true }).click();
  await draft.getByRole('button', { name: 'OK・この予定に変更' }).click();
  await expect(draft).toBeHidden();
  await expect(overlay.getByRole('status').filter({ hasText: '少し後ろにずらそう' })).toBeVisible();
  await expect
    .poll(async () => Date.parse((await api.task(next.id)).plan.start))
    .toBeGreaterThan(Date.parse(next.plan.start));
});
