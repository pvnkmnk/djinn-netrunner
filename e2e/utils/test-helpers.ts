import { type Page, type APIRequestContext } from '@playwright/test';

/**
 * Register a new user via API
 */
export async function registerUser(
  request: APIRequestContext,
  email: string,
  password: string
) {
  return request.post('/api/auth/register', {
    data: { email, password },
  });
}

/**
 * Login via API and return session info
 */
export async function loginUser(
  request: APIRequestContext,
  email: string,
  password: string
) {
  return request.post('/api/auth/login', {
    data: { email, password },
  });
}

/**
 * Wait for HTMX request to complete
 */
export async function waitForHtmx(page: Page) {
  await page.waitForFunction(() => {
    const htmx = (window as any).htmx;
    return htmx && !htmx.ajaxRequests?.length;
  }, { timeout: 5000 }).catch(() => {
    // htmx may not be loaded yet, that's ok
  });
}

/**
 * Generate unique test name to avoid collisions
 */
export function uniqueName(prefix: string): string {
  return `${prefix}-${Date.now()}-${Math.random().toString(36).slice(2, 7)}`;
}
