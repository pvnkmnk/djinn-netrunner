import { expect } from '@playwright/test';
import { test } from '../fixtures/auth.fixture';

// Helper to get CSRF token from cookies
async function getCsrfToken(page: any): Promise<string> {
  const cookies = await page.context().cookies();
  return cookies.find((c: any) => c.name === 'csrf_')?.value || '';
}

// Helper to create a watchlist via API (needed for schedule creation)
async function createWatchlistViaAPI(page: any, name: string = 'Test Watchlist'): Promise<number> {
  const csrfToken = await getCsrfToken(page);
  const response = await page.request.post('/api/watchlists', {
    data: {
      name,
      source_type: 'spotify',
      source_uri: 'spotify:user:test:playlist:test',
      quality_profile_id: '11111111-1111-4111-8111-111111111111',
    },
    headers: { 'X-CSRF-Token': csrfToken },
  });
  
  if (!response.ok()) {
    throw new Error(`Failed to create watchlist: ${response.status()}`);
  }
  
  const watchlist = await response.json();
  return watchlist.ID;
}

// Helper to create a schedule via API
async function createScheduleViaAPI(page: any, watchlistId: number, cronExpr: string = '0 6 * * *', enabled: boolean = true): Promise<any> {
  const csrfToken = await getCsrfToken(page);
  const response = await page.request.post('/api/schedules', {
    data: {
      watchlist_id: watchlistId,
      cron_expr: cronExpr,
      enabled,
    },
    headers: { 'X-CSRF-Token': csrfToken },
  });
  
  if (!response.ok()) {
    throw new Error(`Failed to create schedule: ${response.status()}`);
  }
  
  return response.json();
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
    });
  });

  test.describe('Schedule CRUD via API', () => {
    test('7. Create schedule via API - use API to create a schedule, verify 200/201', async ({ authenticatedPage: page }) => {
      // Create a watchlist first
      const watchlistId = await createWatchlistViaAPI(page, 'API Test Watchlist');
      expect(watchlistId).toBeDefined();
      
      // Create schedule via API
      const csrfToken = await getCsrfToken(page);
      const response = await page.request.post('/api/schedules', {
        data: {
          watchlist_id: watchlistId,
          cron_expr: '0 6 * * *',
          enabled: true,
        },
        headers: { 'X-CSRF-Token': csrfToken },
      });
      
      expect(response.status()).toBeGreaterThanOrEqual(200);
      expect(response.status()).toBeLessThanOrEqual(201);
    });

    test('8. Schedule appears after creation - create schedule via API, verify it appears in the list with cron expression visible', async ({ authenticatedPage: page }) => {
      // Create a watchlist first
      const watchlistId = await createWatchlistViaAPI(page, 'UI Test Watchlist');
      
      // Create schedule via API
      const schedule = await createScheduleViaAPI(page, watchlistId, '0 8 * * *');
      expect(schedule.ID).toBeDefined();
      
      // Navigate to schedules page
      await page.goto('/schedules');
      await page.waitForTimeout(1000);
      
      // Verify schedule appears in the list
      const scheduleCard = page.locator(`#schedule-${schedule.ID}`);
      await expect(scheduleCard).toBeVisible();
      
      // Verify cron expression is visible
      await expect(scheduleCard.locator('.details')).toContainText('0 8 * * *');
    });

    test('9. Schedule card details - verify schedule card shows watchlist name, cron expression, next run time', async ({ authenticatedPage: page }) => {
      // Create a watchlist first
      const watchlistId = await createWatchlistViaAPI(page, 'Details Test Watchlist');
      
      // Create schedule via API
      const schedule = await createScheduleViaAPI(page, watchlistId, '0 12 * * *');
      
      // Navigate to schedules page
      await page.goto('/schedules');
      await page.waitForTimeout(1000);
      
      // Verify schedule card exists
      const scheduleCard = page.locator(`#schedule-${schedule.ID}`);
      await expect(scheduleCard).toBeVisible();
      
      // Verify watchlist name is shown
      await expect(scheduleCard.locator('.name')).toContainText('Details Test Watchlist');
      
      // Verify cron expression is shown
      await expect(scheduleCard.locator('.details')).toContainText('0 12 * * *');
      
      // Verify next run time is shown (Next: label)
      await expect(scheduleCard.locator('.details')).toContainText('Next:');
    });
  });

  test.describe('Toggle Enable/Disable', () => {
    test('10. Toggle enable/disable - click Disable button, verify card updates (Disabled badge appears)', async ({ authenticatedPage: page }) => {
      // Create a watchlist and schedule
      const watchlistId = await createWatchlistViaAPI(page, 'Toggle Test Watchlist');
      const schedule = await createScheduleViaAPI(page, watchlistId, '0 6 * * *', true);
      
      // Navigate to schedules page
      await page.goto('/schedules');
      await page.waitForTimeout(1000);
      
      // Verify schedule card is visible
      const scheduleCard = page.locator(`#schedule-${schedule.ID}`);
      await expect(scheduleCard).toBeVisible();
      
      // Initially should not have disabled badge
      await expect(scheduleCard.locator('.badge-disabled')).not.toBeVisible();
      
      // Click Disable button
      await scheduleCard.locator('button:has-text("Disable")').click();
      
      // Wait for HTMX swap
      await page.waitForTimeout(500);
      
      // Verify Disabled badge appears
      await expect(scheduleCard.locator('.badge-disabled')).toBeVisible();
    });

    test('11. Toggle re-enable - click Enable button, verify Disabled badge disappears', async ({ authenticatedPage: page }) => {
      // Create a watchlist and disabled schedule
      const watchlistId = await createWatchlistViaAPI(page, 'Re-enable Test Watchlist');
      const schedule = await createScheduleViaAPI(page, watchlistId, '0 6 * * *', false);
      
      // Navigate to schedules page
      await page.goto('/schedules');
      await page.waitForTimeout(1000);
      
      // Verify schedule card is visible
      const scheduleCard = page.locator(`#schedule-${schedule.ID}`);
      await expect(scheduleCard).toBeVisible();
      
      // Initially should have disabled badge
      await expect(scheduleCard.locator('.badge-disabled')).toBeVisible();
      
      // Click Enable button
      await scheduleCard.locator('button:has-text("Enable")').click();
      
      // Wait for HTMX swap
      await page.waitForTimeout(500);
      
      // Verify Disabled badge disappears
      await expect(scheduleCard.locator('.badge-disabled')).not.toBeVisible();
    });
  });

  test.describe('Edit Schedule', () => {
    test('12. Edit schedule - click Edit button on a schedule, verify modal opens with pre-filled form', async ({ authenticatedPage: page }) => {
      // Create a watchlist and schedule
      const watchlistId = await createWatchlistViaAPI(page, 'Edit Test Watchlist');
      const schedule = await createScheduleViaAPI(page, watchlistId, '0 6 * * *');
      
      // Navigate to schedules page
      await page.goto('/schedules');
      await page.waitForTimeout(1000);
      
      // Verify schedule card exists
      const scheduleCard = page.locator(`#schedule-${schedule.ID}`);
      await expect(scheduleCard).toBeVisible();
      
      // Click Edit button
      await scheduleCard.locator('button:has-text("Edit")').click();
      
      // Wait for modal to appear
      await page.waitForTimeout(500);
      
      // Verify modal is visible
      const modal = page.locator('#modal-container');
      await expect(modal).toBeVisible();
      
      // Verify form is pre-filled with existing cron expression
      const cronInput = page.locator('#cron_expr');
      await expect(cronInput).toHaveValue('0 6 * * *');
      
      // Verify watchlist is pre-selected
      const watchlistSelect = page.locator('#watchlist_id');
      await expect(watchlistSelect).toBeVisible();
    });

    test('13. Update schedule via edit - modify cron expression in edit form, submit, verify changes reflected', async ({ authenticatedPage: page }) => {
      // Create a watchlist and schedule
      const watchlistId = await createWatchlistViaAPI(page, 'Update Test Watchlist');
      const schedule = await createScheduleViaAPI(page, watchlistId, '0 6 * * *');
      
      // Navigate to schedules page
      await page.goto('/schedules');
      await page.waitForTimeout(1000);
      
      // Verify schedule card exists
      const scheduleCard = page.locator(`#schedule-${schedule.ID}`);
      await expect(scheduleCard).toBeVisible();
      
      // Click Edit button
      await scheduleCard.locator('button:has-text("Edit")').click();
      
      // Wait for modal to appear
      await page.waitForTimeout(500);
      
      // Modify cron expression
      const cronInput = page.locator('#cron_expr');
      await cronInput.clear();
      await cronInput.fill('0 10 * * *');
      
      // Submit the form (click Save)
      await page.locator('button[type="submit"]:has-text("Save")').click();
      
      // Wait for HTMX swap
      await page.waitForTimeout(500);
      
      // Verify changes are reflected in the schedule card
      await expect(scheduleCard.locator('.details')).toContainText('0 10 * * *');
    });
  });

  test.describe('Delete Schedule', () => {
    test('14. Delete schedule with confirm - click Delete, handle confirm dialog, verify schedule removed from list', async ({ authenticatedPage: page }) => {
      // Create a watchlist and schedule to delete
      const watchlistId = await createWatchlistViaAPI(page, 'Delete Test Watchlist');
      const schedule = await createScheduleViaAPI(page, watchlistId, '0 6 * * *');
      
      // Navigate to schedules page
      await page.goto('/schedules');
      await page.waitForTimeout(1000);
      
      // Verify schedule card exists
      const scheduleCard = page.locator(`#schedule-${schedule.ID}`);
      await expect(scheduleCard).toBeVisible();
      
      // Set up dialog handler for confirmation
      page.on('dialog', async dialog => {
        expect(dialog.message()).toContain('Are you sure you want to delete');
        await dialog.accept();
      });
      
      // Click Delete button
      await scheduleCard.locator('button:has-text("Delete")').click();
      
      // Wait for HTMX swap
      await page.waitForTimeout(500);
      
      // Verify schedule is removed from the list
      await expect(scheduleCard).not.toBeVisible();
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
          watchlist_id: watchlistId,
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
    test('17. Multiple schedules - create 3 schedules, verify all display', async ({ authenticatedPage: page }) => {
      // Create three watchlists and schedules
      const watchlistId1 = await createWatchlistViaAPI(page, 'Multi Schedule Watchlist 1');
      const watchlistId2 = await createWatchlistViaAPI(page, 'Multi Schedule Watchlist 2');
      const watchlistId3 = await createWatchlistViaAPI(page, 'Multi Schedule Watchlist 3');
      
      const schedule1 = await createScheduleViaAPI(page, watchlistId1, '0 6 * * *');
      const schedule2 = await createScheduleViaAPI(page, watchlistId2, '0 12 * * *');
      const schedule3 = await createScheduleViaAPI(page, watchlistId3, '0 18 * * *');
      
      // Navigate to schedules page
      await page.goto('/schedules');
      await page.waitForTimeout(1000);
      
      // Verify all three schedule cards are visible
      await expect(page.locator(`#schedule-${schedule1.ID}`)).toBeVisible();
      await expect(page.locator(`#schedule-${schedule2.ID}`)).toBeVisible();
      await expect(page.locator(`#schedule-${schedule3.ID}`)).toBeVisible();
      
      // Verify they show correct cron expressions
      await expect(page.locator(`#schedule-${schedule1.ID} .details`)).toContainText('0 6 * * *');
      await expect(page.locator(`#schedule-${schedule2.ID} .details`)).toContainText('0 12 * * *');
      await expect(page.locator(`#schedule-${schedule3.ID} .details`)).toContainText('0 18 * * *');
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
    test('19. Schedule with disabled watchlist - create schedule for disabled watchlist, verify it still shows', async ({ authenticatedPage: page }) => {
      // Create a watchlist
      const watchlistId = await createWatchlistViaAPI(page, 'Disabled Watchlist Test');
      
      // Disable the watchlist
      await disableWatchlist(page, watchlistId);
      
      // Create schedule for the disabled watchlist
      const schedule = await createScheduleViaAPI(page, watchlistId, '0 6 * * *');
      
      // Navigate to schedules page
      await page.goto('/schedules');
      await page.waitForTimeout(1000);
      
      // Verify schedule still appears (even though watchlist is disabled)
      const scheduleCard = page.locator(`#schedule-${schedule.ID}`);
      await expect(scheduleCard).toBeVisible();
      await expect(scheduleCard.locator('.details')).toContainText('0 6 * * *');
    });
  });

  test.describe('Disabled State Persistence', () => {
    test('20. Disabled state persistence - toggle off, refresh page, verify still disabled', async ({ authenticatedPage: page }) => {
      // Create a watchlist and schedule
      const watchlistId = await createWatchlistViaAPI(page, 'Persistence Test Watchlist');
      const schedule = await createScheduleViaAPI(page, watchlistId, '0 6 * * *', true);
      
      // Navigate to schedules page
      await page.goto('/schedules');
      await page.waitForTimeout(1000);
      
      // Disable the schedule
      const scheduleCard = page.locator(`#schedule-${schedule.ID}`);
      await scheduleCard.locator('button:has-text("Disable")').click();
      await page.waitForTimeout(500);
      
      // Verify disabled badge is visible
      await expect(scheduleCard.locator('.badge-disabled')).toBeVisible();
      
      // Refresh the page
      await page.reload();
      await page.waitForTimeout(1000);
      
      // Verify schedule is still disabled after refresh
      const refreshedCard = page.locator(`#schedule-${schedule.ID}`);
      await expect(refreshedCard).toBeVisible();
      await expect(refreshedCard.locator('.badge-disabled')).toBeVisible();
    });
  });

  test.describe('Error Handling', () => {
    test('21. Error handling - test API errors (e.g., invalid schedule data)', async ({ authenticatedPage: page }) => {
      // Try to get non-existent schedule
      const csrfToken = await getCsrfToken(page);
      const getResponse = await page.request.get('/api/schedules/99999', {
        headers: { 'X-CSRF-Token': csrfToken },
      });
      expect(getResponse.status()).toBe(404);
      
      // Try to delete non-existent schedule
      const deleteResponse = await page.request.delete('/api/schedules/99999', {
        headers: { 'X-CSRF-Token': csrfToken },
      });
      expect(deleteResponse.status()).toBe(404);
      
      // Try to toggle non-existent schedule
      const toggleResponse = await page.request.patch('/api/schedules/99999/toggle', {
        headers: { 'X-CSRF-Token': csrfToken },
      });
      expect(toggleResponse.status()).toBe(404);
      
      // Try to create schedule with non-existent watchlist
      const createResponse = await page.request.post('/api/schedules', {
        data: {
          watchlist_id: 99999,
          cron_expr: '0 6 * * *',
          enabled: true,
        },
        headers: { 'X-CSRF-Token': csrfToken },
      });
      expect(createResponse.status()).toBeGreaterThanOrEqual(400);
    });
  });
});
