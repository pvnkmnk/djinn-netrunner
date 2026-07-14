import { test as base, type Page } from '@playwright/test';

type AuthFixtures = {
  authenticatedPage: Page;
  adminPage: Page;
};

// Shared credentials
const TEST_USER = { email: 'e2e-test@netrunner.dev', password: 'testpass123' };
const ADMIN_USER = { email: 'e2e-admin@netrunner.dev', password: 'admin123' };

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

    await use(page);
    await context.close();
  },
});
