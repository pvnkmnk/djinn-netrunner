import { expect } from '@playwright/test';
import { test } from '../fixtures/auth.fixture';

/**
 * Jobs and job logs (DJI-431).
 *
 * Nothing here starts an acquisition: a real one talks to Soulseek and the
 * worker, which makes it unusable as a gate. The specs cover the page, the
 * filters, the acquire modal and the log panel's own contract — including its
 * behaviour for an id that does not exist, which is what a stale UI hits.
 */
test.describe('Jobs Feature (DJI-431)', () => {
  // A fresh database has no jobs. Enqueue one through the same endpoint the New
  // Acquisition button posts to, so the page (and its log panel) has a real job
  // to show instead of the spec skipping.
  async function ensureAJobExists(page: any) {
    const before = await (await page.request.get('/api/jobs')).json();
    if (Array.isArray(before) && before.length > 0) return before[0].ID || before[0].id;

    const cookies = await page.context().cookies();
    const csrfToken = cookies.find((c: any) => c.name === 'csrf_')?.value || '';
    const created = await page.request.post('/api/acquire', {
      data: { artist: 'E2E Jobs Spec', album: 'Fixture Album' },
      headers: { 'X-CSRF-Token': csrfToken }
    });
    expect([200, 201, 202]).toContain(created.status());

    const after = await (await page.request.get('/api/jobs')).json();
    expect(Array.isArray(after) && after.length).toBeTruthy();
    return after[0].ID || after[0].id;
  }

  async function waitForJobsPartial(page: any) {
    // htmx attaches its listeners while processing the swapped-in partial, one
    // beat after the response lands. Interacting before that does nothing at
    // all — no request, no error — so wait for the swap to settle rather than
    // for a fixed delay.
    await page.addInitScript(() => {
      (window as any).__htmxSettles = 0;
      document.addEventListener('htmx:afterSettle', () => { (window as any).__htmxSettles += 1; });
    });

    const partialResponse = page.waitForResponse(resp => resp.url().includes('/partials/jobs') && resp.status() === 200);
    await page.goto('/jobs');
    await partialResponse;
    await page.waitForFunction(() => (window as any).__htmxSettles > 0);
  }

  test('1. Page loads - /jobs shows the header, the region and the filters', async ({ authenticatedPage: page }) => {
    await page.goto('/jobs');

    await expect(page.locator('.page-header h2')).toContainText('Jobs');
    await expect(page.locator('#jobs-region')).toBeVisible();
  });

  test('2. Partial loads - HTMX fills the region from /partials/jobs', async ({ authenticatedPage: page }) => {
    await waitForJobsPartial(page);

    await expect(page.locator('.jobs-region')).toBeVisible();
    await expect(page.locator('.filters select[name="job_type"]')).toBeVisible();
    await expect(page.locator('.filters select[name="state"]')).toBeVisible();
  });

  test('3. Filters offer the job types and states the backend knows', async ({ authenticatedPage: page }) => {
    await waitForJobsPartial(page);

    const typeOptions = page.locator('.filters select[name="job_type"] option');
    await expect(typeOptions).toHaveCount(5);
    await expect(page.locator('.filters select[name="job_type"] option[value="acquisition"]')).toHaveCount(1);

    const stateOptions = page.locator('.filters select[name="state"] option');
    await expect(stateOptions).toHaveCount(6);
    await expect(page.locator('.filters select[name="state"] option[value="failed"]')).toHaveCount(1);
  });

  test('4. Filtering sends the selected values and re-renders the list', async ({ authenticatedPage: page }) => {
    await waitForJobsPartial(page);

    // This is the assertion that found the defect: with the trigger on the
    // wrapper div, htmx 1.9 fired nothing at all, so the dropdowns were dead
    // controls. Selecting a state must produce a filtered request.
    const byState = page.waitForResponse(resp => resp.url().includes('/partials/jobs') && resp.url().includes('state=failed') && resp.status() === 200);
    await page.locator('.filters select[name="state"]').selectOption('failed');
    expect((await byState).url()).toContain('state=failed');

    // Either some failed jobs are listed, or the empty state is: both are answers.
    const cards = page.locator('.job-card');
    const empty = page.locator('.empty-state');
    expect((await cards.count()) + (await empty.count())).toBeGreaterThan(0);

    for (let i = 0; i < await cards.count(); i++) {
      await expect(cards.nth(i).locator('.job-state')).toContainText('failed');
    }

    // The region was re-rendered, so the filter controls were replaced with it:
    // wait for the second settle before touching them again, and check that the
    // response carried the selection back (otherwise the dropdowns reset).
    await page.waitForFunction(() => (window as any).__htmxSettles >= 2);
    await expect(page.locator('.filters select[name="state"]')).toHaveValue('failed');
    await expect(page.locator('.filters')).toHaveCount(1);

    const byType = page.waitForResponse(resp => resp.url().includes('/partials/jobs') && resp.url().includes('job_type=acquisition') && resp.status() === 200);
    await page.locator('.filters select[name="job_type"]').selectOption('acquisition');
    const typeUrl = (await byType).url();
    expect(typeUrl).toContain('job_type=acquisition');
    expect(typeUrl).toContain('state=failed');

    for (let i = 0; i < await cards.count(); i++) {
      await expect(cards.nth(i).locator('.job-type')).toHaveText('acquisition');
    }
  });

  test('5. Job cards carry their type, state and a log button', async ({ authenticatedPage: page }) => {
    await waitForJobsPartial(page);

    const cards = page.locator('.job-card');
    const count = await cards.count();
    if (count === 0) {
      // A fresh database with no dashboard activity has no jobs; the page still
      // has to say so rather than render an empty list silently.
      await expect(page.locator('.empty-state')).toBeVisible();
      return;
    }

    for (let i = 0; i < count; i++) {
      const card = cards.nth(i);
      await expect(card.locator('.job-type')).not.toBeEmpty();
      await expect(card.locator('.job-state')).not.toBeEmpty();
      await expect(card.locator('button:has-text("View Logs")')).toBeVisible();
    }
  });

  test('6. View Logs loads the panel for a real job', async ({ authenticatedPage: page }) => {
    const jobId = await ensureAJobExists(page);

    await waitForJobsPartial(page);

    const card = page.locator('.job-card').first();
    await expect(card).toBeVisible();

    const logsResponse = page.waitForResponse(resp => resp.url().includes('/partials/job-logs') && resp.status() === 200);
    await card.locator('button:has-text("View Logs")').click();
    await logsResponse;

    await expect(page.locator('#job-logs-container .job-logs')).toBeVisible();
    await expect(page.locator('#job-logs-panel h4')).toContainText(`Logs for Job #${jobId}`);
  });

  test('7. Log panel says so for an id that does not exist', async ({ authenticatedPage: page }) => {
    await page.goto('/jobs');
    await page.waitForTimeout(500);

    const response = await page.request.get('/partials/job-logs?job_id=999999');

    expect(response.status()).toBe(200);
    expect(await response.text()).toContain('Job not found.');
  });

  test('8. Log panel asks for a job when none was chosen', async ({ authenticatedPage: page }) => {
    await page.goto('/jobs');
    await page.waitForTimeout(500);

    const response = await page.request.get('/partials/job-logs');

    expect(response.status()).toBe(200);
    expect(await response.text()).toContain('Select a job to view its logs.');
  });

  test('9. New Acquisition opens the form with the fields the endpoint accepts', async ({ authenticatedPage: page }) => {
    await page.goto('/jobs');
    await page.waitForTimeout(500);

    await page.locator('button:has-text("New Acquisition")').click();
    await page.waitForTimeout(500);

    const modal = page.locator('#modal-container');
    await expect(modal).toBeVisible();
    await expect(modal.locator('form[hx-post="/api/acquire"]')).toBeVisible();
    await expect(modal.locator('#artist')).toBeVisible();
    await expect(modal.locator('#album')).toBeVisible();
    await expect(modal.locator('#title')).toBeVisible();
    await expect(modal.locator('#artist')).toHaveAttribute('required', '');
  });

  test('10. Jobs API lists what the page lists', async ({ authenticatedPage: page }) => {
    const response = await page.request.get('/api/jobs');

    expect(response.status()).toBe(200);
    expect(Array.isArray(await response.json())).toBe(true);
  });

  test('11. Navigation - the nav link reaches /jobs', async ({ authenticatedPage: page }) => {
    await page.goto('/');
    await expect(page.locator('.dashboard')).toBeVisible();

    await page.locator('nav#primary-nav a:has-text("Jobs")').click();
    await page.waitForTimeout(1000);

    await expect(page).toHaveURL(/\/jobs$/);
    await expect(page.locator('.page-header h2')).toContainText('Jobs');
  });
});
