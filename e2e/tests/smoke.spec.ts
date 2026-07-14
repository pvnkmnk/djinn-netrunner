import { test, expect } from '@playwright/test';

test.describe('Smoke Tests', () => {
  test('health endpoint returns 200', async ({ request }) => {
    const response = await request.get('/api/health');
    expect(response.ok()).toBeTruthy();
    const body = await response.json();
    expect(body.status).toBe('ok');
    expect(body.checks.database.status).toBe('ok');
  });

  test('dashboard page loads', async ({ page }) => {
    await page.goto('/');
    // Should redirect to login or show dashboard
    await expect(page).toHaveURL(/\/(login)?/);
  });

  test('login page renders', async ({ page }) => {
    await page.goto('/');
    // Check for login form elements
    const emailInput = page.locator('input[name="email"], input[type="email"]');
    const passwordInput = page.locator('input[name="password"], input[type="password"]');
    await expect(emailInput.first()).toBeVisible({ timeout: 10000 });
    await expect(passwordInput.first()).toBeVisible();
  });
});
