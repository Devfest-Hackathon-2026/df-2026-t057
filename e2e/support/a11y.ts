import AxeBuilder from '@axe-core/playwright';
import { expect, test, type Page } from '@playwright/test';

export interface AccessibleOptions {
  /** 検査から外す要素（理由をコメントに書く）。 */
  readonly exclude?: readonly string[];
  /** 検査する範囲（ダイアログだけを検査するときなど）。 */
  readonly include?: readonly string[];
}

/** WCAG 2.2 AA の違反を「ルール: 対象の要素」の一覧で返す。 */
export async function axeViolations(
  page: Page,
  options: AccessibleOptions = {},
): Promise<string[]> {
  let builder = new AxeBuilder({ page }).withTags([
    'wcag2a',
    'wcag2aa',
    'wcag21a',
    'wcag21aa',
    'wcag22aa',
  ]);
  for (const selector of options.include ?? []) builder = builder.include(selector);
  for (const selector of options.exclude ?? []) builder = builder.exclude(selector);
  const { violations } = await builder.analyze();
  return violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target.join(' ')).join(', ')}`);
}

/** WCAG 2.2 AA の自動検査。違反があれば、ルールと対象の要素を出して失敗させる。 */
export async function expectAccessible(page: Page, options: AccessibleOptions = {}) {
  expect(await axeViolations(page, options), 'アクセシビリティの違反').toEqual([]);
}

/**
 * ライトとダークの両方で検査し、両方の違反をまとめて出す。最後にライトへ戻す。
 * 色の切り替えのアニメーションが終わってから検査する（途中の色でコントラストを測らない）。
 */
export async function expectAccessibleInBothSchemes(page: Page, options: AccessibleOptions = {}) {
  const found: string[] = [];
  for (const colorScheme of ['light', 'dark'] as const) {
    await page.emulateMedia({ colorScheme, reducedMotion: 'reduce' });
    await page.evaluate(async () => {
      await new Promise<void>((resolve) => {
        requestAnimationFrame(() => {
          requestAnimationFrame(() => {
            resolve();
          });
        });
      });
      // ダイアログの表示や色の切り替えのアニメーションが終わるまで待つ（無限に続くものは待たない）。
      const finite = document
        .getAnimations()
        .filter((animation) => animation.effect?.getTiming().iterations !== Infinity);
      await Promise.race([
        Promise.allSettled(finite.map((animation) => animation.finished)),
        new Promise((resolve) => setTimeout(resolve, 2000)),
      ]);
    });
    const violations = await test.step(`axe (${colorScheme})`, () => axeViolations(page, options));
    found.push(...violations.map((violation) => `[${colorScheme}] ${violation}`));
  }
  await page.emulateMedia({ colorScheme: 'light' });
  expect(found, 'アクセシビリティの違反').toEqual([]);
}
