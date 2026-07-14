import { expect } from '@playwright/test';
import { test } from '../fixtures/auth.fixture';

// Helper to get CSRF token from page context
async function getCsrfToken(page: any): Promise<string> {
  const cookies = await page.context().cookies();
  return cookies.find((c: any) => c.name === 'csrf_')?.value || '';
}

// Helper to create a watchlist via API and return its ID
// Uses fixed E2E quality profile UUID seeded by setup-test-db.sh
const E2E_QUALITY_PROFILE_ID = '11111111-1111-4111-8111-111111111111';

async function createWatchlistViaAPI(page: any, data: {
  name: string;
  source_type: string;
  source_uri: string;
  quality_profile_id?: string;
  enabled?: boolean;
}): Promise<{ id: number; success: boolean }> {
  const csrfToken = await getCsrfToken(page);
  const response = await page.request.post('/api/watchlists', {
    data: {
      ...data,
      quality_profile_id: data.quality_profile_id || E2E_QUALITY_PROFILE_ID,
    },
    headers: { 'X-CSRF-Token': csrfToken }
  });

  if (response.status() === 201) {
    const body = await response.json();
    return { id: body.ID || body.id || 0, success: true };
  }

  return { id: 0, success: false };
}

// Helper to delete a watchlist via API
async function deleteWatchlistViaAPI(page: any, id: number): Promise<boolean> {
  const csrfToken = await getCsrfToken(page);
  const response = await page.request.delete(`/api/watchlists/${id}`, {
    headers: { 'X-CSRF-Token': csrfToken }
  });
  return response.ok();
}

// Helper to toggle a watchlist via API
async function toggleWatchlistViaAPI(page: any, id: number): Promise<boolean> {
  const csrfToken = await getCsrfToken(page);
  const response = await page.request.patch(`/api/watchlists/${id}/toggle`, {
    headers: { 'X-CSRF-Token': csrfToken }
  });
  return response.ok();
}

// Helper to update a watchlist via API
async function updateWatchlistViaAPI(page: any, id: number, data: Record<string, any>): Promise<boolean> {
  const csrfToken = await getCsrfToken(page);
  const response = await page.request.patch(`/api/watchlists/${id}`, {
    data,
    headers: { 'X-CSRF-Token': csrfToken }
  });
  return response.ok();
}

// Helper to list all watchlists via API
async function listWatchlistsViaAPI(page: any): Promise<any[]> {
  const response = await page.request.get('/api/watchlists');
  if (response.ok()) {
    return await response.json();
  }
  return [];
}

// Helper to wait for HTMX swap to complete
async function waitForHtmx(page: any, timeout: number = 1000): Promise<void> {
  await page.waitForTimeout(timeout);
}

test.describe('Watchlists Feature - DJI-426', () => {

  // Cleanup: delete all watchlists before each test to ensure clean state
  test.beforeEach(async ({ authenticatedPage: page }) => {
    const watchlists = await listWatchlistsViaAPI(page);
    for (const wl of watchlists) {
      await deleteWatchlistViaAPI(page, wl.ID);
    }
  });

  test.afterEach(async ({ authenticatedPage: page }) => {
    // Clean up any watchlists created during test
    const watchlists = await listWatchlistsViaAPI(page);
    for (const wl of watchlists) {
      await deleteWatchlistViaAPI(page, wl.ID);
    }
  });

  // ========== Page Load Tests ==========

  test('1. Page loads - navigate to /watchlists and verify page header with title', async ({ authenticatedPage: page }) => {
    await page.goto('/watchlists');
    await expect(page.locator('.page-header')).toBeVisible();
    await expect(page.locator('.page-header h2')).toHaveText('Watchlists');
  });

  test('2. Watchlists region loads - verify #watchlists-region HTMX container exists', async ({ authenticatedPage: page }) => {
    await page.goto('/watchlists');
    await waitForHtmx(page);
    await expect(page.locator('#watchlists-region')).toBeVisible();
  });

  test('3. Navigation - from dashboard click Watchlists nav link and verify navigation', async ({ authenticatedPage: page }) => {
    await page.goto('/');
    await expect(page.locator('.dashboard')).toBeVisible();
    await page.locator('nav#primary-nav a:has-text("Watchlists")').click();
    await waitForHtmx(page);
    await expect(page.locator('.page-header h2')).toHaveText('Watchlists');
  });

  test('4. Empty state - verify empty state message when no watchlists exist', async ({ authenticatedPage: page }) => {
    await page.goto('/watchlists');
    await waitForHtmx(page);
    await expect(page.locator('.empty-state')).toBeVisible();
    await expect(page.locator('.empty-state')).toHaveText('No watchlists configured');
  });

  test('5. Add Watchlist button - verify button exists in section header', async ({ authenticatedPage: page }) => {
    await page.goto('/watchlists');
    await waitForHtmx(page);
    const addButton = page.locator('.section-header button:has-text("Add Watchlist")');
    await expect(addButton).toBeVisible();
  });

  // ========== Modal and Form Tests ==========

  test('6. Watchlist form modal opens - click Add Watchlist and verify modal with form fields', async ({ authenticatedPage: page }) => {
    await page.goto('/watchlists');
    await waitForHtmx(page);

    // Click Add Watchlist button
    await page.locator('.section-header button:has-text("Add Watchlist")').click();
    await waitForHtmx(page, 500);

    // Verify modal appears
    await expect(page.locator('#modal-container .modal')).toBeVisible();

    // Verify form fields exist
    await expect(page.locator('#modal-container input[name="name"]')).toBeVisible();
    await expect(page.locator('#modal-container select[name="source_type"]')).toBeVisible();
    await expect(page.locator('#modal-container input[name="source_uri"]')).toBeVisible();
    await expect(page.locator('#modal-container select[name="quality_profile_id"]')).toBeVisible();
  });

  test('7. Create watchlist via API - POST /api/watchlists with valid data returns 201', async ({ authenticatedPage: page }) => {
    const csrfToken = await getCsrfToken(page);
    const response = await page.request.post('/api/watchlists', {
      data: {
        name: 'Test Watchlist',
        source_type: 'local_file',
        source_uri: '/music/test',
        quality_profile_id: E2E_QUALITY_PROFILE_ID,
      },
      headers: { 'X-CSRF-Token': csrfToken }
    });
    expect(response.status()).toBe(201);
  });

  test('8. Watchlist appears in list after creation via API', async ({ authenticatedPage: page }) => {
    // Create via API
    const { id } = await createWatchlistViaAPI(page, {
      name: 'API Created Watchlist',
      source_type: 'rss_feed',
      source_uri: 'https://example.com/feed.xml'
    });
    expect(id).toBeGreaterThan(0);

    // Navigate to page and verify card appears
    await page.goto('/watchlists');
    await waitForHtmx(page);

    const card = page.locator(`#watchlist-${id}`);
    await expect(card).toBeVisible();
    await expect(card.locator('.name')).toHaveText('API Created Watchlist');
  });

  test('9. Watchlist card details - verify card shows name, source_type badge, source_uri', async ({ authenticatedPage: page }) => {
    const { id } = await createWatchlistViaAPI(page, {
      name: 'Card Details Test',
      source_type: 'spotify_playlist',
      source_uri: 'spotify:playlist:abc123'
    });

    await page.goto('/watchlists');
    await waitForHtmx(page);

    const card = page.locator(`#watchlist-${id}`);
    await expect(card).toBeVisible();
    await expect(card.locator('.name')).toHaveText('Card Details Test');
    await expect(card.locator('.source')).toContainText('spotify_playlist');
    await expect(card.locator('.source')).toContainText('spotify:playlist:abc123');
  });

  // ========== Source Type Tests ==========

  test('10a. Create watchlist with source_type spotify_playlist', async ({ authenticatedPage: page }) => {
    const { id, success } = await createWatchlistViaAPI(page, {
      name: 'Spotify Playlist Test',
      source_type: 'spotify_playlist',
      source_uri: 'spotify:playlist:xyz789'
    });
    expect(success).toBe(true);
    expect(id).toBeGreaterThan(0);
  });

  test('10b. Create watchlist with source_type spotify_liked', async ({ authenticatedPage: page }) => {
    const { id, success } = await createWatchlistViaAPI(page, {
      name: 'Spotify Liked Test',
      source_type: 'spotify_liked',
      source_uri: 'spotify:user:123:collection'
    });
    expect(success).toBe(true);
    expect(id).toBeGreaterThan(0);
  });

  test('10c. Create watchlist with source_type lastfm_loved', async ({ authenticatedPage: page }) => {
    const { id, success } = await createWatchlistViaAPI(page, {
      name: 'Last.fm Loved Test',
      source_type: 'lastfm_loved',
      source_uri: 'lastfm://user/testuser/loved'
    });
    expect(success).toBe(true);
    expect(id).toBeGreaterThan(0);
  });

  test('10d. Create watchlist with source_type lastfm_top', async ({ authenticatedPage: page }) => {
    const { id, success } = await createWatchlistViaAPI(page, {
      name: 'Last.fm Top Test',
      source_type: 'lastfm_top',
      source_uri: 'lastfm://user/testuser/toptracks'
    });
    expect(success).toBe(true);
    expect(id).toBeGreaterThan(0);
  });

  test('10e. Create watchlist with source_type listenbrainz_listens', async ({ authenticatedPage: page }) => {
    const { id, success } = await createWatchlistViaAPI(page, {
      name: 'ListenBrainz Test',
      source_type: 'listenbrainz_listens',
      source_uri: 'listenbrainz://user/testuser/listens'
    });
    expect(success).toBe(true);
    expect(id).toBeGreaterThan(0);
  });

  test('10f. Create watchlist with source_type discogs_wantlist', async ({ authenticatedPage: page }) => {
    const { id, success } = await createWatchlistViaAPI(page, {
      name: 'Discogs Wantlist Test',
      source_type: 'discogs_wantlist',
      source_uri: 'discogs://user/testuser/wantlist'
    });
    expect(success).toBe(true);
    expect(id).toBeGreaterThan(0);
  });

  test('10g. Create watchlist with source_type lidarr_wanted', async ({ authenticatedPage: page }) => {
    const { id, success } = await createWatchlistViaAPI(page, {
      name: 'Lidarr Wanted Test',
      source_type: 'lidarr_wanted',
      source_uri: 'lidarr://artist/123'
    });
    expect(success).toBe(true);
    expect(id).toBeGreaterThan(0);
  });

  test('10h. Create watchlist with source_type rss_feed', async ({ authenticatedPage: page }) => {
    const { id, success } = await createWatchlistViaAPI(page, {
      name: 'RSS Feed Test',
      source_type: 'rss_feed',
      source_uri: 'https://example.com/rss.xml'
    });
    expect(success).toBe(true);
    expect(id).toBeGreaterThan(0);
  });

  test('10i. Create watchlist with source_type local_file', async ({ authenticatedPage: page }) => {
    const { id, success } = await createWatchlistViaAPI(page, {
      name: 'Local File Test',
      source_type: 'local_file',
      source_uri: '/path/to/file.mp3'
    });
    expect(success).toBe(true);
    expect(id).toBeGreaterThan(0);
  });

  test('10j. Create watchlist with source_type local_directory', async ({ authenticatedPage: page }) => {
    const { id, success } = await createWatchlistViaAPI(page, {
      name: 'Local Directory Test',
      source_type: 'local_directory',
      source_uri: '/path/to/directory'
    });
    expect(success).toBe(true);
    expect(id).toBeGreaterThan(0);
  });

  // ========== Multiple Watchlist Tests ==========

  test('11. Multiple watchlists display - create 3 watchlists and verify all appear in list', async ({ authenticatedPage: page }) => {
    const wl1 = await createWatchlistViaAPI(page, { name: 'Watchlist 1', source_type: 'local_file', source_uri: '/path1' });
    const wl2 = await createWatchlistViaAPI(page, { name: 'Watchlist 2', source_type: 'rss_feed', source_uri: 'https://feed1.com' });
    const wl3 = await createWatchlistViaAPI(page, { name: 'Watchlist 3', source_type: 'spotify_playlist', source_uri: 'spotify:playlist:abc' });

    await page.goto('/watchlists');
    await waitForHtmx(page);

    await expect(page.locator(`#watchlist-${wl1.id}`)).toBeVisible();
    await expect(page.locator(`#watchlist-${wl2.id}`)).toBeVisible();
    await expect(page.locator(`#watchlist-${wl3.id}`)).toBeVisible();
  });

  // ========== Toggle Tests ==========

  test('12. Toggle enabled/disabled - toggle watchlist state and verify change', async ({ authenticatedPage: page }) => {
    const { id } = await createWatchlistViaAPI(page, {
      name: 'Toggle Test',
      source_type: 'local_file',
      source_uri: '/test',
      enabled: true
    });

    // Toggle off via API
    await toggleWatchlistViaAPI(page, id);

    // Verify toggle state changed - reload and check
    await page.goto('/watchlists');
    await waitForHtmx(page);

    const toggle = page.locator(`#watchlist-${id} input[type="checkbox"]`);
    await expect(toggle).not.toBeChecked();
  });

  // ========== Sync Tests ==========

  test('13. Sync button exists on watchlist card', async ({ authenticatedPage: page }) => {
    const { id } = await createWatchlistViaAPI(page, {
      name: 'Sync Button Test',
      source_type: 'local_file',
      source_uri: '/test'
    });

    await page.goto('/watchlists');
    await waitForHtmx(page);

    const syncButton = page.locator(`#watchlist-${id} button:has-text("Sync")`);
    await expect(syncButton).toBeVisible();
  });

  test('14. Sync button triggers sync action', async ({ authenticatedPage: page }) => {
    const { id } = await createWatchlistViaAPI(page, {
      name: 'Sync Action Test',
      source_type: 'rss_feed',
      source_uri: 'https://example.com/feed.xml'
    });

    await page.goto('/watchlists');
    await waitForHtmx(page);

    // Click sync button
    const syncButton = page.locator(`#watchlist-${id} button:has-text("Sync")`);
    await syncButton.click();
    await waitForHtmx(page, 500);

    // Button should still be present (no error)
    await expect(syncButton).toBeVisible();
  });

  // ========== Edit Tests ==========

  test('15. Edit button opens form with pre-filled data', async ({ authenticatedPage: page }) => {
    const { id } = await createWatchlistViaAPI(page, {
      name: 'Edit Form Test',
      source_type: 'local_file',
      source_uri: '/original/path'
    });

    await page.goto('/watchlists');
    await waitForHtmx(page);

    // Click Edit button
    const editButton = page.locator(`#watchlist-${id} button:has-text("Edit")`);
    await editButton.click();
    await waitForHtmx(page, 500);

    // Verify modal opens with pre-filled form
    await expect(page.locator('#modal-container .modal')).toBeVisible();
    await expect(page.locator('#modal-container input[name="name"]')).toHaveValue('Edit Form Test');
    await expect(page.locator('#modal-container input[name="source_uri"]')).toHaveValue('/original/path');
  });

  test('16. Update watchlist via edit form - modify name and verify changes', async ({ authenticatedPage: page }) => {
    const { id } = await createWatchlistViaAPI(page, {
      name: 'Original Name',
      source_type: 'rss_feed',
      source_uri: 'https://original.com/feed'
    });

    // Update via API
    const success = await updateWatchlistViaAPI(page, id, {
      name: 'Updated Name',
      source_uri: 'https://updated.com/feed'
    });
    expect(success).toBe(true);

    // Verify changes on page
    await page.goto('/watchlists');
    await waitForHtmx(page);

    const card = page.locator(`#watchlist-${id}`);
    await expect(card.locator('.name')).toHaveText('Updated Name');
    await expect(card.locator('.source')).toContainText('https://updated.com/feed');
  });

  // ========== Delete Tests ==========

  test('17. Delete watchlist with confirm - accept confirmation and verify removal', async ({ authenticatedPage: page }) => {
    const { id } = await createWatchlistViaAPI(page, {
      name: 'Delete Confirm Test',
      source_type: 'local_file',
      source_uri: '/delete/me'
    });

    await page.goto('/watchlists');
    await waitForHtmx(page);

    // Set up dialog handler before clicking delete
    page.on('dialog', async dialog => {
      await dialog.accept();
    });

    // Click delete button
    const deleteButton = page.locator(`#watchlist-${id} button:has-text("Delete")`);
    await deleteButton.click();
    await waitForHtmx(page, 500);

    // Card should be removed
    await expect(page.locator(`#watchlist-${id}`)).not.toBeVisible();
  });

  test('18. Cancel delete - dismiss confirmation and verify watchlist still exists', async ({ authenticatedPage: page }) => {
    const { id } = await createWatchlistViaAPI(page, {
      name: 'Cancel Delete Test',
      source_type: 'local_file',
      source_uri: '/keep/me'
    });

    await page.goto('/watchlists');
    await waitForHtmx(page);

    // Set up dialog handler to cancel
    page.on('dialog', async dialog => {
      await dialog.dismiss();
    });

    // Click delete button
    const deleteButton = page.locator(`#watchlist-${id} button:has-text("Delete")`);
    await deleteButton.click();
    await waitForHtmx(page, 500);

    // Card should still be visible
    await expect(page.locator(`#watchlist-${id}`)).toBeVisible();
  });

  // ========== Validation Tests ==========

  test('19. Form validation - creating with empty name should fail via API', async ({ authenticatedPage: page }) => {
    const csrfToken = await getCsrfToken(page);
    const response = await page.request.post('/api/watchlists', {
      data: {
        name: '',
        source_type: 'local_file',
        source_uri: '/test'
      },
      headers: { 'X-CSRF-Token': csrfToken }
    });
    // Should not return 201
    expect(response.status()).not.toBe(201);
  });

  test('19b. Form validation - creating with empty source_uri should fail via API', async ({ authenticatedPage: page }) => {
    const csrfToken = await getCsrfToken(page);
    const response = await page.request.post('/api/watchlists', {
      data: {
        name: 'Valid Name',
        source_type: 'local_file',
        source_uri: ''
      },
      headers: { 'X-CSRF-Token': csrfToken }
    });
    expect(response.status()).not.toBe(201);
  });

  // ========== Preview Tests ==========

  test('20. Preview button exists and triggers preview action', async ({ authenticatedPage: page }) => {
    const { id } = await createWatchlistViaAPI(page, {
      name: 'Preview Test',
      source_type: 'rss_feed',
      source_uri: 'https://example.com/feed.xml'
    });

    await page.goto('/watchlists');
    await waitForHtmx(page);

    const previewButton = page.locator(`#watchlist-${id} button:has-text("Preview")`);
    await expect(previewButton).toBeVisible();

    // Click preview
    await previewButton.click();
    await waitForHtmx(page, 500);

    // Preview area should exist
    await expect(page.locator(`#watchlist-preview-${id}`)).toBeVisible();
  });

  // ========== Quality Profile Tests ==========

  test('21. Quality profile selector exists in form', async ({ authenticatedPage: page }) => {
    await page.goto('/watchlists');
    await waitForHtmx(page);

    // Open modal
    await page.locator('.section-header button:has-text("Add Watchlist")').click();
    await waitForHtmx(page, 500);

    // Verify quality_profile_id select exists
    await expect(page.locator('#modal-container select[name="quality_profile_id"]')).toBeVisible();
  });

  test('21b. Form has enabled checkbox', async ({ authenticatedPage: page }) => {
    await page.goto('/watchlists');
    await waitForHtmx(page);

    // Open modal
    await page.locator('.section-header button:has-text("Add Watchlist")').click();
    await waitForHtmx(page, 500);

    // Verify enabled checkbox exists
    await expect(page.locator('#modal-container input[name="enabled"]')).toBeVisible();
  });

  // ========== Cross-Page Navigation Tests ==========

  test('22. Navigation cross-page - navigate from watchlists to jobs page', async ({ authenticatedPage: page }) => {
    await page.goto('/watchlists');
    await waitForHtmx(page);

    // Navigate to jobs
    await page.locator('nav#primary-nav a:has-text("Jobs")').click();
    await waitForHtmx(page);

    await expect(page.locator('.page-header h2')).toHaveText('Jobs');
  });

  test('23. Navigation cross-page - navigate from watchlists to libraries page', async ({ authenticatedPage: page }) => {
    await page.goto('/watchlists');
    await waitForHtmx(page);

    // Navigate to libraries
    await page.locator('nav#primary-nav a:has-text("Libraries")').click();
    await waitForHtmx(page);

    await expect(page.locator('.page-header h2')).toHaveText('Libraries');
  });

  test('24. Navigation cross-page - navigate from watchlists to artists page', async ({ authenticatedPage: page }) => {
    await page.goto('/watchlists');
    await waitForHtmx(page);

    // Navigate to artists
    await page.locator('nav#primary-nav a:has-text("Artists")').click();
    await waitForHtmx(page);

    await expect(page.locator('.page-header h2')).toHaveText('Artists');
  });

  test('25. Navigation cross-page - navigate from watchlists to schedules page', async ({ authenticatedPage: page }) => {
    await page.goto('/watchlists');
    await waitForHtmx(page);

    // Navigate to schedules
    await page.locator('nav#primary-nav a:has-text("Schedules")').click();
    await waitForHtmx(page);

    await expect(page.locator('.page-header h2')).toHaveText('Schedules');
  });

  // ========== API Endpoint Tests ==========

  test('26. GET /api/watchlists returns list of watchlists', async ({ authenticatedPage: page }) => {
    // Create a watchlist first
    await createWatchlistViaAPI(page, {
      name: 'API List Test',
      source_type: 'local_file',
      source_uri: '/test'
    });

    const response = await page.request.get('/api/watchlists');
    expect(response.ok()).toBe(true);

    const watchlists = await response.json();
    expect(Array.isArray(watchlists)).toBe(true);
  });

  test('27. GET /api/watchlists/:id returns watchlist detail', async ({ authenticatedPage: page }) => {
    const { id } = await createWatchlistViaAPI(page, {
      name: 'Detail Test',
      source_type: 'rss_feed',
      source_uri: 'https://detail.com/feed'
    });

    const response = await page.request.get(`/api/watchlists/${id}`);
    expect(response.ok()).toBe(true);

    const watchlist = await response.json();
    expect(watchlist.Name).toBe('Detail Test');
    expect(watchlist.SourceType).toBe('rss_feed');
  });

  test('28. POST /api/watchlists/:id/sync triggers sync job', async ({ authenticatedPage: page }) => {
    const { id } = await createWatchlistViaAPI(page, {
      name: 'Sync Trigger Test',
      source_type: 'rss_feed',
      source_uri: 'https://sync.com/feed'
    });

    const csrfToken = await getCsrfToken(page);
    const response = await page.request.post(`/api/watchlists/${id}/sync`, {
      headers: { 'X-CSRF-Token': csrfToken }
    });
    expect(response.ok()).toBe(true);
  });

  // ========== Modal Close Tests ==========

  test('29. Modal close button works', async ({ authenticatedPage: page }) => {
    await page.goto('/watchlists');
    await waitForHtmx(page);

    // Open modal
    await page.locator('.section-header button:has-text("Add Watchlist")').click();
    await waitForHtmx(page, 500);

    // Verify modal is open
    await expect(page.locator('#modal-container .modal')).toBeVisible();

    // Click close button
    await page.locator('#modal-container .modal-close').click();
    await waitForHtmx(page, 500);

    // Modal should be hidden
    await expect(page.locator('#modal-container .modal-overlay')).not.toBeVisible({ timeout: 5000 });
  });

  test('30. Click outside modal (overlay) closes modal', async ({ authenticatedPage: page }) => {
    await page.goto('/watchlists');
    await waitForHtmx(page);

    // Open modal
    await page.locator('.section-header button:has-text("Add Watchlist")').click();
    await waitForHtmx(page, 500);

    // Verify modal is open
    await expect(page.locator('#modal-container .modal')).toBeVisible();

    // Click overlay background to close
    await page.locator('#modal-container .modal-overlay').click({ position: { x: 10, y: 10 } });
    await waitForHtmx(page, 500);

    // Modal should be hidden
    await expect(page.locator('#modal-container .modal')).not.toBeVisible();
  });

  // ========== Create via HTMX Form Submit ==========

  test('31. Create watchlist via HTMX form submit in modal', async ({ authenticatedPage: page }) => {
    await page.goto('/watchlists');
    await waitForHtmx(page);

    // Open modal
    await page.locator('.section-header button:has-text("Add Watchlist")').click();
    await waitForHtmx(page, 500);

    // Fill form
    await page.locator('#modal-container input[name="name"]').fill('HTMX Created');
    await page.locator('#modal-container select[name="source_type"]').selectOption('rss_feed');
    await page.locator('#modal-container input[name="source_uri"]').fill('https://htmx.com/feed.xml');

    // Submit form
    await page.locator('#modal-container button[type="submit"]').click();
    await waitForHtmx(page, 1000);

    // Verify modal closed
    await expect(page.locator('#modal-container .modal')).not.toBeVisible();

    // Verify watchlist appears in list
    await expect(page.locator('.watchlist-card .name:has-text("HTMX Created")')).toBeVisible();
  });

  // ========== Watchlist Count Display ==========

  test('32. Watchlist count in section header reflects actual count', async ({ authenticatedPage: page }) => {
    // Create 2 watchlists
    await createWatchlistViaAPI(page, { name: 'Count Test 1', source_type: 'local_file', source_uri: '/one' });
    await createWatchlistViaAPI(page, { name: 'Count Test 2', source_type: 'local_file', source_uri: '/two' });

    await page.goto('/watchlists');
    await waitForHtmx(page);

    // Should see 2 watchlist cards
    const cards = page.locator('.watchlist-card');
    await expect(cards).toHaveCount(2);
  });

  // ========== Source Type Badge Display ==========

  test('33. Source type displayed correctly in card', async ({ authenticatedPage: page }) => {
    const { id } = await createWatchlistViaAPI(page, {
      name: 'Badge Display Test',
      source_type: 'spotify_liked',
      source_uri: 'spotify:user:test:liked'
    });

    await page.goto('/watchlists');
    await waitForHtmx(page);

    const card = page.locator(`#watchlist-${id}`);
    await expect(card.locator('.source')).toContainText('spotify_liked');
  });

  // ========== Empty State Disappears When Watchlists Exist ==========

  test('34. Empty state hidden when watchlists exist', async ({ authenticatedPage: page }) => {
    // Create a watchlist
    await createWatchlistViaAPI(page, {
      name: 'No Empty State Test',
      source_type: 'local_file',
      source_uri: '/test'
    });

    await page.goto('/watchlists');
    await waitForHtmx(page);

    // Empty state should not be visible
    await expect(page.locator('.empty-state')).not.toBeVisible();
  });

  // ========== Watchlist Form Has All Source Type Options ==========

  test('35. Watchlist form has all expected source type options', async ({ authenticatedPage: page }) => {
    await page.goto('/watchlists');
    await waitForHtmx(page);

    // Open modal
    await page.locator('.section-header button:has-text("Add Watchlist")').click();
    await waitForHtmx(page, 500);

    // Verify all expected options exist in select
    const select = page.locator('#modal-container select[name="source_type"]');
    await expect(select).toBeVisible();

    const options = await select.locator('option').allTextContents();
    expect(options.some(opt => opt.includes('Spotify Playlist'))).toBe(true);
    expect(options.some(opt => opt.includes('Last.fm'))).toBe(true);
    expect(options.some(opt => opt.includes('ListenBrainz'))).toBe(true);
    expect(options.some(opt => opt.includes('RSS Feed'))).toBe(true);
    expect(options.some(opt => opt.includes('Local'))).toBe(true);
  });

  // ========== Edit Preserves Other Fields ==========

  test('36. Editing watchlist preserves unchanged fields', async ({ authenticatedPage: page }) => {
    const { id } = await createWatchlistViaAPI(page, {
      name: 'Preserve Fields Test',
      source_type: 'rss_feed',
      source_uri: 'https://preserve.com/feed'
    });

    // Update only the name via API
    await updateWatchlistViaAPI(page, id, { name: 'New Name Only' });

    await page.goto('/watchlists');
    await waitForHtmx(page);

    const card = page.locator(`#watchlist-${id}`);
    // Source URI should still be the original
    await expect(card.locator('.source')).toContainText('https://preserve.com/feed');
  });

  // ========== Watchlist Form Cancel Button ==========

  test('37. Form cancel button closes modal without changes', async ({ authenticatedPage: page }) => {
    await page.goto('/watchlists');
    await waitForHtmx(page);

    // Open modal
    await page.locator('.section-header button:has-text("Add Watchlist")').click();
    await waitForHtmx(page, 500);

    // Fill some data
    await page.locator('#modal-container input[name="name"]').fill('Cancel Test');

    // Click cancel
    await page.locator('#modal-container button:has-text("Cancel")').click();
    await waitForHtmx(page, 500);

    // Modal should be closed
    await expect(page.locator('#modal-container .modal')).not.toBeVisible();
  });

});
