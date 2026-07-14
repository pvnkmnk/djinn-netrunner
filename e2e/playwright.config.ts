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
    command: 'docker compose --env-file ../.env.e2e -f ../docker-compose.yml -f ../docker-compose.e2e.yml up -d --build && ./setup-test-db.sh',
    url: 'http://localhost:8080/api/health',
    reuseExistingServer: !process.env.CI,
    timeout: 180 * 1000, // 3 min for Docker build + startup
    stdout: 'pipe',
    stderr: 'pipe',
    cwd: '/home/idols/orca/workspaces/netrunner_repo/auto-release-readiness-review-run-1-20260616T0835',
  },

  globalTeardown: './teardown.ts',
});
