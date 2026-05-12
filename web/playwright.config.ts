import { defineConfig, devices } from '@playwright/test';

export default defineConfig({
  // testDir is the web/ root so Playwright can discover both the
  // imperative suite under `e2e/` and the BDD suite under `tests/`
  // (introduced in US-002). testIgnore excludes the Vitest unit tests
  // under `src/` and dependency / build artefacts.
  testDir: '.',
  testMatch: /(?:e2e|tests)\/.*\.spec\.ts$/,
  testIgnore: ['src/**', 'node_modules/**', 'dist/**', 'coverage/**', 'playwright-report/**'],
  fullyParallel: false,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  workers: 1,
  reporter: 'html',
  timeout: 30_000,

  use: {
    baseURL: 'http://localhost:5173',
    trace: 'on-first-retry',
    screenshot: 'only-on-failure',
  },

  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],

  webServer: {
    command: 'npm run dev',
    url: 'http://localhost:5173',
    reuseExistingServer: !process.env.CI,
    timeout: 30_000,
  },
});
