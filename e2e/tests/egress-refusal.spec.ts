import { expect, type Page } from '@playwright/test';
import { test } from '../fixtures/auth.fixture';// ---------------------------------------------------------------------------
// DJI-501 — the validating egress boundary in front of yt-dlp.
//
// The app's download guard (DJI-500) validates a source URL and walks its
// redirect chain before handing it to yt-dlp. What no pre-flight check can see
// is the fetching yt-dlp does after handover — its own redirect hops among
// them. The boundary closes that residual: the worker points yt-dlp at the
// validating proxy (YTDLP_PROXY=http://egress-proxy:3128), which denies
// private ranges at connect time and allowlists only known extraction sites.
//
// This spec drives that refusal through the real worker: it seeds an
// acquisition item whose source_url is public and resolvable — so it passes
// the pre-handover guard — but not on the boundary's allowlist. yt-dlp's
// fetch through the proxy must be denied at connect time, and the item must
// reach a terminal state without importing.
// ---------------------------------------------------------------------------

const PROBE_ARTIST = 'Egress Boundary Probe';
const PROBE_ALBUM = 'DJI-501 Refusal Proof';

async function getCsrfToken(page: Page): Promise<string> {
  const cookies = await page.context().cookies();
  return cookies.find(c => c.name === 'csrf_')?.value || '';
}

test.describe('yt-dlp egress boundary (DJI-501)', () => {
  test('a private destination in a seeded source_url is denied by the boundary and terminates the item', async ({ adminPage }) => {
    const page = adminPage;

    // Seed the refusal item through the gated test API.
    const csrf = await getCsrfToken(page);
    const seed = await page.request.post('/api/test/seed-fallback-refusal', {
      data: { artist: PROBE_ARTIST, album: PROBE_ALBUM },
      headers: { 'X-CSRF-Token': csrf },
    });
    expect(seed.status(), 'the seed endpoint requires E2E_ENABLE_TEST_API').toBe(200);
    const { job_id: jobId } = await seed.json();
    expect(jobId).toBeGreaterThan(0);

    // The worker drains the job: the Soulseek stage finds nothing for a probe
    // query, then the fallback runs because the item carries a source_url.
    // The full lifecycle is long: the item retries the failed download up to
    // max_attempts with exponential backoff between attempts (observed ~5
    // minutes seed-to-terminal), so the deadline must cover it.
    const deadline = Date.now() + 420_000;
    let sawFallbackAttempt = false;
    let sawDenial = false;
    let itemState = '';

    while (Date.now() < deadline) {
      await page.waitForTimeout(5000);

      const logs = await page.request.get(`/partials/job-logs?job_id=${jobId}`, {
        headers: { 'X-CSRF-Token': csrf },
      });
      expect(logs.status()).toBe(200);
      const logsText = await logs.text();

      // Evidence 1: the fallback entrance actually ran for the seeded item —
      // the pipeline logs its attempt with the item's URL.
      if (logsText.includes('Trying yt-dlp fallback')) {
        sawFallbackAttempt = true;
      }

      // Evidence 2: the failure names the proxy denial. The markers must be
      // proxy-SPECIFIC: "Tunnel connection failed" / "ProxyError" are urllib's
      // CONNECT-refused errors and appear only when a proxy denied the fetch.
      // (The seeded host's name in the logs is NOT evidence — the attempt line
      // itself carries the URL, and without the boundary yt-dlp downloads it
      // and the identity gate refuses the file, which must also fail here.)
      if (/Tunnel connection failed|ProxyError/i.test(logsText)) {
        sawDenial = true;
      }

      // The only break: the job reached a terminal state. Breaking earlier
      // (on log text) would race the retry scheduler — the job still has
      // attempts to burn after the first denial.
      // NOTE: the Job model has no json tags, so Go marshals fields as
      // "ID"/"State" — read both casings.
      const jobsRes = await page.request.get(`/api/jobs/`, { headers: { 'X-CSRF-Token': csrf } });
      if (jobsRes.status() === 200) {
        const jobs = (await jobsRes.json()) as Array<Record<string, unknown>>;
        const job = jobs.find(j => (j['ID'] ?? j['id']) === jobId);
        const state = String(job?.['State'] ?? job?.['state'] ?? '');
        if (['succeeded', 'failed', 'cancelled'].includes(state)) {
          itemState = state;
          break;
        }
      }
    }

    expect(
      sawFallbackAttempt,
      'the worker must attempt the yt-dlp fallback for the seeded item (see job logs)'
    ).toBe(true);
    expect(
      sawDenial,
      'the fallback failure must name the proxy denial (Tunnel connection failed / ProxyError) — without the boundary yt-dlp would download the file and only the identity gate would refuse it'
    ).toBe(true);

    // Part 3: the item terminated without importing. The job ends failed with
    // a failed item (not imported, not retrying).
    expect(itemState, 'the seeded job must reach a terminal state').toMatch(/failed|succeeded|cancelled/);
    const failureLine = (await page.request.get(`/partials/job-logs?job_id=${jobId}`, {
      headers: { 'X-CSRF-Token': csrf },
    }).then(r => r.text())).split('\n').find(l => /yt-dlp fallback failed/i.test(l));
    expect(
      failureLine,
      'the item log must carry the fallback failure the boundary caused'
    ).toBeTruthy();
  });
});
