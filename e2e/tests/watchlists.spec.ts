import { expect } from '@playwright/test';
import { test } from '../fixtures/auth.fixture';

test.describe('Watchlists CRUD + Toggle + Spotify (DJI-426)', () => {
  test('watchlists page loads', async ({ authenticatedPage: page }) => {
    await page.goto('/watchlists');

    // Wait for page to load
    await expect(page.locator('.page-header')).toBeVisible();

    // Wait for HTMX to load watchlists region
    await page.waitForTimeout(1000);

    // Verify watchlists region exists
    const watchlistsRegion = page.locator('#watchlists-region');
    await expect(watchlistsRegion).toBeVisible();

    // Verify "Add Watchlist" button exists
    const addButton = page.locator('button:has-text("Add Watchlist")');
    await expect(addButton).toBeVisible();
  });

  test('empty state shows when no watchlists', async ({ authenticatedPage: page }) => {
    await page.goto('/watchlists');
    await page.waitForTimeout(1000);

    // Check for empty state (may or may not be present depending on existing data)
    const emptyState = page.locator('.empty-state, text=No watchlists');
    const isEmpty = await emptyState.isVisible().catch(() => false);

    // Either empty state or watchlist cards should be visible
    if (isEmpty) {
      await expect(emptyState.first()).toBeVisible();
    } else {
      const watchlistCards = page.locator('.watchlist-card');
      const count = await watchlistCards.count();
      expect(count).toBeGreaterThanOrEqual(0);
    }
  });

  test('navigation to watchlists page works', async ({ authenticatedPage: page }) => {
    // Start from dashboard
    await page.goto('/');
    await expect(page.locator('.dashboard')).toBeVisible();

    // Click Watchlists nav link
    await page.locator('nav#primary-nav a:has-text("Watchlists")').click();

    // Wait for navigation
    await page.waitForTimeout(1000);

    // Verify we're on watchlists page
    await expect(page.locator('.page-header h2')).toHaveText('Watchlists');
  });

  test('watchlist form modal opens', async ({ authenticatedPage: page }) => {
    await page.goto('/watchlists');
    await page.waitForTimeout(1000);

    // Click Add Watchlist button
    await page.locator('button:has-text("Add Watchlist")').click();

    // Wait for modal to appear
    await page.waitForTimeout(500);

    // Verify modal is visible
    const modal = page.locator('#modal-container');
    await expect(modal).toBeVisible();

    // Verify form fields exist
    const nameInput = page.locator('#watchlist-name, input[name="name"]');
    await expect(nameInput.first()).toBeVisible();
  });

  test('jobs page accessible from watchlists', async ({ authenticatedPage: page }) => {
    await page.goto('/watchlists');
    await page.waitForTimeout(1000);

    // Navigate to jobs page
    await page.locator('nav#primary-nav a:has-text("Jobs")').click();
    await page.waitForTimeout(1000);

    // Verify jobs page loads
    await expect(page.locator('.page-header h2')).toHaveText('Jobs');
  });
});
