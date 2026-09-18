import { expect } from '@playwright/test';
import { test } from '../fixtures/auth.fixture';

/**
 * Playlists (DJI-430).
 *
 * The whole surface is local — create, rename, delete, and the HTMX region that
 * renders them — so unlike the Artists suite this one drives the real CRUD path.
 */
test.describe('Playlists Feature (DJI-430)', () => {
  async function getCsrfToken(page: any): Promise<string> {
    const cookies = await page.context().cookies();
    return cookies.find((c: any) => c.name === 'csrf_')?.value || '';
  }

  async function listPlaylists(page: any) {
    return page.request.get('/api/playlists');
  }

  async function createPlaylist(page: any, name: string, description = '') {
    const csrfToken = await getCsrfToken(page);
    return page.request.post('/api/playlists', {
      data: { name, description },
      headers: { 'X-CSRF-Token': csrfToken }
    });
  }

  async function deletePlaylist(page: any, id: string) {
    const csrfToken = await getCsrfToken(page);
    return page.request.delete(`/api/playlists/${id}`, {
      headers: { 'X-CSRF-Token': csrfToken }
    });
  }

  // The e2e database is recreated per run, but a spec that fails midway would
  // otherwise leave rows behind for the next one.
  async function removeAllPlaylists(page: any) {
    const response = await listPlaylists(page);
    if (!response.ok()) return;
    const body = await response.json();
    for (const playlist of body || []) {
      await deletePlaylist(page, playlist.id || playlist.ID);
    }
  }

  test.beforeEach(async ({ authenticatedPage: page }) => {
    await removeAllPlaylists(page);
  });

  test.afterEach(async ({ authenticatedPage: page }) => {
    await removeAllPlaylists(page);
  });

  test('1. Page loads - /playlists shows the page header and the HTMX region', async ({ authenticatedPage: page }) => {
    await page.goto('/playlists');

    await expect(page.locator('.page-header h2')).toHaveText('Playlists');
    // Exactly one element owns this id: the partial used to declare it too,
    // which made every id-based selector ambiguous.
    await expect(page.locator('#playlists-region')).toHaveCount(1);
    await expect(page.locator('#playlists-region')).toBeVisible();
  });

  test('2. Partial loads - HTMX fills the region from /partials/playlists', async ({ authenticatedPage: page }) => {
    const partialResponse = page.waitForResponse(resp => resp.url().includes('/partials/playlists') && resp.status() === 200);
    await page.goto('/playlists');
    await partialResponse;

    await expect(page.locator('.playlists-region')).toBeVisible();
  });

  test('3. Empty state - no playlists yet', async ({ authenticatedPage: page }) => {
    await page.goto('/playlists');
    await page.waitForTimeout(500);

    await expect(page.locator('.empty-state')).toHaveText('No playlists yet');
  });

  test('4. Create via API returns 201 with the playlist', async ({ authenticatedPage: page }) => {
    const response = await createPlaylist(page, 'API Playlist', 'made by the suite');

    expect(response.status()).toBe(201);
    const body = await response.json();
    expect(body.id).toBeTruthy();
    expect(body.name).toBe('API Playlist');
    expect(body.description).toBe('made by the suite');
  });

  test('5. Created playlist appears as a card in the UI', async ({ authenticatedPage: page }) => {
    expect((await createPlaylist(page, 'Visible Playlist')).status()).toBe(201);

    const partialResponse = page.waitForResponse(resp => resp.url().includes('/partials/playlists') && resp.status() === 200);
    await page.goto('/playlists');
    await partialResponse;

    const card = page.locator('.playlist-card:has-text("Visible Playlist")');
    await expect(card).toBeVisible();
    await expect(card.locator('.name')).toHaveText('Visible Playlist');
    await expect(card.locator('.meta')).toHaveText('Private');
  });

  test('6. Create with no name is rejected with 400', async ({ authenticatedPage: page }) => {
    const response = await createPlaylist(page, '');

    expect(response.status()).toBe(400);
    expect((await response.json()).error).toBe('name is required');
  });

  test('7. Rename via PATCH shows the new name in the UI', async ({ authenticatedPage: page }) => {
    const created = await createPlaylist(page, 'Before Rename');
    const playlist = await created.json();

    const csrfToken = await getCsrfToken(page);
    const patched = await page.request.patch(`/api/playlists/${playlist.id}`, {
      data: { name: 'After Rename' },
      headers: { 'X-CSRF-Token': csrfToken }
    });
    expect(patched.status()).toBe(200);

    const partialResponse = page.waitForResponse(resp => resp.url().includes('/partials/playlists') && resp.status() === 200);
    await page.goto('/playlists');
    await partialResponse;

    await expect(page.locator('.playlist-card:has-text("After Rename")')).toBeVisible();
    await expect(page.locator('.playlist-card:has-text("Before Rename")')).toHaveCount(0);
  });

  test('8. Delete from the card removes it, confirm dialog and all', async ({ authenticatedPage: page }) => {
    const created = await createPlaylist(page, 'Delete Me');
    const playlist = await created.json();

    const partialResponse = page.waitForResponse(resp => resp.url().includes('/partials/playlists') && resp.status() === 200);
    await page.goto('/playlists');
    await partialResponse;

    const card = page.locator('.playlist-card:has-text("Delete Me")');
    await expect(card).toBeVisible();

    page.on('dialog', dialog => dialog.accept());
    await card.locator('button:has-text("Delete")').click();
    await page.waitForTimeout(1000);

    await expect(page.locator('.playlist-card:has-text("Delete Me")')).toHaveCount(0);

    const remaining = await (await listPlaylists(page)).json();
    expect((remaining || []).find((p: any) => (p.id || p.ID) === playlist.id)).toBeUndefined();
  });

  test('9. Cancel on the confirm dialog keeps the playlist', async ({ authenticatedPage: page }) => {
    const created = await createPlaylist(page, 'Keep Me');
    const playlist = await created.json();

    const partialResponse = page.waitForResponse(resp => resp.url().includes('/partials/playlists') && resp.status() === 200);
    await page.goto('/playlists');
    await partialResponse;

    const card = page.locator('.playlist-card:has-text("Keep Me")');
    page.on('dialog', dialog => dialog.dismiss());
    await card.locator('button:has-text("Delete")').click();
    await page.waitForTimeout(500);

    await expect(card).toBeVisible();
    const remaining = await (await listPlaylists(page)).json();
    expect((remaining || []).some((p: any) => (p.id || p.ID) === playlist.id)).toBe(true);
  });

  test('10. Unknown playlist id returns 404', async ({ authenticatedPage: page }) => {
    const response = await page.request.get('/api/playlists/11111111-2222-4333-8444-555555555555');

    expect(response.status()).toBe(404);
  });

  test('11. Navigation - the nav link reaches /playlists', async ({ authenticatedPage: page }) => {
    await page.goto('/');
    await expect(page.locator('.dashboard')).toBeVisible();

    await page.locator('nav#primary-nav a:has-text("Playlists")').click();
    await page.waitForTimeout(1000);

    await expect(page).toHaveURL(/\/playlists$/);
    await expect(page.locator('.page-header h2')).toHaveText('Playlists');
  });
});
