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

      // The only break: the job reached its required final state. The probe
      // proves a refusal, so the job must end FAILED — a later retry
      // succeeding or an external cancel must not read as a pass.
      // NOTE: the Job model has no json tags, so Go marshals fields as
      // "ID"/"State" — read both casings.
      const jobsRes = await page.request.get(`/api/jobs/`, { headers: { 'X-CSRF-Token': csrf } });
      if (jobsRes.status() === 200) {
        const jobs = (await jobsRes.json()) as Array<Record<string, unknown>>;
        const job = jobs.find(j => (j['ID'] ?? j['id']) === jobId);
        const state = String(job?.['State'] ?? job?.['state'] ?? '');
        if (state === 'failed') {
          itemState = state;
          break;
        }
        if (['succeeded', 'cancelled'].includes(state)) {
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

    // Part 3: the item terminated in the required way — refused, not
    // imported, and the job FAILED (not succeeded by a fluke or cancelled by
    // an outside hand).
    expect(itemState, 'the seeded job must fail after the proxy refusal').toBe('failed');
    const failureLine = (await page.request.get(`/partials/job-logs?job_id=${jobId}`, {
      headers: { 'X-CSRF-Token': csrf },
    }).then(r => r.text())).split('\n').find(l => /yt-dlp fallback failed/i.test(l));
    expect(
      failureLine,
      'the item log must carry the fallback failure the boundary caused'
    ).toBeTruthy();
  });

  // The clause the acceptance record carries as "not driven live": a reachable
  // URL serving PLAYABLE audio that is not the requested work must be refused
  // by the identity gate (download_gate.go) — not by the guard in front of it
  // (this URL passes DJI-500's walk: the host is pinned via /etc/hosts to a
  // documentation-IPv6 address, 2001:db8:aa::/64, which no layer treats as
  // private) and not by the proxy (the allowlist... the host is not on it, so
  // squid must ALLOW it — it does, via dstdomain... no: the allowlist DENIES
  // unlisted domains. The e2e overlay therefore allowlists
  // audio-probe.e2e.test in allowed-domains.txt; see that file's e2e note).
  // yt-dlp downloads the FLAC through the proxy successfully; the gate reads
  // its tags, finds artist/album disagree with the request, and the item is
  // abandoned terminally with "does not match the request". Mutation: deleting
  // the gate call in stageYtdlpFallback (acquisition_pipeline.go) makes this
  // spec import the file and go green-failing — the spec asserts zero
  // acquisitions for the probe job.
  test('a reachable but wrong-work download is refused by the identity gate, not the boundary', async ({ adminPage }) => {
    const page = adminPage;
    const csrf = await getCsrfToken(page);

    const seed = await page.request.post('/api/test/seed-fallback-refusal', {
      data: {
        artist: 'Wrong Work Probe',
        album: 'DJI Gate Proof',
        // Served by the audio-probe container (docker-compose.e2e.yml): a
        // real 20s FLAC tagged "Totally Different Band / Unrelated Record" —
        // playable, plausible-sized, and sharing NO word with this request
        // (the gate folds names to words; one shared word reads as a legit
        // name variant and passes — the first live run proved exactly that).
        url: 'http://audio-probe.e2e.test:8080/wrong-work.flac',
      },
      headers: { 'X-CSRF-Token': csrf },
    });
    expect(seed.status()).toBe(200);
    const { job_id: jobId } = await seed.json();
    expect(jobId).toBeGreaterThan(0);

    // No retry loop here: the gate's rejection is terminal on the first
    // attempt (DJI-497), so this lifecycle is minutes shorter than the
    // boundary probe's — download, ffprobe, tag compare, abandon.
    const deadline = Date.now() + 180_000;
    let sawDownload = false;
    let sawGateRefusal = false;
    let jobState = '';

    while (Date.now() < deadline) {
      await page.waitForTimeout(4000);
      const logsText = await page.request
        .get(`/partials/job-logs?job_id=${jobId}`, { headers: { 'X-CSRF-Token': csrf } })
        .then(r => r.text());

      if (logsText.includes('Trying yt-dlp fallback')) {
        sawDownload = true;
      }

      // The gate's wording (download_gate.go): "<source> delivered a file that
      // does not match the request". The discriminator between the gate and
      // every earlier layer: the download SUCCEEDED first ("yt-dlp downloaded"),
      // so any refusal the guard or proxy produced would never reach this line.
      if (/yt-dlp downloaded/.test(logsText)) {
        sawDownload = true;
      }
      if (/does not match the request/.test(logsText)) {
        sawGateRefusal = true;
      }

      const jobsRes = await page.request.get('/api/jobs/', { headers: { 'X-CSRF-Token': csrf } });
      if (jobsRes.status() === 200) {
        const jobs = (await jobsRes.json()) as Array<Record<string, unknown>>;
        const job = jobs.find(j => (j['ID'] ?? j['id']) === jobId);
        const state = String(job?.['State'] ?? job?.['state'] ?? '');
        if (['failed', 'succeeded', 'cancelled'].includes(state)) {
          jobState = state;
          break;
        }
      }
    }

    expect(sawDownload, 'yt-dlp must have downloaded the probe FLAC (every layer before the gate passed)').toBe(true);
    expect(
      sawGateRefusal,
      'the identity gate must refuse the file: tags name a different work than the request'
    ).toBe(true);
    expect(jobState, 'the job must fail — the only item was refused').toBe('failed');

    // The refusal must be the gate's, with its own wording, and nothing may
    // have reached the library.
    const failureLine = await page.request
      .get(`/partials/job-logs?job_id=${jobId}`, { headers: { 'X-CSRF-Token': csrf } })
      .then(r => r.text())
      .then(t => t.split('\n').find(l => /does not match the request/.test(l)));
    expect(failureLine, 'the refusal line must name yt-dlp as the source').toMatch(/yt-dlp/);

    // Nothing may have reached the library. There is no acquisitions JSON
    // route, so the proof goes through the real library surface: create a
    // library, scan it, and list its tracks — the same flow a Subsonic    // client's view is built from.
    const libPath = `/tmp/wrong-work-probe-${Date.now()}`;
    await page.request.post('/api/test/create-dir', {
      data: { path: libPath },
      headers: { 'X-CSRF-Token': csrf },
    });
    const libRes = await page.request.post('/api/libraries', {
      data: { name: 'Wrong Work Probe', path: libPath },
      headers: { 'X-CSRF-Token': csrf },
    });
    expect(libRes.status(), 'library creation must succeed').toBeLessThan(300);
    const libId = (await libRes.json())['id'] ?? (await libRes.json())['ID'];
    const scanRes = await page.request.post(`/api/libraries/${libId}/scan`, {
      headers: { 'X-CSRF-Token': csrf },
    });
    expect(scanRes.status()).toBe(202);
    // Wait for the scan job to finish, then list the tracks it indexed.
    const scanDeadline = Date.now() + 30_000;
    let scanDone = false;
    while (Date.now() < scanDeadline && !scanDone) {
      await page.waitForTimeout(2000);
      const jobs = await page.request.get('/api/jobs/', { headers: { 'X-CSRF-Token': csrf } }).then(r =>
        r.status() === 200 ? (r.json() as Array<Record<string, unknown>>) : []
      );
      const scanJob = jobs.find(
        j => String(j['Type'] ?? j['type'] ?? '') === 'scan' &&
             String(j['ScopeID'] ?? j['scope_id'] ?? '') === String(libId)
      );
      const state = String(scanJob?.['State'] ?? scanJob?.['state'] ?? '');
      if (['failed', 'succeeded', 'cancelled'].includes(state)) scanDone = true;
    }
    const tracks = await page.request
      .get(`/api/libraries/${libId}/tracks`, { headers: { 'X-CSRF-Token': csrf } })
      .then(r => (r.status() === 200 ? (r.json() as Array<Record<string, unknown>>) : []));
    expect(tracks, 'nothing from the refused download may reach the library').toHaveLength(0);
  });
});
