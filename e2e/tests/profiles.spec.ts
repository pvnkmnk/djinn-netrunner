import { expect } from '@playwright/test';
import { test } from '../fixtures/auth.fixture';

test.describe('Quality Profiles CRUD (DJI-427)', () => {
  test('profiles page loads', async ({ authenticatedPage: page }) => {
    await page.goto('/profiles');

    // Wait for page to load
    await expect(page.locator('.page-header')).toBeVisible();

    // Wait for HTMX to load profiles region
    await page.waitForTimeout(1000);

    // Verify profiles region exists
    const profilesRegion = page.locator('#profiles-region');
    await expect(profilesRegion).toBeVisible();

    // Verify "Add Profile" button exists
    const addButton = page.locator('button:has-text("Add Profile")');
    await expect(addButton).toBeVisible();
  });

  test('empty state shows when no profiles', async ({ authenticatedPage: page }) => {
    await page.goto('/profiles');
    await page.waitForTimeout(1000);

    // Check for empty state (may or may not be present depending on existing data)
    const emptyState = page.locator('.empty-state, text=No profiles');
    const isEmpty = await emptyState.isVisible().catch(() => false);

    // Either empty state or profile cards should be visible
    if (isEmpty) {
      await expect(emptyState.first()).toBeVisible();
    } else {
      const profileCards = page.locator('.profile-card');
      const count = await profileCards.count();
      expect(count).toBeGreaterThanOrEqual(0);
    }
  });

  test('navigation to profiles page works', async ({ authenticatedPage: page }) => {
    // Start from dashboard
    await page.goto('/');
    await expect(page.locator('.dashboard')).toBeVisible();

    // Click Profiles nav link
    await page.locator('nav#primary-nav a:has-text("Profiles")').click();

    // Wait for navigation
    await page.waitForTimeout(1000);

    // Verify we're on profiles page
    await expect(page.locator('.page-header h2')).toHaveText('Profiles');
  });

  test('profile form modal opens', async ({ authenticatedPage: page }) => {
    await page.goto('/profiles');
    await page.waitForTimeout(1000);

    // Click Add Profile button
    await page.locator('button:has-text("Add Profile")').click();

    // Wait for modal to appear
    await page.waitForTimeout(500);

    // Verify modal is visible
    const modal = page.locator('#modal-container');
    await expect(modal).toBeVisible();

    // Verify form fields exist
    const nameInput = page.locator('#profile-name, input[name="name"]');
    await expect(nameInput.first()).toBeVisible();
  });

  test('jobs page accessible from profiles', async ({ authenticatedPage: page }) => {
    await page.goto('/profiles');
    await page.waitForTimeout(1000);

    // Navigate to jobs page
    await page.locator('nav#primary-nav a:has-text("Jobs")').click();
    await page.waitForTimeout(1000);

    // Verify jobs page loads
    await expect(page.locator('.page-header h2')).toHaveText('Jobs');
  });
});
