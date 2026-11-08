import type { Locator, Page } from '@playwright/test';

/** カレンダー（ホーム）の主な領域。 */
export function workspace(page: Page) {
  const summary = page.getByRole('region', { name: '視聴できる時間と次の課題' });
  return {
    summary,
    /** 「今日、視聴できる時間」の値（意味のある役割を持たない強調表示なので、クラスで探す）。 */
    credit: summary.locator('.credit-number'),
    creditHelp: summary.getByRole('button', { name: '視聴時間について', exact: true }),
    calendar: page.getByRole('region', { name: '予定カレンダー' }),
    detail: page.getByRole('region', { name: '選択した課題' }),
    composer: page.getByRole('complementary', { name: '予定の相談と詳細' }).getByLabel('相談する', {
      exact: true,
    }),
  };
}

/** 設定の折りたたみの見出しを開く（すでに開いていれば何もしない）。 */
export async function expandPanel(page: Page, name: string | RegExp): Promise<Locator> {
  const header = page.getByRole('button', { name }).and(page.locator('mat-expansion-panel-header'));
  if ((await header.getAttribute('aria-expanded')) !== 'true') await header.click();
  return header;
}
