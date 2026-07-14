import { defineConfig, devices } from '@playwright/test';

export default defineConfig({
  testDir: './tests',
  fullyParallel: false,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  workers: 1,
  reporter: 'html',

  use: {
    baseURL: 'http://localhost:8080',
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
    command: './setup-test-db.sh',
    url: 'http://localhost:8080/api/health',
    reuseExistingServer: !process.env.CI,
    timeout: 300 * 1000, // 5 min for Docker build + startup
    stdout: 'pipe',
    stderr: 'pipe',
    // No explicit cwd — Playwright defaults to config file's dir (e2e/), so ../ paths resolve to repo root
  },

  globalTeardown: './teardown.ts',
});
