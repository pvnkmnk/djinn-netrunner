import { test as base, type Page } from '@playwright/test';
import { execSync } from 'child_process';
import path from 'path';

type AuthFixtures = {
  authenticatedPage: Page;
  adminPage: Page;
};

// Shared credentials
const TEST_USER = { email: 'e2e-test@netrunner.dev', password: 'testpass123' };
const ADMIN_USER = { email: 'e2e-admin@netrunner.dev', password: 'admin123' };
const REPO_ROOT = path.resolve(__dirname, '..');

async function loginViaAPI(page: Page, user: { email: string; password: string }) {
  // Visit page to get CSRF cookie
  await page.goto('/');
  const cookies = await page.context().cookies();
  const csrfToken = cookies.find(c => c.name === 'csrf_')?.value || '';

  // Login via API
  const loginResponse = await page.request.post('/api/auth/login', {
    data: { email: user.email, password: user.password },
    headers: { 'X-CSRF-Token': csrfToken }
  });

  if (!loginResponse.ok()) {
    throw new Error(`Login failed: ${loginResponse.status()}`);
  }

  // Navigate to dashboard to verify login worked
  await page.goto('/');
  await page.waitForSelector('.dashboard', { timeout: 5000 });
}

async function ensureUserExists(page: Page, user: { email: string; password: string }) {
  // Visit page to get CSRF cookie
  await page.goto('/');
  const cookies = await page.context().cookies();
  const csrfToken = cookies.find(c => c.name === 'csrf_')?.value || '';

  // Try to register (may fail if user already exists)
  const registerResponse = await page.request.post('/api/auth/register', {
    data: { email: user.email, password: user.password },
    headers: { 'X-CSRF-Token': csrfToken }
  });
  
  // 201 = created, 409 = already exists (both fine)
  if (!registerResponse.ok() && registerResponse.status() !== 409) {
    throw new Error(`Registration failed: ${registerResponse.status()}`);
  }
}

function promoteAdminUser(): void {
  try {
    // Try docker exec with the known container name first
    execSync(
      `docker exec netrunner-postgres psql -U musicops -d musicops_test -c "UPDATE users SET role='admin' WHERE email='e2e-admin@netrunner.dev';"`,
      { timeout: 15000, stdio: 'pipe' }
    );
  } catch (e1: any) {
    console.warn('Could not promote admin user via docker exec:', e1.message);
    // Fallback: try docker compose exec (handles different container name schemes)
    try {
      execSync(
        `docker compose --env-file ../.env.e2e -f ../docker-compose.yml -f ../docker-compose.e2e.yml exec -T postgres psql -U musicops -d musicops_test -c "UPDATE users SET role='admin' WHERE email='e2e-admin@netrunner.dev';"`,
        { cwd: path.resolve(__dirname, '..'), timeout: 15000, stdio: 'pipe' }
      );
    } catch (e2: any) {
      console.warn('Could not promote admin user via compose exec:', e2.message);
    }
  }
}

export const test = base.extend<AuthFixtures>({
  authenticatedPage: async ({ browser }, use) => {
    const context = await browser.newContext();
    const page = await context.newPage();

    // Ensure user exists
    await ensureUserExists(page, TEST_USER);
    
    // Login fresh for this test
    await loginViaAPI(page, TEST_USER);

    await use(page);
    await context.close();
  },

  adminPage: async ({ browser }, use) => {
    const context = await browser.newContext();
    const page = await context.newPage();

    // Ensure admin exists
    await ensureUserExists(page, ADMIN_USER);
    
    // Login fresh for this test
    await loginViaAPI(page, ADMIN_USER);

    // Promote admin role in database
    promoteAdminUser();

    await use(page);
    await context.close();
  },
});
