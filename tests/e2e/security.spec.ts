import { test, expect } from '@playwright/test';

test('CSP preserves the calendar and dialogs while blocking injected inline scripts', async ({
  page,
}) => {
  await page.addInitScript(() => {
    const violations: string[] = [];
    Object.defineProperty(window, '__cspDirectives', { value: violations });
    document.addEventListener('securitypolicyviolation', (event) => {
      violations.push(event.effectiveDirective);
    });
  });
  const violations = () =>
    page.evaluate(() => (window as Window & { __cspDirectives: string[] }).__cspDirectives);
  const response = await page.goto('/');
  const policy = response!.headers()['content-security-policy'];
  expect(policy).toContain("'strict-dynamic'");
  expect(policy).not.toContain("'unsafe-eval'");
  const nonce = policy.match(/'nonce-([^']+)'/)![1];
  await expect(page.locator('app-calendar-workspace')).toBeVisible();
  await page.getByRole('button', { name: '課題を登録', exact: true }).click();
  await expect(page.getByRole('dialog')).toBeVisible();
  expect(await violations()).toEqual([]);
  expect(
    await page
      .locator('style')
      .evaluateAll((styles, nonce) => styles.every((s) => s.nonce === nonce), nonce),
  ).toBe(true);
  await page.route(page.url(), async (route) => {
    const response = await route.fetch();
    const body = (await response.text()).replace(
      '</head>',
      '<script>window.__injectedByFixture = true</script></head>',
    );
    await route.fulfill({ response, body });
  });
  const reloaded = await page.reload();
  expect(await reloaded!.text()).toContain('window.__injectedByFixture = true');
  expect(
    await page.evaluate(
      () => (window as Window & { __injectedByFixture?: boolean }).__injectedByFixture,
    ),
  ).toBeUndefined();
  await expect.poll(violations).toContain('script-src-elem');
  expect(reloaded!.headers()['content-security-policy']).not.toContain(`'nonce-${nonce}'`);
});

test('privacy and API responses enforce their own resource restrictions', async ({ request }) => {
  const privacy = await request.get('/privacy.html');
  expect(privacy.headers()['content-security-policy']).toContain("script-src 'none'");
  expect(await privacy.text()).toContain('href="/privacy.css"');
  expect((await request.get('/privacy.css')).status()).toBe(200);
  const api = await request.get('/api/health');
  expect(api.headers()['content-security-policy']).toContain("default-src 'none'");
});

test('an AI limit preserves the message and existing tasks', async ({ page }) => {
  await page.route('**/api/config', async (route) => {
    const response = await route.fetch();
    await route.fulfill({ response, json: { ...(await response.json()), aiEnabled: true } });
  });
  const error = 'AIの利用上限に達しました。時間をおいて再試行してください。';
  await page.route('**/api/assistant/messages', (route) =>
    route.fulfill({ status: 429, headers: { 'Retry-After': '60' }, json: { error } }),
  );
  await page.goto('/');
  await page.getByRole('radio', { name: '相談', exact: true }).click();
  const composer = page.getByLabel('相談する', { exact: true });
  await composer.fill('この相談を消さずに残しておく');
  await page.getByRole('button', { name: '送信', exact: true }).click();
  await expect(page.getByText(error, { exact: true })).toBeVisible();
  await expect(composer).toHaveValue('この相談を消さずに残しておく');
  const state = await (await page.request.get('/api/state')).json();
  expect(state.tasks).toEqual([]);
  expect(state.balanceSeconds).toBe(600);
});
