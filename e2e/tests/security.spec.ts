import { expect, test } from '../support/fixtures';
import { workspace } from '../support/pages';
import { enableAi, fulfillProblem } from '../support/routes';

declare global {
  interface Window {
    __cspDirectives?: string[];
    __injectedByFixture?: boolean;
  }
}

test('CSP preserves the calendar and dialogs while blocking injected inline scripts', async ({
  page,
}) => {
  await page.addInitScript(() => {
    const violations: string[] = [];
    window.__cspDirectives = violations;
    document.addEventListener('securitypolicyviolation', (event) => {
      violations.push(event.effectiveDirective);
    });
  });
  const violations = () => page.evaluate(() => window.__cspDirectives ?? []);
  const response = await page.goto('/calendar');
  const policy = response?.headers()['content-security-policy'] ?? '';
  expect(policy).toContain("'strict-dynamic'");
  expect(policy).not.toContain("'unsafe-eval'");
  const nonce = /'nonce-([^']+)'/.exec(policy)?.[1] ?? '';
  expect(nonce).not.toBe('');

  const home = workspace(page);
  await expect(home.calendar).toBeVisible();
  await home.creditHelp.click();
  await expect(page.getByRole('dialog', { name: '視聴時間について' })).toBeVisible();
  await page.keyboard.press('Escape');
  await page.getByRole('button', { name: '課題を登録', exact: true }).click();
  await expect(page.getByRole('dialog', { name: '課題を登録' })).toBeVisible();
  expect(await violations()).toEqual([]);
  // Angular が入れる <style> には、ページの nonce が付いている。
  expect(
    await page
      .locator('style')
      .evaluateAll((styles, value) => styles.every((style) => style.nonce === value), nonce),
  ).toBe(true);

  await page.route(page.url(), async (route) => {
    const original = await route.fetch();
    const body = (await original.text()).replace(
      '</head>',
      '<script>window.__injectedByFixture = true</script></head>',
    );
    await route.fulfill({ response: original, body });
  });
  const reloaded = await page.reload();
  expect(await reloaded?.text()).toContain('window.__injectedByFixture = true');
  expect(await page.evaluate(() => window.__injectedByFixture)).toBeUndefined();
  await expect.poll(violations).toContain('script-src-elem');
  // nonce は読み込みごとに変わる。
  expect(reloaded?.headers()['content-security-policy']).not.toContain(`'nonce-${nonce}'`);
});

test('privacy and API responses enforce their own resource restrictions', async ({ request }) => {
  const privacy = await request.get('/privacy.html');
  expect(privacy.status()).toBe(200);
  expect(privacy.headers()['content-security-policy']).toContain("script-src 'none'");
  expect(await privacy.text()).toContain('href="/privacy.css"');
  expect((await request.get('/privacy.css')).status()).toBe(200);
  const api = await request.get('/api/health');
  expect(api.headers()['content-security-policy']).toContain("default-src 'none'");
});

test('an AI limit preserves the message and existing tasks', async ({ page, api }) => {
  await enableAi(page);
  let calls = 0;
  await page.route('**/api/assistant/turns', async (route) => {
    if (route.request().method() !== 'POST') {
      await route.fallback();
      return;
    }
    calls += 1;
    await fulfillProblem(route, 429, 'ai.rate_limited', { 'Retry-After': '60' });
  });
  await page.goto('/calendar');
  await page.getByRole('radio', { name: '相談', exact: true }).click();
  const composer = workspace(page).composer;
  await composer.fill('この相談を消さずに残しておく');
  await page.getByRole('button', { name: '送信', exact: true }).click();
  await expect(
    page.getByText('AIの利用上限に達しました。少し待ってから再試行してください。', {
      exact: true,
    }),
  ).toBeVisible();
  await expect(composer).toHaveValue('この相談を消さずに残しておく');
  await expect(page.getByRole('button', { name: '同じ相談を再試行', exact: true })).toBeVisible();
  expect(calls).toBe(1);
  expect((await api.tasks()).items).toEqual([]);
  expect((await api.wallet()).balanceSeconds).toBe(600);
});
