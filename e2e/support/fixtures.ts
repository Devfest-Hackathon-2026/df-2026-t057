import { test as base, expect } from '@playwright/test';
import { Api } from './api';

interface Fixtures {
  /** 画面と同じデモの利用者として API を呼ぶ。 */
  readonly api: Api;
  /** ページで起きた未処理の例外。 */
  readonly pageErrors: string[];
}

/**
 * 管理画面のテスト。テストごとに新しいデモの利用者になる（ブラウザーのコンテキストごとに Cookie が別）。
 * 画面を開く前に Cookie を発行させ、画面の最初の並列のリクエストと、テストの API 呼び出しを同じ利用者にそろえる。
 */
export const test = base.extend<Fixtures>({
  api: [
    async ({ page }, use) => {
      const api = new Api(page.request);
      await api.signIn();
      await use(api);
    },
    { auto: true },
  ],
  pageErrors: async ({ page }, use) => {
    const errors: string[] = [];
    page.on('pageerror', (error) => errors.push(error.message));
    await use(errors);
  },
});

export { expect };

/** Asia/Tokyo（テストの端末とデモの利用者のタイムゾーン）での `YYYY-MM-DD`。 */
export function tokyoDate(value: string | number | Date): string {
  return new Intl.DateTimeFormat('sv-SE', {
    timeZone: 'Asia/Tokyo',
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
  }).format(new Date(value));
}

/** Asia/Tokyo での `YYYY-MM-DDTHH:mm`（datetime-local の値）。 */
export function tokyoLocalInput(value: string | number | Date): string {
  const parts = new Intl.DateTimeFormat('sv-SE', {
    timeZone: 'Asia/Tokyo',
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hourCycle: 'h23',
  }).format(new Date(value));
  return parts.replace(' ', 'T');
}
