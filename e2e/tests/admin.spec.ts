import { expect } from '@playwright/test';
import { test } from '../fixtures/auth.fixture';

/**
 * Admin panel (DJI-432).
 *
 * The admin fixture logs in as the account `e2e/setup-test-db.sh` seeds with
 * `role='admin'`; the plain fixture's account is a normal user, which is what
 * the authorization specs need.
 */
test.describe('Admin Feature (DJI-432)', () => {
  test('1. Page loads for an admin - dashboard, heading and content slot', async ({ adminPage: page }) => {
    await page.goto('/admin');

    await expect(page.locator('.admin-dashboard')).toBeVisible();
    await expect(page.locator('.admin-dashboard h2')).toHaveText('Admin Panel');
    await expect(page.locator('#admin-content')).toBeAttached();
  });

  test('2. The panel offers all three sections', async ({ adminPage: page }) => {
    await page.goto('/admin');

    const links = page.locator('nav.admin-nav a');
    await expect(links).toHaveCount(3);
    await expect(page.locator('nav.admin-nav a:has-text("Users")')).toBeVisible();
    await expect(page.locator('nav.admin-nav a:has-text("Audit Log")')).toBeVisible();
    await expect(page.locator('nav.admin-nav a:has-text("System Config")')).toBeVisible();
  });

  test('3. Users loads the table with roles', async ({ adminPage: page }) => {
    await page.goto('/admin');

    const partial = page.waitForResponse(resp => resp.url().includes('/partials/admin/users') && resp.status() === 200);
    await page.locator('nav.admin-nav a:has-text("Users")').click();
    await partial;

    await expect(page.locator('#admin-content h3')).toHaveText('Users');
    await expect(page.locator('#admin-content table.admin-table')).toBeVisible();
    await expect(page.locator('#admin-content tr:has-text("e2e-admin@netrunner.dev")')).toBeVisible();
    await expect(page.locator('#admin-content tr:has-text("e2e-admin@netrunner.dev") .role-badge')).toHaveText('admin');
  });

  test('4. Audit Log loads its table', async ({ adminPage: page }) => {
    await page.goto('/admin');

    const partial = page.waitForResponse(resp => resp.url().includes('/partials/admin/audit') && resp.status() === 200);
    await page.locator('nav.admin-nav a:has-text("Audit Log")').click();
    await partial;

    await expect(page.locator('#admin-content h3')).toHaveText('Audit Log');
    await expect(page.locator('#admin-content table.admin-table')).toBeVisible();
    await expect(page.locator('#admin-content th')).toHaveCount(5);
  });

  test('5. System Config loads its table with editable settings', async ({ adminPage: page }) => {
    await page.goto('/admin');

    const partial = page.waitForResponse(resp => resp.url().includes('/partials/admin/config') && resp.status() === 200);
    await page.locator('nav.admin-nav a:has-text("System Config")').click();
    await partial;

    await expect(page.locator('#admin-content h3')).toHaveText('System Config');
    await expect(page.locator('#admin-content table.admin-table')).toBeVisible();
    // A fresh database has no settings rows yet, so the table's shape is what
    // this asserts; the Edit affordance is per row and only exists with one.
    await expect(page.locator('#admin-content th')).toHaveCount(3);
  });

  test('6. A section swap pushes a page URL, and the partial still answers directly', async ({ adminPage: page }) => {
    await page.goto('/admin');

    const partial = page.waitForResponse(resp => resp.url().includes('/partials/admin/audit') && resp.status() === 200);
    await page.locator('nav.admin-nav a:has-text("Audit Log")').click();
    await partial;

    // hx-push-url="true" made htmx push the *request* URL
    // (/partials/admin/audit), which is not a page route, so the address bar
    // pointed at nothing reloadable (DJI-499). It must name the page.
    //
    // waitForURL, not page.url(): the push lands with the swap, so reading
    // the location right after the response is a race.
    await page.waitForURL(url => url.pathname === '/admin' && url.searchParams.get('section') === 'audit');
    await expect(page.locator('#admin-content h3')).toHaveText('Audit Log');

    const direct = await page.request.get('/partials/admin/config');
    expect(direct.status()).toBe(200);
    expect(await direct.text()).toContain('System Config');
  });

  test('7. Admin API lists users for an admin', async ({ adminPage: page }) => {
    const response = await page.request.get('/api/admin/users');

    expect(response.status()).toBe(200);
    const body = await response.json();
    const users = Array.isArray(body) ? body : body.users;
    expect(Array.isArray(users)).toBe(true);
    // Marshalled from database.User, so the field is `Email`.
    expect(users.some((u: any) => (u.Email ?? u.email) === 'e2e-admin@netrunner.dev')).toBe(true);
  });

  test('8. A non-admin is refused the page with 403', async ({ authenticatedPage: page }) => {
    const response = await page.goto('/admin');

    expect(response?.status()).toBe(403);
    await expect(page.locator('.admin-dashboard')).toHaveCount(0);
  });

  test('9. A non-admin is refused every admin partial and endpoint', async ({ authenticatedPage: page }) => {
    await page.goto('/');

    for (const path of ['/partials/admin/users', '/partials/admin/audit', '/partials/admin/config', '/api/admin/users', '/api/admin/audit', '/api/admin/config']) {
      const response = await page.request.get(path);
      expect(response.status(), `${path} must be admin-only`).toBe(403);
    }
  });

  test('10. A section URL survives a reload (DJI-499)', async ({ adminPage: page }) => {
    const response = await page.goto('/admin?section=config');
    expect(response?.status()).toBe(200);

    // Rendered server-side, not only swapped in, so the panel is never empty
    // on a load that did not come from a click.
    await expect(page.locator('#admin-content h3')).toHaveText('System Config');

    await page.reload();

    await expect(page.locator('#admin-content h3')).toHaveText('System Config');
    expect(page.url()).toContain('section=config');
  });

  test('11. An unknown section still renders the panel (DJI-499)', async ({ adminPage: page }) => {
    const response = await page.goto('/admin?section=does-not-exist');

    expect(response?.status()).toBe(200);
    await expect(page.locator('#admin-content h3')).toHaveText('Users');
  });
});
