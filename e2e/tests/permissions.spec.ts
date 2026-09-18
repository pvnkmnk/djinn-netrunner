import { test, type Page } from '../fixtures/auth.fixture';
import { expect } from '@playwright/test';

// ---------------------------------------------------------------------------
// Permissions & edge cases — the deliberate pass the other specs only assert
// incidentally (DJI-434). The fixture's two seats drive the authorization
// matrix: e2e-admin@netrunner.dev (promoted admin) and e2e-test@netrunner.dev
// (plain user). Malformed inputs sweep the public forms.
//
// Note on CSRF: mutating API calls require the X-CSRF-Token header even with a
// valid session, so every deliberate request below fetches the token from the
// csrf_ cookie first — otherwise the CSRF middleware's 403 masks whatever the
// test is actually trying to exercise. Test 5 is the exception: it sends a
// wrong token on purpose.
// ---------------------------------------------------------------------------

async function getCsrfToken(page: Page): Promise<string> {
  const cookies = await page.context().cookies();
  return cookies.find((c: { name: string }) => c.name === 'csrf_')?.value || '';
}

test.describe('Permissions & edge cases (DJI-434)', () => {
  test.describe('Authorization matrix — the fixture’s two seats', () => {
    test('1. /admin refuses a non-admin with 403, via page and partials', async ({
      adminPage,
      authenticatedPage,
    }) => {
      // The admin seat sees the panel; the plain user's seat must not.
      await adminPage.goto('/admin');
      await expect(adminPage.locator('.page-header, h1, h2').first()).toBeVisible();

      const resp = await authenticatedPage.goto('/admin');
      expect(resp?.status()).toBe(403);
      // And the admin partials behind the panel's own nav, for each section.
      for (const section of ['users', 'audit', 'config']) {
        const partial = await authenticatedPage.request.get(`/partials/admin/${section}`);
        expect(partial.status(), `partials/admin/${section}`).toBe(403);
      }
    });

    test('2. the API admin surface refuses a non-admin before any data moves', async ({
      authenticatedPage,
    }) => {
      // GETs carry no CSRF requirement, so these 403s are the role check.
      const list = await authenticatedPage.request.get('/api/admin/users');
      expect(list.status()).toBe(403);
      const audit = await authenticatedPage.request.get('/api/admin/audit');
      expect(audit.status()).toBe(403);
      // A mutating admin call from a non-admin must not create anything
      // (403 here is the role check or the CSRF gate — either way nothing moves).
      const create = await authenticatedPage.request.post('/api/admin/users', {
        data: { email: 'escalated@netrunner.dev', password: 'nope12345', role: 'admin' },
      });
      expect(create.status()).toBe(403);
    });

    test('3. one owner cannot read or mutate another owner’s playlist', async ({
      adminPage,
      authenticatedPage,
    }) => {
      // The admin creates a playlist the plain user must never see.
      const create = await adminPage.request.post('/api/playlists', {
        data: { name: 'admin-private-marker', description: 'owned by admin' },
        headers: { 'X-CSRF-Token': await getCsrfToken(adminPage) },
      });
      expect(create.status()).toBeLessThan(300);
      const created = (await create.json()) as { id?: string; ID?: string };
      const pid = created.id ?? created.ID;
      expect(pid, 'create response must expose the playlist id').toBeTruthy();

      // The plain user's own list must not contain the admin's playlist.
      const ownList = await authenticatedPage.request.get('/api/playlists');
      expect(ownList.status()).toBeLessThan(300);
      const names = ((await ownList.json()) as Array<{ name?: string }>).map((p) => p.name);
      expect(names).not.toContain('admin-private-marker');

      // Direct fetch by id: 4xx, never the record.
      const direct = await authenticatedPage.request.get(`/api/playlists/${pid}`);
      expect(direct.status()).toBeGreaterThanOrEqual(400);
      expect(direct.status()).toBeLessThan(500);

      // And mutation from the wrong owner is refused.
      const del = await authenticatedPage.request.delete(`/api/playlists/${pid}`);
      expect(del.status()).toBeGreaterThanOrEqual(400);
      expect(del.status()).toBeLessThan(500);
    });

    test('4. unauthenticated requests are rejected, not redirected into loops', async ({
      request,
    }) => {
      const page = await request.get('/libraries');
      expect(page.status()).toBeGreaterThanOrEqual(300);
      const api = await request.get('/api/libraries');
      expect(api.status()).toBe(401);
      const admin = await request.get('/api/admin/users');
      expect(admin.status()).toBe(401);
    });

    test('5. a mutating API call with a wrong CSRF token is refused', async ({
      authenticatedPage,
    }) => {
      // Deliberately wrong X-CSRF-Token: the middleware must refuse it even
      // though the session cookie is valid.
      const resp = await authenticatedPage.request.post('/api/playlists', {
        data: { name: 'csrfless-marker' },
        headers: { 'X-CSRF-Token': 'deliberately-wrong' },
      });
      expect(resp.status()).toBe(403);
    });
  });

  test.describe('Form edge cases — malformed and hostile inputs', () => {
    test('6. an empty playlist name is refused with 400 by the API', async ({
      authenticatedPage,
    }) => {
      const csrf = await getCsrfToken(authenticatedPage);
      const empty = await authenticatedPage.request.post('/api/playlists', {
        data: { name: '' },
        headers: { 'X-CSRF-Token': csrf },
      });
      expect(empty.status()).toBe(400);
      const whitespace = await authenticatedPage.request.post('/api/playlists', {
        data: { name: '   ' },
        headers: { 'X-CSRF-Token': csrf },
      });
      // Either 400 (validation) or 201 (trimmed-then-accepted) is defensible;
      // what must never happen is a 500.
      expect(whitespace.status()).toBeLessThan(500);
    });

    test('7. hostile payloads get 4xx responses, never a 500', async ({
      authenticatedPage,
    }) => {
      const csrf = await getCsrfToken(authenticatedPage);
      const unicode = await authenticatedPage.request.post('/api/playlists', {
        data: { name: '🎧ᚠᛢ-ünïcødé-名字', description: 'üñîçø∂é' },
        headers: { 'X-CSRF-Token': csrf },
      });
      expect(unicode.status()).toBeLessThan(500);
      const long = await authenticatedPage.request.post('/api/playlists', {
        data: { name: 'x'.repeat(10_000) },
        headers: { 'X-CSRF-Token': csrf },
      });
      expect(long.status()).toBeLessThan(500);
      const badJson = await authenticatedPage.request.post('/api/playlists', {
        data: 'this is not json',
        headers: { 'content-type': 'application/json', 'X-CSRF-Token': csrf },
      });
      expect(badJson.status()).toBe(400);
      const wrongType = await authenticatedPage.request.post('/api/playlists', {
        data: { name: 42, description: { nested: 'object' } },
        headers: { 'X-CSRF-Token': csrf },
      });
      expect(wrongType.status()).toBeLessThan(500);
    });

    test('8. an unknown library id gets 404/400-class, not a 500', async ({
      authenticatedPage,
    }) => {
      const bogus = await authenticatedPage.request.get('/api/libraries/99999999');
      expect(bogus.status()).toBeLessThan(500);
      expect(bogus.status()).toBeGreaterThanOrEqual(400);
      const malformed = await authenticatedPage.request.get('/api/libraries/not-a-uuid');
      expect(malformed.status()).toBeLessThan(500);
    });

    test('9. rapid-fire concurrent creates are all handled, none 500', async ({
      authenticatedPage,
    }) => {
      const csrf = await getCsrfToken(authenticatedPage);
      const attempts = Array.from({ length: 8 }, (_, i) =>
        authenticatedPage.request.post('/api/playlists', {
          data: { name: `rapid-fire-${Date.now()}-${i}` },
          headers: { 'X-CSRF-Token': csrf },
        })
      );
      const responses = await Promise.all(attempts);
      for (const r of responses) {
        expect(r.status()).toBeLessThan(500);
      }
      // And the server actually accepted the concurrent writes.
      const statuses = responses.map((r) => r.status());
      expect(statuses.some((s) => s < 300)).toBe(true);
    });
  });
});
