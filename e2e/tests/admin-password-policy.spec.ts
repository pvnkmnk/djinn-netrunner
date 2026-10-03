import { expect } from '@playwright/test';
import type { APIRequestContext, Page } from '@playwright/test';
import { test, ADMIN_USER } from '../fixtures/auth.fixture';

// DJI-600 -- the two admin password routes enforced NEITHER bound.
//
//   POST /api/admin/users                      CreateUser
//   POST /api/admin/users/:id/reset-password   ResetPassword
//
// Both hashed whatever they were given: over 72 BYTES returned 500 "internal
// server error", and a ONE-CHARACTER password was accepted outright -- on the
// same server where registration refused both after DJI-559.
//
// Scope note: there is no admin create-user or reset-password FORM. The admin
// Users panel (ops/web/templates/partials/admin_users.html) carries only a role
// toggle and a delete button, so these two routes are API-only surfaces today
// and "through the admin UI" means driving them from an admin-authenticated
// browser session -- the same adminPage fixture admin.spec.ts uses. If a form is
// ever added, this file is where its assertions belong.
//
// The boundary is pinned per route rather than the happy path: the longest
// password still accepted, the first refused, and 40 e-acutes, which are under
// 72 CHARACTERS and over 72 BYTES. A guard counting characters, or placed at 70
// bytes, passes a single "too long is refused" assertion and fails every case
// here.

const E_ACUTE = '\u00e9';
const CEILING_BYTES = 72;

const bytes = (s: string) => Buffer.byteLength(s, 'utf8');

async function csrf(page: Page): Promise<string> {
  const cookies = await page.context().cookies();
  return cookies.find(c => c.name === 'csrf_')?.value || '';
}

/** Every mutating request echoes the CSRF token, so a 403 gate can never pass. */
async function send(
  request: APIRequestContext,
  page: Page,
  path: string,
  data: Record<string, string>,
) {
  return request.post(path, {
    data,
    headers: { 'X-CSRF-Token': await csrf(page) },
  });
}

async function createUser(page: Page, email: string, password: string) {
  return send(page.request, page, '/api/admin/users', { email, password, role: 'user' });
}

async function resetPassword(page: Page, id: string, password: string) {
  return send(page.request, page, `/api/admin/users/${id}/reset-password`, { password });
}

/** Seed a user the reset route can be pointed at, with a compliant password. */
async function seedTarget(page: Page, email: string) {
  const response = await createUser(page, email, 'correct-horse-battery-staple');
  expect(response.status(), await response.text()).toBe(201);
  const created = await response.json();
  return String(created.id);
}

/**
 * The cases, with their own sizes asserted first: a boundary fixture that does
 * not measure itself is decoration. An eight-word passphrase is 57 bytes and
 * passes where it means to prove a refusal.
 */
function boundaryCases() {
  const multiByte = E_ACUTE.repeat(40); // 40 characters, 80 bytes
  const passphrase =
    'correct horse battery staple vanilla harbor candle drawer lonely summit kitten';
  const atCeiling = 'a'.repeat(CEILING_BYTES);

  expect(passphrase.length).toBeGreaterThan(CEILING_BYTES);
  expect(multiByte.length).toBeLessThan(CEILING_BYTES);
  expect(bytes(multiByte)).toBeGreaterThan(CEILING_BYTES);
  expect(bytes(atCeiling)).toBe(CEILING_BYTES);

  return [
    { name: 'exactly at the floor is accepted', password: 'a'.repeat(12), ok: true },
    { name: 'the longest accepted password is exactly 72 bytes', password: atCeiling, ok: true },
    { name: 'a one-character password is refused', password: 'x', ok: false, bound: 'characters' },
    { name: 'one below the floor is refused', password: 'a'.repeat(11), ok: false, bound: 'characters' },
    {
      name: 'the first password past the ceiling is refused',
      password: 'a'.repeat(CEILING_BYTES + 1),
      ok: false,
      bound: 'bytes',
    },
    { name: 'a long passphrase past the ceiling is refused', password: passphrase, ok: false, bound: 'bytes' },
    {
      name: 'under 72 characters but over 72 bytes is refused',
      password: multiByte,
      ok: false,
      bound: 'bytes',
    },
    {
      name: 'exactly at the ceiling in multi-byte text is accepted',
      password: E_ACUTE.repeat(CEILING_BYTES / 2),
      ok: true,
    },
  ] as const;
}

test.describe('Admin password policy (DJI-600)', () => {
  const stamp = Date.now();

  test.describe('POST /api/admin/users', () => {
    test('the admin session is real before any boundary claim', async ({ adminPage }) => {
      await adminPage.goto('/admin');
      const users = await adminPage.request.get('/api/admin/users');
      expect(users.status()).toBe(200);
    });

    for (const tc of boundaryCases()) {
      test(`CreateUser: ${tc.name}`, async ({ adminPage }) => {
        const email = `dji600-create-${Date.now()}-${Math.random().toString(36).slice(2, 8)}@netrunner.dev`;
        const response = await createUser(adminPage, email, tc.password);

        if (tc.ok) {
          expect(response.status(), await response.text()).toBe(201);
          return;
        }

        // The regression this ticket exists for: a 500 here is the bug.
        expect(response.status(), await response.text()).toBe(400);
        expect(response.status()).not.toBe(500);

        const body = await response.json();
        expect(body.error).toContain(tc.bound === 'bytes' ? `${CEILING_BYTES} bytes` : 'characters');
        if (tc.bound === 'bytes') {
          // The unit is the whole point: 40 e-acutes clear the character floor.
          expect(body.error).toContain('not characters');
          expect(body.maxBytes).toBe(CEILING_BYTES);
          expect(body.passwordBytes).toBe(bytes(tc.password));
        } else {
          expect(body.minLength).toBe(12);
          expect(body.passwordLength).toBe(tc.password.length);
        }
      });
    }

    test('CreateUser refuses identically for a known and an unknown address', async ({ adminPage }) => {
      const existing = `dji600-known-${Date.now()}@netrunner.dev`;
      expect((await createUser(adminPage, existing, 'correct-horse-battery-staple')).status()).toBe(201);

      // CreateUser answers 409 for a duplicate. If the policy ran after that,
      // an existing address and a new one would answer differently for the same
      // over-limit password, which enumerates the user table.
      const over = 'a'.repeat(CEILING_BYTES + 1);
      const known = await createUser(adminPage, existing, over);
      const unknown = await createUser(adminPage, `dji600-unknown-${Date.now()}@netrunner.dev`, over);

      expect(known.status()).toBe(400);
      expect(unknown.status()).toBe(400);
      expect(await known.json()).toEqual(await unknown.json());
    });
  });

  test.describe('POST /api/admin/users/:id/reset-password', () => {
    for (const tc of boundaryCases()) {
      test(`ResetPassword: ${tc.name}`, async ({ adminPage }) => {
        const id = await seedTarget(adminPage, `dji600-reset-${Date.now()}-${Math.random().toString(36).slice(2, 8)}@netrunner.dev`);
        const response = await resetPassword(adminPage, id, tc.password);

        if (tc.ok) {
          expect(response.status(), await response.text()).toBe(200);
          return;
        }
        expect(response.status(), await response.text()).toBe(400);
        expect(response.status()).not.toBe(500);
        expect((await response.json()).error).toContain(
          tc.bound === 'bytes' ? `${CEILING_BYTES} bytes` : 'characters',
        );
      });
    }

    test('a refused reset leaves the original password working', async ({ adminPage }) => {
      const good = 'correct-horse-battery-staple';
      const email = `dji600-intact-${Date.now()}@netrunner.dev`;
      const id = await seedTarget(adminPage, email);

      for (const bad of ['x', 'a'.repeat(11), 'a'.repeat(100), E_ACUTE.repeat(40)]) {
        const response = await resetPassword(adminPage, id, bad);
        expect(response.status(), `password ${bad.slice(0, 12)} must be refused`)
          .toBe(400);
      }

      // The credential still works: four refused resets must not have
      // overwritten the stored hash.
      const login = await adminPage.request.post('/api/auth/login', {
        data: { email, password: good },
        headers: { 'X-CSRF-Token': await csrf(adminPage) },
        maxRedirects: 0, // login answers 302 + Set-Cookie with an empty body
      });
      expect(login.status()).toBe(302);
    });
  });

  test('registration and both admin routes refuse the same password identically', async ({
    adminPage,
  }) => {
    // The assertion the whole refactor exists to enable: a shared helper that
    // one route forgets to call is invisible per-route and obvious here.
    const multiByte = E_ACUTE.repeat(40);
    const email = `dji600-parity-${stamp}@netrunner.dev`;

    const registration = await send(adminPage.request, adminPage, '/api/auth/register', {
      email,
      password: multiByte,
    });
    const create = await createUser(adminPage, `dji600-parity-admin-${stamp}@netrunner.dev`, multiByte);
    const id = await seedTarget(adminPage, `dji600-parity-reset-${stamp}@netrunner.dev`);
    const reset = await resetPassword(adminPage, id, multiByte);

    expect(registration.status()).toBe(400);
    expect(create.status()).toBe(400);
    expect(reset.status()).toBe(400);
    expect(await create.json()).toEqual(await registration.json());
    expect(await reset.json()).toEqual(await registration.json());
  });
});