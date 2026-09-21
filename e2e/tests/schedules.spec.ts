import { expect } from '@playwright/test';
import { test } from '../fixtures/auth.fixture';

// Helper to get CSRF token from cookies
async function getCsrfToken(page: any): Promise<string> {
  const cookies = await page.context().cookies();
  return cookies.find((c: any) => c.name === 'csrf_')?.value || '';
}

// Helper to get a quality profile ID (required for watchlist creation)
async function getQualityProfileId(page: any): Promise<string> {
  const csrfToken = await getCsrfToken(page);
  // Use /api/profiles which returns all profiles (including global/default ones)
  const response = await page.request.get('/api/profiles', {
    headers: { 'X-CSRF-Token': csrfToken },
  });

  if (!response.ok()) {
    throw new Error(`Failed to get profiles: ${response.status()}`);
  }

  let profiles;
  try {
    profiles = await response.json();
  } catch {
    throw new Error(`Failed to parse profiles response`);
  }

  if (!Array.isArray(profiles) || profiles.length === 0) {
    throw new Error('No quality profiles available');
  }

  return profiles[0].ID || profiles[0].id;
}

// Helper to create a watchlist via API (needed for schedule creation)
async function createWatchlistViaAPI(page: any, name: string = 'Test Watchlist'): Promise<number> {
  const csrfToken = await getCsrfToken(page);
  // Use unique URI to avoid "already exists" errors
  const uniqueUri = `https://example.com/feed-${Date.now()}-${Math.random().toString(36).substring(7)}.xml`;
  // Get a valid quality profile ID dynamically
  const qualityProfileId = await getQualityProfileId(page);
  const response = await page.request.post('/api/watchlists', {
    data: {
      name,
      source_type: 'rss_feed',
      source_uri: uniqueUri,
      quality_profile_id: qualityProfileId,
    },
    headers: { 'X-CSRF-Token': csrfToken },
  });

  if (!response.ok()) {
    const body = await response.text();
    throw new Error(`Failed to create watchlist: ${response.status()} - ${body}`);
  }

  let watchlist;
  try {
    watchlist = await response.json();
  } catch {
    throw new Error(`Failed to parse watchlist response: ${response.status()}`);
  }

  return watchlist.ID || watchlist.id;
}

// Helper to create a schedule via API
async function createScheduleViaAPI(page: any, watchlistId: number, cronExpr: string = '0 6 * * *', enabled: boolean = true): Promise<any> {
  const csrfToken = await getCsrfToken(page);
  const response = await page.request.post('/api/schedules', {
    data: {
      watchlist_id: watchlistId.toString(),
      cron_expr: cronExpr,
      enabled,
    },
    headers: { 'X-CSRF-Token': csrfToken },
  });

  const status = response.status();
  const bodyText = await response.text();

  if (status < 200 || status > 201) {
    throw new Error(`Failed to create schedule: ${status} - ${bodyText}`);
  }

  let schedule;
  try {
    schedule = JSON.parse(bodyText);
  } catch {
    throw new Error(`Failed to parse schedule response: ${status} - ${bodyText}`);
  }

  return schedule;
}

// Helper to delete all schedules (cleanup)
async function cleanupSchedules(page: any): Promise<void> {
  const csrfToken = await getCsrfToken(page);
  const response = await page.request.get('/api/schedules', {
    headers: { 'X-CSRF-Token': csrfToken },
  });

  if (response.ok()) {
    const schedules = await response.json();
    for (const schedule of schedules) {
      await page.request.delete(`/api/schedules/${schedule.ID}`, {
        headers: { 'X-CSRF-Token': csrfToken },
      });
    }
  }
}

// Helper to disable a watchlist
async function disableWatchlist(page: any, watchlistId: number): Promise<void> {
  const csrfToken = await getCsrfToken(page);
  const response = await page.request.patch(`/api/watchlists/${watchlistId}/toggle`, {
    headers: { 'X-CSRF-Token': csrfToken },
  });
  if (!response.ok()) {
    throw new Error(`Failed to disable watchlist ${watchlistId}: ${response.status()} ${response.statusText()}`);
  }
}

test.describe('Schedules Feature (DJI-429)', () => {
  test.describe('Page Load & Structure', () => {
    test('1. Page loads - navigate to /schedules, verify .page-header visible, verify title "Schedules"', async ({ authenticatedPage: page }) => {
      await page.goto('/schedules');

      // Wait for HTMX to load
      await page.waitForTimeout(1000);

      // Verify page header is visible
      await expect(page.locator('.page-header')).toBeVisible();

      // Verify title is "Schedules"
      await expect(page.locator('.page-header h2')).toHaveText('Schedules');
    });

    test('2. Schedules region loads - verify #schedules-region is visible', async ({ authenticatedPage: page }) => {
      await page.goto('/schedules');

      // Wait for HTMX to load
      await page.waitForTimeout(1000);

      // Verify schedules region is visible
      const schedulesRegion = page.locator('#schedules-region');
      await expect(schedulesRegion).toBeVisible();

      // Verify the schedules region has loaded content
      await expect(page.locator('.schedules-region')).toBeVisible();
    });
  });

  test.describe('Navigation', () => {
    test('3. Navigation - from dashboard /, click "Schedules" nav link, verify page navigates to /schedules with correct title', async ({ authenticatedPage: page }) => {
      // Start from dashboard
      await page.goto('/');
      await expect(page.locator('.dashboard')).toBeVisible();

      // Click Schedules nav link
      await page.locator('nav#primary-nav a:has-text("Schedules")').click();

      // Wait for navigation and HTMX load
      await page.waitForTimeout(1000);

      // Verify we're on schedules page with correct title
      await expect(page.locator('.page-header h2')).toHaveText('Schedules');

      // Verify URL is /schedules
      expect(page.url()).toContain('/schedules');
    });
  });

  test.describe('Empty State', () => {
    test('4. Empty state - navigate /schedules, verify "No schedules configured" is present', async ({ authenticatedPage: page }) => {
      // First cleanup any existing schedules
      await cleanupSchedules(page);

      await page.goto('/schedules');
      await page.waitForTimeout(1000);

      // Verify empty state text is present
      const emptyState = page.locator('.empty-state, p:has-text("No schedules configured")');
      await expect(emptyState.first()).toBeVisible();
    });
  });

  test.describe('Add Schedule Button', () => {
    test('5. Add Schedule button - verify "Add Schedule" button exists', async ({ authenticatedPage: page }) => {
      await page.goto('/schedules');
      await page.waitForTimeout(1000);

      // Verify Add Schedule button exists
      const addButton = page.locator('button:has-text("Add Schedule")');
      await expect(addButton).toBeVisible();
    });
  });

  test.describe('Schedule Form Modal', () => {
    test('6. Schedule form modal opens - click "Add Schedule", wait for #modal-container, verify form fields (watchlist select, cron input)', async ({ authenticatedPage: page }) => {
      await page.goto('/schedules');
      await page.waitForTimeout(1000);

      // Click Add Schedule button
      await page.locator('button:has-text("Add Schedule")').click();

      // Wait for modal to appear
      await page.waitForTimeout(500);

      // Verify modal is visible
      const modal = page.locator('#modal-container');
      await expect(modal).toBeVisible();

      // Verify form fields exist
      const watchlistSelect = page.locator('#watchlist_id, select[name="watchlist_id"]');
      await expect(watchlistSelect.first()).toBeVisible();

      const cronInput = page.locator('#cron_expr, input[name="cron_expr"]');
      await expect(cronInput.first()).toBeVisible();

      // The Add form claims a new schedule will be enabled, which is what
      // Create does when the field arrives absent.
      await expect(page.locator('#modal-container input[name="enabled"]')).toBeChecked();
    });
  });

  test.describe('Schedule CRUD via API', () => {
    test('7. Create schedule via API - use API to create a schedule, verify 200/201', async ({ adminPage: page }) => {
      // Create a watchlist first
      const watchlistId = await createWatchlistViaAPI(page, 'API Test Watchlist');
      expect(watchlistId).toBeDefined();

      // Create schedule via API
      const csrfToken = await getCsrfToken(page);
      const response = await page.request.post('/api/schedules', {
        data: {
          watchlist_id: watchlistId.toString(),
          cron_expr: '0 6 * * *',
          enabled: true,
        },
        headers: { 'X-CSRF-Token': csrfToken },
      });

      expect(response.status()).toBeGreaterThanOrEqual(200);
      expect(response.status()).toBeLessThanOrEqual(201);
    });

    test('8. Schedule appears after creation - create schedule via API, verify it appears in the list', async ({ adminPage: page }) => {
      const watchlistId = await createWatchlistViaAPI(page, `Schedule List ${Date.now()}`);
      const created = await createScheduleViaAPI(page, watchlistId, '0 6 * * *', true);

      // A schedule created through the API has a null next_run_at, which is the
      // shape that used to 500 the partial and hide every row.
      await page.goto('/schedules');
      await expect(page.locator(`#schedule-${created.ID || created.id}`)).toBeVisible();
    });

    test('9. Schedule card details - the card names the watchlist with its cron and next run', async ({ adminPage: page }) => {
      const watchlistName = `Schedule Detail ${Date.now()}`;
      const watchlistId = await createWatchlistViaAPI(page, watchlistName);
      const created = await createScheduleViaAPI(page, watchlistId, '0 7 * * *', true);

      await page.goto('/schedules');
      const card = page.locator(`#schedule-${created.ID || created.id}`);
      await expect(card).toBeVisible();
      await expect(card.locator('.name')).toContainText(watchlistName);
      await expect(card.locator('.details')).toContainText('Cron: 0 7 * * *');
      await expect(card.locator('.details')).toContainText('Next:');
    });
  });

  test.describe('Toggle Enable/Disable', () => {
    test('10. Toggle enable/disable - the card comes back disabled', async ({ adminPage: page }) => {
      const watchlistId = await createWatchlistViaAPI(page, `Schedule Toggle ${Date.now()}`);
      const created = await createScheduleViaAPI(page, watchlistId, '0 6 * * *', true);
      const scheduleId = created.ID || created.id;

      await page.goto('/schedules');
      await page.locator(`#schedule-${scheduleId} button[aria-label^="Disable"]`).click();

      // The toggle swaps in the card partial, so this also pins that the card
      // the server returns is the one the list renders.
      await expect(page.locator(`#schedule-${scheduleId} button[aria-label^="Enable"]`)).toBeVisible();
      await expect(page.locator(`#schedule-${scheduleId} .badge-disabled`)).toBeVisible();
    });

    test('11. Toggle re-enable - a second toggle restores the enabled card', async ({ adminPage: page }) => {
      const watchlistId = await createWatchlistViaAPI(page, `Schedule Retoggle ${Date.now()}`);
      const created = await createScheduleViaAPI(page, watchlistId, '0 6 * * *', true);
      const scheduleId = created.ID || created.id;

      await page.goto('/schedules');
      await page.locator(`#schedule-${scheduleId} button[aria-label^="Disable"]`).click();
      await page.locator(`#schedule-${scheduleId} button[aria-label^="Enable"]`).click();

      await expect(page.locator(`#schedule-${scheduleId} button[aria-label^="Disable"]`)).toBeVisible();
      await expect(page.locator(`#schedule-${scheduleId} .badge-disabled`)).toHaveCount(0);
    });
  });

  test.describe('Edit Schedule', () => {
    test('12. Edit schedule - the modal opens populated with the schedule values', async ({ adminPage: page }) => {
      const watchlistId = await createWatchlistViaAPI(page, `Schedule Edit ${Date.now()}`);
      const created = await createScheduleViaAPI(page, watchlistId, '0 8 * * *', true);
      const scheduleId = created.ID || created.id;

      await page.goto('/schedules');
      await page.locator(`#schedule-${scheduleId} button[aria-label^="Edit"]`).click();

      const modal = page.locator('#modal-container');
      await expect(modal.locator('#modal-title')).toHaveText('Edit Schedule');
      await expect(modal.locator('#cron_expr')).toHaveValue('0 8 * * *');
      await expect(modal.locator('#watchlist_id')).toHaveValue(String(watchlistId));
      await expect(modal.locator('input[name="enabled"]')).toBeChecked();
    });

    test('13. Update schedule via edit - saving changes the cron and closes the modal', async ({ adminPage: page }) => {
      const watchlistId = await createWatchlistViaAPI(page, `Schedule Update ${Date.now()}`);
      const created = await createScheduleViaAPI(page, watchlistId, '0 9 * * *', true);
      const scheduleId = created.ID || created.id;

      await page.goto('/schedules');
      await page.locator(`#schedule-${scheduleId} button[aria-label^="Edit"]`).click();

      await page.locator('#cron_expr').fill('0 10 * * *');
      await page.locator('#modal-container button[type="submit"]').click();

      await expect(page.locator(`#schedule-${scheduleId} .details`)).toContainText('Cron: 0 10 * * *');
      // The update handler sets HX-Trigger: closeModal, so the modal has to go.
      await expect(page.locator('#modal-container .modal')).toHaveCount(0);
    });
  });

  test.describe('Delete Schedule', () => {
    test('14. Delete schedule - confirming the dialog removes the card', async ({ adminPage: page }) => {
      const watchlistId = await createWatchlistViaAPI(page, `Schedule Delete ${Date.now()}`);
      const created = await createScheduleViaAPI(page, watchlistId, '0 6 * * *', true);
      const scheduleId = created.ID || created.id;

      await page.goto('/schedules');
      await expect(page.locator(`#schedule-${scheduleId}`)).toBeVisible();

      // hx-confirm raises a native confirm() before the delete is issued.
      page.on('dialog', (dialog: any) => dialog.accept());
      await page.locator(`#schedule-${scheduleId} button[aria-label^="Delete"]`).click();

      await expect(page.locator(`#schedule-${scheduleId}`)).toHaveCount(0);
    });
  });

  test.describe('Form Validation', () => {
    test('15. Form validation - invalid cron - try creating with invalid cron expression, verify error', async ({ authenticatedPage: page }) => {
      // Create a watchlist first
      const watchlistId = await createWatchlistViaAPI(page, 'Invalid Cron Watchlist');

      // Try to create schedule with invalid cron via API
      const csrfToken = await getCsrfToken(page);
      const response = await page.request.post('/api/schedules', {
        data: {
          watchlist_id: watchlistId.toString(),
          cron_expr: 'invalid-cron',
          enabled: true,
        },
        headers: { 'X-CSRF-Token': csrfToken },
      });

      // Should return an error (4xx)
      expect(response.status()).toBeGreaterThanOrEqual(400);
    });

    test('16. Form validation - missing fields - try creating without required fields, verify error', async ({ authenticatedPage: page }) => {
      // Try to create schedule without required fields via API
      const csrfToken = await getCsrfToken(page);
      const response = await page.request.post('/api/schedules', {
        data: {
          // Missing watchlist_id and cron_expr
        },
        headers: { 'X-CSRF-Token': csrfToken },
      });

      // Should return an error (4xx)
      expect(response.status()).toBeGreaterThanOrEqual(400);
    });
  });

  test.describe('Multiple Schedules', () => {
    test('17. Multiple schedules - both cards render for the one watchlist', async ({ adminPage: page }) => {
      const watchlistName = `Schedule Multi ${Date.now()}`;
      const watchlistId = await createWatchlistViaAPI(page, watchlistName);
      const first = await createScheduleViaAPI(page, watchlistId, '0 1 * * *', true);
      const second = await createScheduleViaAPI(page, watchlistId, '0 2 * * *', true);

      await page.goto('/schedules');
      await expect(page.locator(`#schedule-${first.ID || first.id}`)).toBeVisible();
      await expect(page.locator(`#schedule-${second.ID || second.id}`)).toBeVisible();
      await expect(
        page.locator('#schedules-list .schedule-card').filter({ hasText: watchlistName }),
      ).toHaveCount(2);
    });
  });

  test.describe('Cross-Navigation', () => {
    test('18. Navigation from schedules to other pages - use nav from schedules page to verify cross-navigation', async ({ authenticatedPage: page }) => {
      await page.goto('/schedules');
      await page.waitForTimeout(1000);

      // Navigate to Jobs page
      await page.locator('nav#primary-nav a:has-text("Jobs")').click();
      await page.waitForTimeout(1000);
      await expect(page.locator('.page-header h2')).toHaveText('Jobs');

      // Navigate to Libraries page
      await page.locator('nav#primary-nav a:has-text("Libraries")').click();
      await page.waitForTimeout(1000);
      await expect(page.locator('.page-header h2')).toHaveText('Libraries');

      // Navigate to Watchlists page
      await page.locator('nav#primary-nav a:has-text("Watchlists")').click();
      await page.waitForTimeout(1000);
      await expect(page.locator('.page-header h2')).toHaveText('Watchlists');

      // Navigate back to Schedules
      await page.locator('nav#primary-nav a:has-text("Schedules")').click();
      await page.waitForTimeout(1000);
      await expect(page.locator('.page-header h2')).toHaveText('Schedules');
    });
  });

  test.describe('Disabled Watchlist Behavior', () => {
    test('19. Schedule with disabled watchlist - the schedule still renders', async ({ adminPage: page }) => {
      const watchlistId = await createWatchlistViaAPI(page, `Schedule Disabled Watchlist ${Date.now()}`);
      await disableWatchlist(page, watchlistId);
      const created = await createScheduleViaAPI(page, watchlistId, '0 3 * * *', true);

      await page.goto('/schedules');
      await expect(page.locator(`#schedule-${created.ID || created.id}`)).toBeVisible();
    });
  });

  test.describe('Disabled State Persistence', () => {
    test('20. Disabled state persistence - a disabled schedule stays disabled across reloads', async ({ adminPage: page }) => {
      const watchlistId = await createWatchlistViaAPI(page, `Schedule Disabled State ${Date.now()}`);
      const created = await createScheduleViaAPI(page, watchlistId, '0 4 * * *', false);
      const scheduleId = created.ID || created.id;

      await page.goto('/schedules');
      await expect(page.locator(`#schedule-${scheduleId} .badge-disabled`)).toBeVisible();

      await page.reload();
      await expect(page.locator(`#schedule-${scheduleId} .badge-disabled`)).toBeVisible();
      await expect(page.locator(`#schedule-${scheduleId} button[aria-label^="Enable"]`)).toBeVisible();
    });
  });

  test.describe('Error Handling', () => {
    test('21. Error handling - test API errors (e.g., invalid schedule data)', async ({ adminPage: page }) => {
      // Try to get non-existent schedule
      const csrfToken = await getCsrfToken(page);
      const getResponse = await page.request.get('/api/schedules/99999', {
        headers: { 'X-CSRF-Token': csrfToken },
      });
      // GET /api/schedules/:id is not supported, returns 405 Method Not Allowed
      expect([404, 405]).toContain(getResponse.status());

      // Try to delete non-existent schedule - admin can delete, might return 200 or 404 depending on implementation
      const deleteResponse = await page.request.delete('/api/schedules/99999', {
        headers: { 'X-CSRF-Token': csrfToken },
      });
      // Accept both success and not found
      expect([200, 204, 404]).toContain(deleteResponse.status());

      // Try to toggle non-existent schedule - returns 404 (not found)
      const toggleResponse = await page.request.patch('/api/schedules/99999/toggle', {
        headers: { 'X-CSRF-Token': csrfToken },
      });
      expect(toggleResponse.status()).toBe(404);

      // Try to create schedule with non-existent watchlist
      const createResponse = await page.request.post('/api/schedules', {
        data: {
          watchlist_id: '00000000-0000-0000-0000-000000000000',
          cron_expr: '0 6 * * *',
          enabled: true,
        },
        headers: { 'X-CSRF-Token': csrfToken },
      });
      expect(createResponse.status()).toBeGreaterThanOrEqual(400);
    });
  });
});
