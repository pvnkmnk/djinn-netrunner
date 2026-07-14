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
    command: 'docker compose -f docker-compose.yml -f docker-compose.e2e.yml up -d --build',
    url: 'http://localhost:8080/api/health',
    reuseExistingServer: !process.env.CI,
    timeout: 180 * 1000, // 3 min for Docker build + startup
    stdout: 'pipe',
    stderr: 'pipe',
    cwd: '/home/idols/orca/workspaces/netrunner_repo/auto-release-readiness-review-run-1-20260616T0835',
    env: {
      // Docker compose env vars (from .env or inline)
      POSTGRES_PASSWORD: 'testpass',
      SLSKD_USERNAME: 'testuser',
      SLSKD_PASSWORD: 'testpass',
      SLSKD_API_KEY: 'test-api-key',
      JWT_SECRET: 'test-secret-key-for-e2e-testing',
      ENVIRONMENT: 'development',
    },
  },

  globalTeardown: './teardown.ts',
});
