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
    // The item's full lifecycle (fallback attempt, retry backoff, terminal
    // state) takes minutes — the default 30s per-test timeout is what red
    // master's e2e.yml run died on. Locally this only ever passed because
    // every invocation passed an ad-hoc `--timeout` flag.
    test.setTimeout(480_000);
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

  // History: this probe used to drive the identity gate live — a reachable
  // URL serving PLAYABLE audio that is not the requested work, refused by the
  // gate (download_gate.go) after yt-dlp downloaded it through the proxy.
  // That worked because the host was pinned via /etc/hosts to a
  // documentation-IPv6 address, 2001:db8:aa::/64, which no layer treated as
  // private. #392 (DJI-683) closed that hole: the guard now refuses every
  // non-global destination, documentation space included, so the guard
  // refuses this URL before yt-dlp runs and the gate never sees the file.
  // The E2E spec now proves the guard refusal; the gate proof is the Go
  // test TestAcquisitionHandler_ExecuteItem_YtdlpFallbackIsGatedToo, which
  // already drove the same wrong-work file through the same pipeline with a
  // canned downloader and fails when the gate call is removed. The fixture (audio-probe container, doc network,
  // extra_hosts pin) stays: the guard must SEE a resolvable documentation
  // address to refuse it — an unresolvable name would prove nothing, a
  // LookupIP error is not a refusal.
  test('a documentation-address probe is refused by the pre-handover guard, not the boundary', async ({ adminPage }) => {
    const page = adminPage;
    const csrf = await getCsrfToken(page);

    const seed = await page.request.post('/api/test/seed-fallback-refusal', {
      data: {
        artist: 'Wrong Work Probe',
        album: 'DJI Gate Proof',
        // Served by the audio-probe container (docker-compose.e2e.yml): a
        // real 20s FLAC tagged "Totally Different Band / Unrelated Record".
        // The download never happens now — the guard refuses the host first —
        // but the URL must stay this one: the host is what the guard refuses,
        // and it must resolve to the documentation address to prove it.
        url: 'http://audio-probe.e2e.test:8080/wrong-work.flac',
      },
      headers: { 'X-CSRF-Token': csrf },
    });
    expect(seed.status()).toBe(200);
    const { job_id: jobId } = await seed.json();
    expect(jobId).toBeGreaterThan(0);

    // No retry loop here: the guard's refusal is terminal on the first
    // attempt (DJI-497's abandonment), so this lifecycle is minutes shorter
    // than the boundary probe's — walk, refusal, abandon.
    const deadline = Date.now() + 180_000;
    let sawFallbackAttempt = false;
    let sawGuardRefusal = false;
    let sawDownload = false;
    let jobState = '';

    while (Date.now() < deadline) {
      await page.waitForTimeout(4000);
      const logsText = await page.request
        .get(`/partials/job-logs?job_id=${jobId}`, { headers: { 'X-CSRF-Token': csrf } })
        .then(r => r.text());

      if (logsText.includes('Trying yt-dlp fallback')) {
        sawFallbackAttempt = true;
      }

      // The guard's wording (safe_http.go): "ssrf: target <host> resolves
      // to non-global IP <ip>". The discriminator between the guard and the
      // layers behind it: the download must NOT have happened, so a refusal
      // naming the gate ("does not match the request") or the proxy
      // ("Tunnel connection failed" / "ProxyError") would mean a layer the
      // guard was supposed to stop ran instead.
      if (/resolves to non-global IP/.test(logsText)) {
        sawGuardRefusal = true;
      }
      if (/yt-dlp downloaded/.test(logsText)) {
        sawDownload = true;
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

    expect(sawFallbackAttempt, 'the fallback entrance must run for the probe URL').toBe(true);
    expect(
      sawGuardRefusal,
      'the pre-handover guard must refuse the documentation address before yt-dlp runs'
    ).toBe(true);
    expect(sawDownload, 'yt-dlp must NOT have downloaded anything — the guard refused first').toBe(false);
    expect(jobState, 'the job must fail — the only item was refused').toBe('failed');

    // The refusal must be the guard's, with its own wording, and nothing may
    // have reached the library.
    const failureLine = await page.request
      .get(`/partials/job-logs?job_id=${jobId}`, { headers: { 'X-CSRF-Token': csrf } })
      .then(r => r.text())
      .then(t => t.split('\n').find(l => /yt-dlp fallback failed/i.test(l)));
    expect(failureLine, 'the item log must carry the fallback failure the guard caused').toBeTruthy();
    expect(failureLine, 'the refusal must name the non-global destination').toMatch(/non-global/);
    expect(
      failureLine,
      'the gate must not have run: nothing was downloaded, so no tag comparison happened'
    ).not.toMatch(/does not match the request/);    // Nothing may have reached the library. The import stage writes below
    // MUSIC_LIBRARY (the worker's libraryRoot), so the assertion must scan
    // THAT directory — a random /tmp path would always be empty and the
    // assertion would pass vacuously. The container's own music root is
    // mounted into ops-web too, so registering it as a library and scanning
    // it exercises the same flow a Subsonic client's view is built from.
    // The scan target is empty in a fresh DB; a seed could only have filled
    // it if the guard had let the file through.
    const libPath = '/app/music';
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
    // Wait for the scan job to reach a terminal state, then require SUCCEEDED
    // before listing tracks: a failed/cancelled scan proves nothing about the
    // library's contents, and listing early would read as an empty library.
    const scanDeadline = Date.now() + 30_000;
    let scanState = '';
    while (Date.now() < scanDeadline && scanState === '') {
      await page.waitForTimeout(2000);
      const jobs = await page.request.get('/api/jobs/', { headers: { 'X-CSRF-Token': csrf } }).then(r =>
        r.status() === 200 ? (r.json() as Array<Record<string, unknown>>) : []
      );
      const scanJob = jobs.find(
        j => String(j['Type'] ?? j['type'] ?? '') === 'scan' &&
             String(j['ScopeID'] ?? j['scope_id'] ?? '') === String(libId)
      );
      const state = String(scanJob?.['State'] ?? scanJob?.['state'] ?? '');
      if (['failed', 'succeeded', 'cancelled'].includes(state)) scanState = state;
    }
    expect(scanState, 'the library scan must have completed').toBe('succeeded');
    const tracks = await page.request
      .get(`/api/libraries/${libId}/tracks`, { headers: { 'X-CSRF-Token': csrf } })
      .then(r => (r.status() === 200 ? (r.json() as Array<Record<string, unknown>>) : []));
    // The scan covers the whole /app/music root, which other specs
    // legitimately populate (ga-probes' success path imports a real track
    // there; alphabetical order runs this spec first in CI, which is why the
    // collision never surfaced there). Assert on the refused download's
    // identity — the decoy's tag words (ops/audio-probe/entrypoint.sh), which
    // no other fixture shares — not global emptiness.
    const leaked = (Array.isArray(tracks) ? tracks : []).filter(t => {
      const artist = String(t['artist'] ?? t['Artist'] ?? '').toLowerCase();
      const album = String(t['album'] ?? t['Album'] ?? '').toLowerCase();
      return artist.includes('totally different band') || album.includes('unrelated record');
    });
    expect(leaked, 'nothing from the refused download may reach the library').toHaveLength(0);
  });
});

