import { defineConfig, devices } from '@playwright/test';
import { existsSync } from 'fs';
import path from 'path';

// The bring-up lives in e2e/setup-test-db.sh and nowhere else. Windows cannot
// run it as written: Playwright spawns the webServer command through cmd.exe,
// which reads './setup-test-db.sh' as a program name ("'.' is not recognized"),
// and Git for Windows puts bash outside the PATH cmd.exe sees. Resolving the
// interpreter explicitly keeps `scripts/e2e.sh test` a one-command run on the
// operator's Windows checkout without duplicating the bring-up in Node.
const SETUP_SCRIPT = path.join(__dirname, 'setup-test-db.sh');

function setupCommand(): string {
  if (process.platform !== 'win32') return './setup-test-db.sh';
  const gitBash = [
    'C:\\Program Files\\Git\\bin\\bash.exe',
    'C:\\Program Files\\Git\\usr\\bin\\bash.exe',
    'C:\\Program Files (x86)\\Git\\bin\\bash.exe',
  ].find(existsSync);
  return gitBash ? `"${gitBash}" "${SETUP_SCRIPT}"` : 'bash ./setup-test-db.sh';
}

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
    command: setupCommand(),
    url: 'http://localhost:8080/api/health',
    reuseExistingServer: !process.env.CI,
    timeout: 300 * 1000, // 5 min for Docker build + startup
    stdout: 'pipe',
    stderr: 'pipe',
    cwd: __dirname, // Always run from e2e/ directory regardless of where playwright is invoked
  },

  globalTeardown: './teardown.ts',
});
