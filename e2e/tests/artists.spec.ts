import { expect } from '@playwright/test';
import { test } from '../fixtures/auth.fixture';

/**
 * Monitored Artists (DJI-428).
 *
 * Adding an artist resolves it against MusicBrainz (network, third-party
 * availability), so the create path is asserted where it is deterministic —
 * request validation and the form's wiring — rather than by driving a live
 * lookup. Everything else here is local to the app.
 */
test.describe('Artists Feature (DJI-428)', () => {
  async function getCsrfToken(page: any): Promise<string> {
    const cookies = await page.context().cookies();
    return cookies.find((c: any) => c.name === 'csrf_')?.value || '';
  }

  async function listArtists(page: any) {
    return page.request.get('/api/artists');
  }

  // The e2e database is dropped and recreated per run (e2e/setup-test-db.sh), so
  // the artists list starts empty and nothing here needs a cleanup pass.
  test('1. Page loads - /artists shows the page header and the HTMX region', async ({ authenticatedPage: page }) => {
    await page.goto('/artists');

    await expect(page.locator('.page-header')).toBeVisible();
    await expect(page.locator('.page-header h2')).toHaveText('Monitored Artists');
    await expect(page.locator('#artists-region')).toBeVisible();
  });

  test('2. Partial loads - HTMX fills the region from /partials/artists', async ({ authenticatedPage: page }) => {
    const partialResponse = page.waitForResponse(resp => resp.url().includes('/partials/artists') && resp.status() === 200);
    await page.goto('/artists');
    await partialResponse;

    await expect(page.locator('.artists-region')).toBeVisible();
  });

  test('3. Empty state - a fresh install says nothing is monitored', async ({ authenticatedPage: page }) => {
    await page.goto('/artists');
    await page.waitForTimeout(500);

    await expect(page.locator('.empty-state')).toHaveText('No artists being monitored');
  });

  test('4. Add Artist opens the modal with the name and profile fields', async ({ authenticatedPage: page }) => {
    await page.goto('/artists');
    await page.waitForTimeout(500);

    await page.locator('button:has-text("Add Artist")').click();
    await page.waitForTimeout(500);

    await expect(page.locator('#modal-container')).toBeVisible();
    await expect(page.locator('#modal-container #name')).toBeVisible();
    await expect(page.locator('#modal-container #quality_profile_id')).toBeVisible();
    await expect(page.locator('#modal-container h3')).toHaveText('Add Artist');
  });

  test('5. The add form posts to /api/artists and requires a name', async ({ authenticatedPage: page }) => {
    await page.goto('/artists');
    await page.waitForTimeout(500);

    await page.locator('button:has-text("Add Artist")').click();
    await page.waitForTimeout(500);

    const form = page.locator('#modal-container form[hx-post="/api/artists"]');
    await expect(form).toBeVisible();
    // A name is the only input the endpoint accepts; an empty submit must not reach it.
    await expect(form.locator('#name')).toHaveAttribute('required', '');
  });

  test('6. Cancel closes the modal without creating anything', async ({ authenticatedPage: page }) => {
    await page.goto('/artists');
    await page.waitForTimeout(500);

    await page.locator('button:has-text("Add Artist")').click();
    await page.waitForTimeout(500);
    await expect(page.locator('#modal-container')).toBeVisible();

    await page.locator('#modal-container button:has-text("Cancel")').click();
    await page.waitForTimeout(500);

    await expect(page.locator('#modal-container')).not.toBeVisible();
    const response = await listArtists(page);
    expect(await response.json()).toEqual([]);
  });

  test('7. List API returns an array for the authenticated user', async ({ authenticatedPage: page }) => {
    const response = await listArtists(page);

    expect(response.status()).toBe(200);
    const body = await response.json();
    expect(Array.isArray(body)).toBe(true);
    expect(body).toHaveLength(0);
  });

  test('8. Add rejects a missing name with 400 - no lookup is attempted', async ({ authenticatedPage: page }) => {
    const csrfToken = await getCsrfToken(page);
    const response = await page.request.post('/api/artists', {
      data: { name: '' },
      headers: { 'X-CSRF-Token': csrfToken }
    });

    expect(response.status()).toBe(400);
    expect((await response.json()).error).toBe('name is required');
  });

  test('9. Sync reports 404 for an artist that is not monitored', async ({ authenticatedPage: page }) => {
    const csrfToken = await getCsrfToken(page);
    const response = await page.request.post('/api/artists/11111111-2222-4333-8444-555555555555/sync', {
      data: {},
      headers: { 'X-CSRF-Token': csrfToken }
    });

    expect(response.status()).toBe(404);
    expect((await response.json()).error).toBe('artist not found');
  });

  test('10. Navigation - the nav link reaches /artists from the dashboard', async ({ authenticatedPage: page }) => {
    await page.goto('/');
    await expect(page.locator('.dashboard')).toBeVisible();

    await page.locator('nav#primary-nav a:has-text("Artists")').click();
    await page.waitForTimeout(1000);

    await expect(page).toHaveURL(/\/artists$/);
    await expect(page.locator('.page-header h2')).toHaveText('Monitored Artists');
  });

  test('11. Authorization - an unauthenticated request is refused', async ({ browser }) => {
    const context = await browser.newContext();
    const page = await context.newPage();

    const response = await page.request.get('/api/artists');

    expect(response.status()).toBe(401);
    await context.close();
  });
});
