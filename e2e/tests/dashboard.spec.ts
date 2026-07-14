import { expect } from '@playwright/test';
import { test } from '../fixtures/auth.fixture';

test.describe('Dashboard (DJI-424)', () => {
  test('dashboard loads for authenticated user', async ({ authenticatedPage: page }) => {
    await page.goto('/');

    // Wait for dashboard to load
    await expect(page.locator('.dashboard')).toBeVisible({ timeout: 10000 });

    // Verify the page title
    await expect(page).toHaveTitle(/Dashboard/i);
  });

  test('stats region is present', async ({ authenticatedPage: page }) => {
    await page.goto('/');

    // Wait for dashboard to load
    await expect(page.locator('.dashboard')).toBeVisible({ timeout: 10000 });

    // Verify stats region exists (HTMX will populate it)
    const statsRegion = page.locator('.stats-region');
    await expect(statsRegion).toBeVisible();
  });

  test('watchlists region is present', async ({ authenticatedPage: page }) => {
    await page.goto('/');

    // Wait for dashboard to load
    await expect(page.locator('.dashboard')).toBeVisible({ timeout: 10000 });

    // Verify watchlists region exists
    const watchlistsRegion = page.locator('.watchlists-region');
    await expect(watchlistsRegion).toBeVisible();
  });

  test('console region is present', async ({ authenticatedPage: page }) => {
    await page.goto('/');

    // Wait for dashboard to load
    await expect(page.locator('.dashboard')).toBeVisible({ timeout: 10000 });

    // Verify console region exists
    const consoleRegion = page.locator('.console-region');
    await expect(consoleRegion).toBeVisible();
  });

  test('navigation links are present', async ({ authenticatedPage: page }) => {
    await page.goto('/');

    // Wait for dashboard to load
    await expect(page.locator('.dashboard')).toBeVisible({ timeout: 10000 });

    // Verify main navigation links
    const navLinks = page.locator('nav#primary-nav a');
    await expect(navLinks.first()).toBeVisible();

    const count = await navLinks.count();
    expect(count).toBeGreaterThan(0);
  });
});
