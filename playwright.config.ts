import { defineConfig } from '@playwright/test';

export default defineConfig({
  testDir: './tests/e2e',
  fullyParallel: true,
  timeout: 30000,
  use: {
    baseURL: 'http://127.0.0.1:4200',
    viewport: { width: 1440, height: 1080 },
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
  webServer: {
    command: 'cd api && APP_MODE=demo PORT=4200 go run ./cmd/server',
    url: 'http://127.0.0.1:4200/api/health',
    reuseExistingServer: !process.env.CI,
    timeout: 120000,
  },
});
