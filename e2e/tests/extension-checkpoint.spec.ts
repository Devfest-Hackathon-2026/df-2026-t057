import { expect, openShorts, routeYouTube, test } from '../support/extension';

declare const chrome: {
  storage: { local: { get(key: string): Promise<Record<string, unknown>> } };
};

for (const exit of ['reload', 'navigate', 'close'] as const) {
  test(`preserves pre-threshold viewing on ${exit}`, async ({ context, worker, api, app }) => {
    expect(app).toBeDefined();
    await routeYouTube(context, { autoplay: true });
    const { page, overlay } = await openShorts(context, `checkpoint-${exit}`);
    await overlay.getByRole('button', { name: /^5分にする/ }).click();
    await expect(overlay.getByRole('region', { name: '視聴タイマー' })).toBeVisible();
    await expect
      .poll(async () =>
        worker.evaluate(async () => {
          const journal = (await chrome.storage.local.get('viewingJournal'))['viewingJournal'] as
            { checkpoints?: { total: number }[] } | undefined;
          return journal?.checkpoints?.some((item) => item.total >= 2) ?? false;
        }),
      )
      .toBe(true);
    // 30秒の通常送信より前。サーバーにはまだ送っていない。
    expect((await api.viewing()).session?.consumedSeconds).toBe(0);
    // 復帰先は再生しない。終了前の区間だけが消費されたことを照合する。
    await routeYouTube(context, { autoplay: false });
    if (exit === 'reload') await page.reload();
    else if (exit === 'navigate') await page.goto('https://www.youtube.com/watch?v=synthetic');
    else await page.close();
    await expect
      .poll(async () => (await api.viewing()).session?.consumedSeconds ?? 0, { timeout: 15_000 })
      .toBeGreaterThanOrEqual(2);
    const once = (await api.viewing()).session?.consumedSeconds;
    await app.reload();
    await expect(app.getByRole('navigation', { name: 'メインメニュー' })).toBeVisible();
    expect((await api.viewing()).session?.consumedSeconds).toBe(once);
  });
}
