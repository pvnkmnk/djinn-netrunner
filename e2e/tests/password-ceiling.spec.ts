import { expect } from '@playwright/test';
import type { APIRequestContext, Page } from '@playwright/test';
import { test } from '../fixtures/auth.fixture';

// DJI-559 -- registration answered 500 "failed to hash password" for any
// password past bcrypt's 72-byte limit. That is a server fault for something a
// person did perfectly reasonably: a browser states no length limit on a
// password field, and a 128-bit Diceware passphrase is ten words (well over 72
// bytes), as is anything a password manager emits with symbols.
//
// This spec pins the BOUNDARY, not the happy path, because "long passwords are
// refused" is a claim that a guard placed at 70 bytes would also satisfy:
//
//   * the LONGEST password still accepted  (exactly 72 bytes) -- and it must
//     then log in, so the limit is a real boundary and not an off-by-one that
//     stores a hash nothing can verify;
//   * the FIRST password refused          (73 bytes);
//   * the case that separates the two limits: 40 accented characters, which is
//     under 72 CHARACTERS and over 72 BYTES. A ceiling counted in characters
//     waves that through and bcrypt still refuses it.
//
// The floor is 12 characters and the ceiling is 72 bytes. They are different
// units, so the refusal has to name which one was hit; "password too long"
// tells a user nothing when they satisfied every rule the form shows.

const E_ACUTE = '\u00e9'; // two UTF-8 bytes: the whole point of the last case
const CEILING_BYTES = 72; // bcrypt's limit; the server reads this from config.BcryptMaxPasswordBytes

async function csrf(page: Page): Promise<string> {
  const cookies = await page.context().cookies();
  return cookies.find(c => c.name === 'csrf_')?.value || '';
}

/** Register with the CSRF token echoed, so a 403 gate can never pass a test. */
async function register(
  request: APIRequestContext,
  token: string,
  email: string,
  password: string,
) {
  return request.post('/api/auth/register', {
    data: { email, password },
    headers: { 'X-CSRF-Token': token },
  });
}

/** Byte length, computed the way the server computes it. */
const bytes = (s: string) => Buffer.byteLength(s, 'utf8');

test.describe('Registration password ceiling (DJI-559)', () => {
  const stamp = Date.now();

  test('the longest accepted password is exactly 72 bytes and it can log in', async ({ page }) => {
    await page.goto('/');
    const token = await csrf(page);
    const password = 'a'.repeat(CEILING_BYTES);
    expect(bytes(password)).toBe(CEILING_BYTES);

    const email = `at-ceiling-${stamp}@netrunner.dev`;
    const response = await register(page.request, token, email, password);
    expect(response.status(), await response.text()).toBe(201);

    // A boundary that stores a hash nothing can verify would pass the 201 above.
    // Login proves the credential round-trips at exactly the ceiling.
    // maxRedirects: 0 because login answers 302 + Set-Cookie with an empty body
    // and page.request follows redirects by default, reporting the final 200.
    const login = await page.request.post('/api/auth/login', {
      data: { email, password },
      headers: { 'X-CSRF-Token': token },
      maxRedirects: 0,
    });
    expect(login.status()).toBe(302);
    const cookies = await page.context().cookies();
    expect(cookies.some(c => c.name === 'session_id')).toBe(true);
  });

  test('the first password past the ceiling is refused with 400, never 500', async ({ page }) => {
    await page.goto('/');
    const token = await csrf(page);
    const password = 'a'.repeat(CEILING_BYTES + 1);
    expect(bytes(password)).toBe(CEILING_BYTES + 1);

    const response = await register(
      page.request,
      token,
      `over-ceiling-${stamp}@netrunner.dev`,
      password,
    );
    expect(response.status()).toBe(400);
    expect(response.status()).not.toBe(500);

    const body = await response.json();
    expect(body.error).toContain(`${CEILING_BYTES} bytes`);
    expect(body.error).toContain('not characters');
    expect(body.maxBytes).toBe(CEILING_BYTES);
    expect(body.passwordBytes).toBe(CEILING_BYTES + 1);
  });

  test('a passphrase well past the ceiling is refused with 400, never 500', async ({ page }) => {
    await page.goto('/');
    const token = await csrf(page);
    // Eleven words -- what a 128-bit Diceware passphrase actually looks like,
    // and past the ceiling. Asserted here because the first draft of this test
    // used ten short words, landed on 71 bytes, and passed 201 instead of 400:
    // a passphrase test that does not measure its own passphrase is decoration.
    const password = 'correct horse battery staple vanilla harbor candle '
      + 'drawer lonely summit kitten';
    expect(bytes(password)).toBeGreaterThan(CEILING_BYTES);

    const response = await register(
      page.request,
      token,
      `passphrase-${stamp}@netrunner.dev`,
      password,
    );
    expect(response.status(), await response.text()).toBe(400);
    expect(response.status()).not.toBe(500);
    expect((await response.json()).passwordBytes).toBe(bytes(password));
  });

  test('a passphrase under 72 characters but over 72 bytes is refused', async ({ page }) => {
    await page.goto('/');
    const token = await csrf(page);
    const password = E_ACUTE.repeat(40); // 40 characters, 80 bytes

    // The assertion that makes this a different test rather than a repeat of
    // the one above: a rune-counted ceiling would accept it.
    expect(password.length).toBeLessThan(CEILING_BYTES);
    expect(bytes(password)).toBeGreaterThan(CEILING_BYTES);

    const response = await register(
      page.request,
      token,
      `multibyte-${stamp}@netrunner.dev`,
      password,
    );
    expect(response.status(), await response.text()).toBe(400);
    expect(response.status()).not.toBe(500);

    const body = await response.json();
    expect(body.error).toContain('not characters');
    expect(body.passwordBytes).toBe(80);
  });

  test('a multi-byte passphrase exactly at 72 bytes is accepted', async ({ page }) => {
    await page.goto('/');
    const token = await csrf(page);
    const password = E_ACUTE.repeat(CEILING_BYTES / 2); // 36 characters, 72 bytes
    expect(bytes(password)).toBe(CEILING_BYTES);
    expect(password.length).toBeLessThan(CEILING_BYTES);

    const email = `wide-at-ceiling-${stamp}@netrunner.dev`;
    const response = await register(page.request, token, email, password);
    expect(response.status(), await response.text()).toBe(201);
  });

  test('the refusal is identical for an existing and an unknown address', async ({ page }) => {
    await page.goto('/');
    const token = await csrf(page);
    const over = 'a'.repeat(CEILING_BYTES + 1);

    const existing = `existing-${stamp}@netrunner.dev`;
    expect((await register(page.request, token, existing, 'a'.repeat(16))).status()).toBe(201);

    // If the ceiling were checked after the duplicate lookup, the existing
    // address would answer 201 and the unknown one 400 -- which enumerates the
    // user table. The guard runs before the lookup for exactly this reason.
    const known = await register(page.request, token, existing, over);
    const unknown = await register(page.request, token, `unknown-${stamp}@netrunner.dev`, over);

    expect(known.status()).toBe(400);
    expect(unknown.status()).toBe(400);
    expect(await known.json()).toEqual(await unknown.json());
  });

  test('the register form states both the character floor and the byte ceiling', async ({ page }) => {
    await page.goto('/');
    await page.locator('#show-register').click();

    const hint = page.locator('#reg-password-hint');
    await expect(hint).toContainText('At least 12 characters');
    await expect(hint).toContainText(`at most ${CEILING_BYTES} bytes`);
  });

  test('submitting an over-limit passphrase in the browser shows the limit, not a server error', async ({ page }) => {
    await page.goto('/');
    await page.locator('#show-register').click();

    await page.locator('#reg-email').fill(`form-${stamp}@netrunner.dev`);
    await page.locator('#reg-password').fill(E_ACUTE.repeat(40)); // 40 chars, 80 bytes
    await page.locator('#register-form button[type="submit"]').click();

    // This is the user-visible proof. The old behaviour rendered "failed to
    // hash password" here, because app.js shows data.error from any non-2xx
    // response -- including a 500.
    const error = page.locator('#register-error');
    await expect(error).toBeVisible();
    await expect(error).toContainText(`${CEILING_BYTES} bytes`);
    await expect(error).toContainText('not characters');
    await expect(error).not.toContainText('failed to hash password');
  });
});
