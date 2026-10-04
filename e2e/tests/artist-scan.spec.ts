import { expect } from '@playwright/test';
import { test } from '../fixtures/auth.fixture';

/**
 * DJI-588 — adding a monitored artist queues its scan.
 *
 * Monitoring an artist used to be a bare INSERT: the row appeared on /artists,
 * nothing ever scanned it, and the only way to make the product keep its
 * promise was to notice the card and press Sync by hand. Measured on a running
 * stack before the fix: POST /api/artists answered 201, the artist row existed,
 * `select count(*) from jobs where job_type='artist_scan'` was 0, and
 * last_scan_date was NULL.
 *
 * The create path cannot be driven from here without MusicBrainz — a third
 * party whose availability and rate limit the e2e gate must not depend on
 * (which is why artists.spec.ts asserts validation and form wiring instead of a
 * live lookup). So this spec seeds through POST /api/test/seed-monitored-artist,
 * which calls the SAME ArtistTrackingService.AddMonitoredArtist the product
 * route calls. If the service stops queueing, the seed stops queueing and this
 * goes red with it. The route-level wiring is pinned by
 * backend/internal/api/artist_scan_queue_test.go and proved non-vacuous by
 * scripts/artist_scan_mutation_check.py in CI.
 */

const MBID = 'dji588-e2e-mbid';
const NAME = 'DJI588 Scan Fixture';

test.describe('Monitored artist scan is queued on add (DJI-588)', () => {
  // The e2e database is recreated per CI run, but a local run reuses the stack
  // (reuseExistingServer: !CI), so this spec removes what it added rather than
  // relying on a fresh database. Leaving the card behind would break the
  // "a fresh install says nothing is monitored" assertion in artists.spec.ts.
  let artistId = '';

  test.afterEach(async ({ authenticatedPage: page }) => {
    if (!artistId) return;
    // The CSRF header is NOT optional here. Without it the delete answers 403,
    // the artist row survives, and the NEXT test's seed fails with 400
    // "artist already monitored" -- a failure that reads like a product bug and
    // is really this cleanup silently not happening (DJI-434).
    const csrf = await getCsrfToken(page);
    const res = await page.request.delete(`/api/artists/${artistId}`, {
      headers: { 'X-CSRF-Token': csrf },
    });
    expect(res.status()).toBe(200);
    artistId = '';
  });

  // getCsrfToken is ASYNC. An un-awaited call yields the string
  // "[object Promise]" and every mutating request comes back 403 -- and a 403
  // is not the behaviour under test, it is the CSRF gate (DJI-441/DJI-434).
  async function getCsrfToken(page: any): Promise<string> {
    const cookies = await page.context().cookies();
    return cookies.find((c: any) => c.name === 'csrf_')?.value || '';
  }

  async function seedMonitoredArtist(page: any) {
    const csrf = await getCsrfToken(page);
    const res = await page.request.post('/api/test/seed-monitored-artist', {
      headers: { 'X-CSRF-Token': csrf },
      data: { name: NAME, musicbrainz_id: MBID },
    });
    expect(res.status()).toBe(201);
    const body = await res.json();
    artistId = body.artist.ID;
    return body;
  }

  test('1. the artist row and its scan reach the queue together', async ({ authenticatedPage: page }) => {
    const body = await seedMonitoredArtist(page);

    // The endpoint answers 500 rather than 201 when no scan was queued, so a
    // 201 here already means the job exists. Assert its shape as well: a scan
    // for a different artist, or one already settled, would keep the promise
    // looking kept while scanning nothing.
    expect(body.job_type).toBe('artist_scan');
    expect(body.job_state).toBe('queued');
    expect(body.artist.Name).toBe(NAME);
    expect(body.artist.MusicBrainzID).toBe(MBID);

    // And the Jobs page shows it, which is where an operator would look for the
    // promise being kept.
    await page.goto('/jobs');
    const card = page.locator(`#job-${body.job_id}`);
    await expect(card).toBeVisible();
    await expect(card.locator('.job-type')).toHaveText('artist_scan');
  });

  test('2. the artist card is there, and Sync answers the scan Add already queued', async ({ authenticatedPage: page }) => {
    await seedMonitoredArtist(page);

    await page.goto('/artists');
    const artistCard = page.locator(`#artist-${artistId}`);
    await expect(artistCard).toBeVisible();
    await expect(artistCard.locator('.name')).toContainText(NAME);

    // Pressing Sync now must NOT queue a second scan: Add already queued this
    // artist's, and the two routes share one enqueue owner. The operator is told
    // so in the notice region the button targets.
    await artistCard.getByRole('button', { name: /Sync discography for/ }).click();
    await expect(page.locator('#notice')).toContainText('Sync already active');

    // Exactly one scan for this artist, on the queue, after both calls.
    const jobs = await page.request.get(`/api/jobs/?job_type=artist_scan`);
    expect(jobs.status()).toBe(200);
    const all = await jobs.json();
    const scoped = (Array.isArray(all) ? all : []).filter(
      (j: any) => j.ScopeType === 'artist' && j.ScopeID === artistId
    );
    expect(scoped).toHaveLength(1);
  });
});
