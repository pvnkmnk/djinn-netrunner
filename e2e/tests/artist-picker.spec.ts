import { expect } from '@playwright/test';
import { test } from '../fixtures/auth.fixture';

/**
 * The Add Artist picker, driven the way an operator drives it.
 *
 * This flow used to be dead in the browser and no spec noticed. The candidate
 * row built its request in an htmx values expression beginning with "js:",
 * which htmx compiles with eval. The app serves `script-src 'self'` -- no
 * 'unsafe-eval' -- so clicking a candidate threw
 *
 *   Evaluating a string as JavaScript violates the following Content Security
 *   Policy directive because 'unsafe-eval' is not an allowed source of script
 *
 * issued NO request, and /artists ended with no artist. Measured before the
 * fix: 5 candidates rendered, the click produced zero requests, one page error,
 * 0 artist cards.
 *
 * artists.spec.ts cannot catch that: it asserts the form searches and that the
 * modal wiring is right, then stops. artist-scan.spec.ts cannot either: it
 * seeds through the test API and never renders the picker. So the picker had no
 * coverage at all, which is how a shipped flow sat inert for this long.
 *
 * This spec used to depend on musicbrainz.org being reachable, which made it a
 * canary for a third party's uptime: an outage produced a red CI run on an
 * unrelated commit. It could not simply skip, because scripts/e2e_gate.sh treats
 * DECLARED_SKIPS as exact in BOTH directions -- a conditional skip either fails
 * the gate as an undeclared skip, or fails it as a declared skip that stopped
 * running (DJI-603).
 *
 * That dependency is gone. The e2e stack serves MusicBrainz from a local
 * stand-in (ops/fake-musicbrainz) and the service reads MUSICBRAINZ_URL to find
 * it, so the search below is a fixed answer on every run. What is left is a real
 * assertion rather than an excuse: the no-alert check below now fails because
 * the SEAM is misconfigured -- the override missing, the service ignoring it, the
 * stand-in unreachable -- and not because someone else's server was down.
 */

// A real artist with more than one plausible match, so the spec exercises the
// ambiguous case -- the picker renders a list of one for a unique name, and a
// list of one is not the thing that was broken.
const ARTIST = 'Boards of Canada';

test.describe('Add Artist picker (CSP-safe selection)', () => {
  // The e2e database is recreated per CI run, but a local run reuses the stack,
  // so whatever this spec adds is removed again. Leaving the card behind would
  // break the "a fresh install says nothing is monitored" assertion in
  // artists.spec.ts.
  let artistId = '';
  let jobId = 0;

  async function getCsrfToken(page: any): Promise<string> {
    const cookies = await page.context().cookies();
    return cookies.find((c: any) => c.name === 'csrf_')?.value || '';
  }

  test.afterEach(async ({ authenticatedPage: page }) => {
    if (!artistId) return;
    // CSRF on every deliberate mutating request: without it the delete answers
    // 403 and this cleanup silently does nothing (DJI-434).
    const csrf = await getCsrfToken(page);
    if (jobId) {
      // Stop the worker claiming a scan for an artist that is about to go, or
      // the job fails and the Jobs page keeps a Failed row nobody asked for.
      // That failure mode is DJI-601: a delete does not take its own queued
      // jobs with it, so the job outlives the artist and fails forever. This
      // spec has the job id, so it cancels.
      await page.request
        .post(`/api/jobs/${jobId}/cancel`, { headers: { 'X-CSRF-Token': csrf } })
        .catch(() => {});
    }
    const res = await page.request.delete(`/api/artists/${artistId}`, {
      headers: { 'X-CSRF-Token': csrf },
    });
    expect([200, 404]).toContain(res.status());
    artistId = '';
    jobId = 0;
  });

  test('searching, choosing a candidate and adding it all work under script-src self', async ({
    authenticatedPage: page,
  }) => {
    const errors: string[] = [];
    page.on('pageerror', (e) => errors.push(e.message));

    const posts: string[] = [];
    page.on('request', (r) => {
      if (r.url().includes('/api/artists') && r.method() === 'POST') // Strip scheme+host so the assertion reads a path. Matching the port as
      // well (^.*8080) couples the spec to one port: on any other APP_HTTP_PORT
      // the prefix survives and every path assertion fails for a reason that has
      // nothing to do with the picker.
      posts.push(r.url().replace(/^https?:\/\/[^/]+/, ''));
    });

    await page.goto('/artists');
    await page.locator('button:has-text("Add Artist")').click();
    await page.locator('#name').fill(ARTIST);
    await page.locator('#modal-container button[type=submit]').click();
    await page.waitForTimeout(3000);

    // The modal's own words for "MusicBrainz did not answer". The stand-in always
    // answers, so seeing this means the SEAM is broken -- MUSICBRAINZ_URL unset, the
    // service ignoring it, or the stand-in unreachable -- and the message says so.
    // It is no longer a note about someone else's uptime.
    await expect(
      page.locator('#modal-container [role=alert]'),
      'the e2e MusicBrainz stand-in did not answer: check MUSICBRAINZ_URL is set on ops-web and ops-worker, and that fake-musicbrainz is healthy'
    ).toHaveCount(0);

    expect(posts, 'the modal must search before it creates').toContain('/api/artists/search');

    const rows = page.locator('#modal-container .candidate-row');
    expect(await rows.count(), 'the search must render the candidates it returned').toBeGreaterThan(0);

    // Choose the row for exactly this artist, so an ambiguous match cannot make
    // this pass by picking the wrong entity.
    const chosen = rows.filter({ has: page.locator('.name', { hasText: new RegExp(`^${ARTIST}$`) }) }).first();
    await chosen.click();
    await page.waitForTimeout(3000);

    // The request the old markup never made.
    expect(posts, 'the pick must POST to /api/artists').toContain('/api/artists');

    // And nothing threw on the way: an eval attempt is exactly what the CSP
    // used to block, and it fails SILENTLY apart from this error.
    expect(errors, `page errors during the pick: ${errors.join(' | ')}`).toHaveLength(0);

    // The artist is really there, through the UI.
    const cards = page.locator('.artist-card').filter({ hasText: ARTIST });
    await expect(cards).toHaveCount(1);
    artistId = (await cards.first().getAttribute('id'))!.replace('artist-', '');

    // DJI-588: adding it queued its scan. This is the promise the picker was
    // blocking, so the picker spec asserts it rather than leaving it to a spec
    // that seeds around the UI.
    const jobs = await page.request.get('/api/jobs/?job_type=artist_scan');
    expect(jobs.status()).toBe(200);
    const all = await jobs.json();
    // Scope alone is not enough: an acquisition job is scoped to the artist
    // too, so a scan that already found a release contributes two rows here.
    // Filtering on the type is what makes "the scan is on the queue" the claim.
    const mine = (Array.isArray(all) ? all : []).filter(
      (j: any) => j.Type === 'artist_scan' && j.ScopeType === 'artist' && j.ScopeID === artistId
    );
    expect(mine).toHaveLength(1);
    jobId = mine[0].ID;
  });
});
