import { expect, type Page } from '@playwright/test';

/** 横方向のスクロールが出ていない（WCAG 1.4.10 リフロー）。 */
export async function expectNoHorizontalScroll(page: Page): Promise<void> {
  await expect
    .poll(() => page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth), {
      message: '横スクロールが出ている',
    })
    .toBe(true);
}

/**
 * 幅を変えて、横スクロールが出ないことを確かめる。
 * - 320px：400% 拡大と同じ幅（WCAG 1.4.10）
 * - 720×540：1440×1080 の画面を 200% に拡大したのと同じ CSS の大きさ（WCAG 1.4.4）
 * 終わったら元の大きさに戻す。
 */
export async function expectReflow(page: Page): Promise<void> {
  const original = page.viewportSize();
  for (const size of [
    { width: 720, height: 540 },
    { width: 320, height: 800 },
  ]) {
    await page.setViewportSize(size);
    await expectNoHorizontalScroll(page);
  }
  if (original) await page.setViewportSize(original);
}

/** 要素が画面の横幅に収まっている。 */
export function expectInsideViewport(
  page: Page,
  box: { x: number; width: number; y?: number; height?: number } | null,
): void {
  const viewport = page.viewportSize();
  expect(box, '要素が表示されていない').not.toBeNull();
  if (!box || !viewport) return;
  expect(box.x).toBeGreaterThanOrEqual(0);
  expect(box.x + box.width).toBeLessThanOrEqual(viewport.width + 0.5);
  if (box.y !== undefined && box.height !== undefined) {
    expect(box.y + box.height).toBeLessThanOrEqual(viewport.height + 0.5);
  }
}
