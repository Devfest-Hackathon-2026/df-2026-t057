import { defineConfig } from '@playwright/test';

const port = Number(process.env.E2E_PORT ?? 4200);
const baseURL = `http://127.0.0.1:${port}`;

export default defineConfig({
  testDir: './tests/e2e',
  fullyParallel: true,
  timeout: 30000,
  use: {
    baseURL,
    viewport: { width: 1440, height: 1080 },
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
  webServer: {
    command: `cd api && APP_MODE=demo APP_URL=${baseURL} PORT=${port} go run ./cmd/server`,
    url: `${baseURL}/api/health`,
    reuseExistingServer: !process.env.CI,
    timeout: 120000,
  },
});
