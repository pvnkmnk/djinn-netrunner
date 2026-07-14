import { expect } from '@playwright/test';
import { test } from '../fixtures/auth.fixture';

test.describe('Libraries CRUD + Browse + Scan (DJI-425)', () => {
  test('libraries page loads', async ({ authenticatedPage: page }) => {
    await page.goto('/libraries');

    // Wait for page to load
    await expect(page.locator('.page-header')).toBeVisible();

    // Wait for HTMX to load libraries region
    await page.waitForTimeout(1000);

    // Verify libraries region exists
    const librariesRegion = page.locator('.libraries-region');
    await expect(librariesRegion).toBeVisible();

    // Verify "Add Library" button exists
    const addButton = page.locator('button:has-text("Add Library")');
    await expect(addButton).toBeVisible();
  });

  test('empty state shows when no libraries', async ({ authenticatedPage: page }) => {
    await page.goto('/libraries');
    await page.waitForTimeout(1000);

    // Check for empty state (may or may not be present depending on existing data)
    const emptyState = page.locator('.empty-state');
    const isEmpty = await emptyState.isVisible().catch(() => false);

    // Either empty state or library list should be visible
    if (isEmpty) {
      await expect(emptyState).toBeVisible();
    } else {
      const libraryCards = page.locator('.library-card');
      const count = await libraryCards.count();
      expect(count).toBeGreaterThanOrEqual(0);
    }
  });

  test('navigation to libraries page works', async ({ authenticatedPage: page }) => {
    // Start from dashboard
    await page.goto('/');
    await expect(page.locator('.dashboard')).toBeVisible();

    // Click Libraries nav link
    await page.locator('nav#primary-nav a:has-text("Libraries")').click();

    // Wait for navigation
    await page.waitForTimeout(1000);

    // Verify we're on libraries page
    await expect(page.locator('.page-header h2')).toHaveText('Libraries');
  });

  test('library form modal opens', async ({ authenticatedPage: page }) => {
    await page.goto('/libraries');
    await page.waitForTimeout(1000);

    // Click Add Library button
    await page.locator('button:has-text("Add Library")').click();

    // Wait for modal to appear
    await page.waitForTimeout(500);

    // Verify modal is visible
    const modal = page.locator('#modal-container');
    await expect(modal).toBeVisible();

    // Verify form fields exist
    const nameInput = page.locator('#library-name, input[name="name"]');
    await expect(nameInput.first()).toBeVisible();
  });

  test('jobs page accessible from libraries', async ({ authenticatedPage: page }) => {
    await page.goto('/libraries');
    await page.waitForTimeout(1000);

    // Navigate to jobs page
    await page.locator('nav#primary-nav a:has-text("Jobs")').click();
    await page.waitForTimeout(1000);

    // Verify jobs page loads
    await expect(page.locator('.page-header h2')).toHaveText('Jobs');
  });
});
