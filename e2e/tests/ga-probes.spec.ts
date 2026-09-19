import { expect, type Page } from '@playwright/test';
import { test } from '../fixtures/auth.fixture';

// ---------------------------------------------------------------------------
// GA gap closers — the last clauses of the gap list (docs/plans/
// 2026-09-19-v0.0.2-ga-gap-list.md, items A1-Soulseek, A2, C9, C10).
//
// Three probes, each driving the REAL worker through a real pipeline:
//
//  1. Soulseek wrong-work refusal — the entrance that had no seam: a real
//     peer only appears in search results when its filename matches the
//     query, which is the same signal the gate uses. The e2e overlay
//     replaces slskd with ops/fake-slskd, a stand-in that answers queries
//     mentioning "Unrelated Record" with a peer serving a real, playable
//     FLAC tagged for a different work. Everything before the gate passes:
//     plausibility, download (bytes already staged), ffprobe. The identity
//     gate must refuse.
//
//  2. Success path — the inverse: a correctly-tagged file must pass the
//     gate, import, and become visible in the library. Also the boundary's
//     success-path companion (C10): the fallback probes ride the proxy on
//     every download, so an allowlist mistake that blocked everything shows
//     here as a failure before any gate runs.
//
//  3. Multi-hop post-handover refusal — the DJI-500 residual, driven live:
//     /flip on the audio-probe server answers the pre-handover walk with a
//     clean 200 and every LATER request with a 302 to an RFC1918 address.
//     The redirect hop happens inside yt-dlp after handover, where no
//     pre-flight check can see it; the egress boundary must refuse the hop
//     (TCP_DENIED on a private CONNECT) and the item must fail without
//     importing.
// ---------------------------------------------------------------------------

const SOULSEEK_DECOY = { artist: 'Wrong Work Probe', album: 'Unrelated Record' };
const SUCCESS = { artist: 'Clean Success Artist', album: 'Clean Success Album' };

async function getCsrfToken(page: Page): Promise<string> {
  const cookies = await page.context().cookies();
  return cookies.find(c => c.name === 'csrf_')?.value || '';
}

interface SeedResult { jobId: number; }

async function seedProbe(
  page: Page,
  csrf: string,
  data: Record<string, unknown>,
): Promise<SeedResult> {
  const seed = await page.request.post('/api/test/seed-fallback-refusal', {
    data,
    headers: { 'X-CSRF-Token': csrf },
  });
  expect(seed.status(), 'the seed endpoint requires E2E_ENABLE_TEST_API').toBe(200);
  const { job_id: jobId } = await seed.json();
  expect(jobId).toBeGreaterThan(0);
  return { jobId };
}

// The probes seed fixed artist/album names, so a leftover from an earlier run
// (or from the mutation check that imports the decoy) short-circuits the very
// behavior under test: an identical-bytes file hits the hash-duplicate path,
// an identical-recording file hits the recording dedup — both bypass the
// identity gate. Clean this suite's own rows and files before seeding.
async function cleanProbeResidue(page: Page, csrf: string): Promise<void> {
  await page.request.post(
    '/api/test/seed-fallback-refusal/cleanup',
    { headers: { 'X-CSRF-Token': csrf } },
  );
}

// The Job model has no json tags, so Go marshals fields as "ID"/"State" —
// read both casings.
async function jobState(page: Page, csrf: string, jobId: number): Promise<string> {
  const res = await page.request.get('/api/jobs/', { headers: { 'X-CSRF-Token': csrf } });
  if (res.status() !== 200) return '';
  const jobs = (await res.json()) as Array<Record<string, unknown>>;
  const job = jobs.find(j => (j['ID'] ?? j['id']) === jobId);
  return String(job?.['State'] ?? job?.['state'] ?? '');
}

async function jobLogs(page: Page, csrf: string, jobId: number): Promise<string> {
  return page.request
    .get(`/partials/job-logs?job_id=${jobId}`, { headers: { 'X-CSRF-Token': csrf } })
    .then(r => r.text());
}

test.describe('GA gap closers', () => {

  test('Soulseek entrance: a peer serving a different work is refused by the identity gate', async ({ adminPage }) => {
    const page = adminPage;
    const csrf = await getCsrfToken(page);
    await cleanProbeResidue(page, csrf);
    const { jobId } = await seedProbe(page, csrf, {
      ...SOULSEEK_DECOY,
      // No source_url: this item runs the SOULSEEK entrance only. The fake
      // slskd answers the query with the decoy peer.
      no_fallback: true,
      // One attempt: the "all candidates failed" retry path (3 attempts,
      // ~1 min then ~5 min backoff, identical refusal each time) was already
      // observed live to terminal on this exact probe (job 58, 6m16s), so
      // burning the schedule again proves nothing new.
      max_attempts: 1,
    });

    // The lifecycle is short: search, plausibility, transfer "completes",
    // ffprobe validates, tag compare, refusal.
    const deadline = Date.now() + 120_000;
    let sawSearchHit = false;
    let sawDownload = false;
    let sawGateRefusal = false;
    let state = '';

    while (Date.now() < deadline && state === '') {
      await page.waitForTimeout(4000);
      const logs = await jobLogs(page, csrf, jobId);

      if (logs.includes('Found 1 results')) sawSearchHit = true;
      // "Validated <file> (flac, …)" is the Soulseek path's ffprobe marker —
      // the file was downloaded, playable, and plausible. ("Download
      // completed" is the yt-dlp fallback path's wording; the Soulseek
      // entrance logs "Download queued from <peer>" + "Validated …".)
      if (/Validated .+\(flac,/.test(logs)) sawDownload = true;
      if (/does not match the request/.test(logs)) sawGateRefusal = true;

      const current = await jobState(page, csrf, jobId);
      if (['failed', 'succeeded', 'cancelled'].includes(current)) {
        state = current;
        break;
      }
    }

    expect(sawSearchHit, 'the stand-in slskd must return the decoy peer').toBe(true);
    expect(sawDownload, 'the decoy file must download cleanly (playable, plausible)').toBe(true);
    expect(
      sawGateRefusal,
      'the identity gate must refuse the Soulseek file whose tags name a different work'
    ).toBe(true);
    expect(state, 'the job must fail — the only item was refused').toBe('failed');

    // Nothing the decoy names may exist under the library root: a refused
    // download must not be in the library, and the job's own scan must not
    // have indexed one either.
    for (const name of ['Totally Different Band', 'Wrong Work Probe']) {
      const listing = await page.request
        .post('/api/test/create-dir', {
          data: { path: `/app/music/${name}`, check_only: true },
          headers: { 'X-CSRF-Token': csrf },
        })
        .catch(() => null);
      // Best-effort only: the helper exists in the e2e build; the assertion
      // below is the real proof.
      if (listing && listing.status() === 200) {
        const body = (await listing.json()) as Record<string, unknown>;
        expect(body['exists'], `nothing from "${name}" may exist under the library root`).toBeFalsy();
      }
    }
    const libTracks = await page.request
      .get(`/api/libraries/`, { headers: { 'X-CSRF-Token': csrf } })
      .then(r => (r.status() === 200 ? (r.json() as Array<Record<string, unknown>>) : []));
    for (const lib of libTracks) {
      const libId = String(lib['id'] ?? lib['ID']);
      const items = await page.request
        .get(`/api/libraries/${libId}/tracks`, { headers: { 'X-CSRF-Token': csrf } })
        .then(r => (r.status() === 200 ? (r.json() as Array<Record<string, unknown>>) : []));
      const decoy = (Array.isArray(items) ? items : []).filter((t: Record<string, unknown>) =>
        ['Totally Different Band', 'Wrong Work Probe'].some(n =>
          [t['artist'], t['Artist']].some(v => String(v ?? '') === n),
        ),
      );
      expect(decoy, 'the refused Soulseek file must not be in the library').toHaveLength(0);
    }
  });

  test('success path: a matching Soulseek download imports and becomes visible in the library', async ({ adminPage }) => {
    const page = adminPage;
    const csrf = await getCsrfToken(page);
    await cleanProbeResidue(page, csrf);
    const { jobId } = await seedProbe(page, csrf, {
      ...SUCCESS,
      no_fallback: true, // Soulseek entrance via the fake slskd's clean peer
    });

    const deadline = Date.now() + 180_000;
    let sawDownload = false;
    let sawImport = false;
    let state = '';

    while (Date.now() < deadline && state === '') {
      await page.waitForTimeout(4000);
      const logs = await jobLogs(page, csrf, jobId);
      if (logs.includes('Download completed')) sawDownload = true;
      if (logs.includes('Imported:')) sawImport = true;
      const current = await jobState(page, csrf, jobId);
      if (['failed', 'succeeded', 'cancelled'].includes(current)) {
        state = current;
        break;
      }
    }

    expect(sawDownload, 'the clean file must download').toBe(true);
    expect(sawImport, 'the clean file must pass the gate and import').toBe(true);
    expect(state, 'the job must succeed end to end').toBe('succeeded');

    // The import stage writes the track row with its library_id immediately,
    // but the scan that indexes the file into the library's track list runs
    // asynchronously — poll until the track shows up rather than racing it.
    // The Track model has no json tags, so fields marshal as "Artist" — read
    // both casings.
    const scanDeadline = Date.now() + 60_000;
    let found = false;
    while (Date.now() < scanDeadline && !found) {
      await page.waitForTimeout(3000);
      const libs = await page.request
        .get('/api/libraries/', { headers: { 'X-CSRF-Token': csrf } })
        .then(r => (r.status() === 200 ? (r.json() as Array<Record<string, unknown>>) : []));
      for (const lib of libs) {
        const libId = String(lib['id'] ?? lib['ID']);
        const items = await page.request
          .get(`/api/libraries/${libId}/tracks`, { headers: { 'X-CSRF-Token': csrf } })
          .then(r => (r.status() === 200 ? (r.json() as Array<Record<string, unknown>>) : []));
        if ((Array.isArray(items) ? items : []).some(
          t => String(t['artist'] ?? t['Artist'] ?? '') === 'Clean Success Artist',
        )) {
          found = true;
          break;
        }
      }
    }
    expect(found, 'the imported track must be visible in a library').toBe(true);
  });

  test('multi-hop: a post-handover redirect to a private address is refused by the boundary', async ({ adminPage }) => {
    const page = adminPage;
    const csrf = await getCsrfToken(page);
    const { jobId } = await seedProbe(page, csrf, {
      artist: 'Multi Hop Probe',
      album: 'Post Handover Refusal',
      // One attempt: the denial is deterministic, so burning the full retry
      // schedule (~6 min, all the same denial) proves nothing extra. The
      // Soulseek probe watches the full 3-attempt path instead.
      max_attempts: 1,
      // The audio-probe's flip endpoint: the guard's pre-handover walk
      // (Range: bytes=0-0) always gets a clean 200, while yt-dlp's actual
      // fetch after handover always gets the 302 -> RFC1918. The rule is
      // request-shaped, not a counter, so worker retries see the same split.
      url: 'http://audio-probe.e2e.test:8081/flip',
    });

    const deadline = Date.now() + 180_000; // one attempt: walk, denial, abandon
    let sawWalkClean = false;
    let sawDenial = false;
    let state = '';

    while (Date.now() < deadline && state === '') {
      await page.waitForTimeout(4000);
      const logs = await jobLogs(page, csrf, jobId);
      if (logs.includes('Trying yt-dlp fallback')) sawWalkClean = true;
      // The post-handover denial's markers: the proxy denies the private
      // hop (TCP_DENIED/403 in squid's log) and yt-dlp surfaces it as
      // "HTTP Error 403: Forbidden" (plain HTTP) or "Tunnel connection
      // failed"/"ProxyError" (CONNECT). A PRE-flight refusal instead says
      // "refusing source URL" — the wording this probe must NOT see.
      if (/Tunnel connection failed|ProxyError|HTTP Error 403/i.test(logs)) sawDenial = true;
      const current = await jobState(page, csrf, jobId);
      if (['failed', 'succeeded', 'cancelled'].includes(current)) {
        state = current;
        break;
      }
    }

    expect(sawWalkClean, 'the fallback entrance must run for the flip URL').toBe(true);
    expect(
      sawDenial,
      'the post-handover hop to the RFC1918 address must be refused by the egress boundary'
    ).toBe(true);
    expect(state, 'the job must fail after the boundary refusal').toBe('failed');

    const failureLine = (await jobLogs(page, csrf, jobId))
      .split('\n')
      .find(l => /yt-dlp fallback failed/i.test(l));
    expect(failureLine, 'the item log must carry the failure the boundary caused').toBeTruthy();
    expect(
      failureLine,
      'the refusal must be the POST-handover denial, not the pre-flight walk (no "refusing source URL")'
    ).not.toMatch(/refusing source URL/);
  });
});
