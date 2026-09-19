import { test as base, type Page } from '@playwright/test';
import { execFileSync } from 'child_process';
import path from 'path';

type AuthFixtures = {
  authenticatedPage: Page;
  adminPage: Page;
};

// Shared credentials
const TEST_USER = { email: 'e2e-test@netrunner.dev', password: 'testpass123' };
const ADMIN_USER = { email: 'e2e-admin@netrunner.dev', password: 'admin123' };
const REPO_ROOT = path.resolve(__dirname, '..');

async function loginViaAPI(page: Page, user: { email: string; password: string }) {
  // Visit page to get CSRF cookie
  await page.goto('/');
  const cookies = await page.context().cookies();
  const csrfToken = cookies.find(c => c.name === 'csrf_')?.value || '';

  // Login via API
  const loginResponse = await page.request.post('/api/auth/login', {
    data: { email: user.email, password: user.password },
    headers: { 'X-CSRF-Token': csrfToken }
  });

  if (!loginResponse.ok()) {
    throw new Error(`Login failed: ${loginResponse.status()}`);
  }

  // Navigate to dashboard to verify login worked
  await page.goto('/');
  await page.waitForSelector('.dashboard', { timeout: 5000 });
}

async function ensureUserExists(page: Page, user: { email: string; password: string }) {
  // Visit page to get CSRF cookie
  await page.goto('/');
  const cookies = await page.context().cookies();
  const csrfToken = cookies.find(c => c.name === 'csrf_')?.value || '';

  // Try to register (may fail if user already exists)
  const registerResponse = await page.request.post('/api/auth/register', {
    data: { email: user.email, password: user.password },
    headers: { 'X-CSRF-Token': csrfToken }
  });
  
  // 201 = created, 409 = already exists (both fine)
  if (!registerResponse.ok() && registerResponse.status() !== 409) {
    throw new Error(`Registration failed: ${registerResponse.status()}`);
  }
}

// Resolve the docker binary explicitly. Windows dev shells frequently lack it
// on the PATH a spawned process inherits (CI runners have it), so rely on the
// PATH only as one candidate among several -- never as the assumption.
let dockerBinCache: string | null | undefined;

function resolveDockerBin(): string | null {
  if (dockerBinCache !== undefined) return dockerBinCache;

  const candidates = [
    process.env.DOCKER_BIN,
    'docker',
    process.env.ProgramFiles
      ? path.join(process.env.ProgramFiles, 'Docker', 'Docker', 'resources', 'bin', 'docker.exe')
      : undefined,
  ].filter((c): c is string => !!c);

  for (const candidate of candidates) {
    try {
      execFileSync(candidate, ['--version'], { stdio: 'pipe', timeout: 5000 });
      dockerBinCache = candidate;
      return candidate;
    } catch {
      // Not runnable here -- try the next candidate.
    }
  }
  dockerBinCache = null;
  return null;
}

const PROMOTE_SQL = "UPDATE users SET role='admin' WHERE email='e2e-admin@netrunner.dev';";

function promoteAdminUser(): void {
  const dockerBin = resolveDockerBin();
  if (!dockerBin) {
    console.warn(
      'Could not resolve a docker binary (tried DOCKER_BIN, PATH, Docker Desktop default); ' +
        'admin promotion skipped and admin-seat tests will fail unless the role is already set.'
    );
    return;
  }

  // Try docker exec with the e2e overlay container name first (see docker-compose.e2e.yml)
  try {
    execFileSync(
      dockerBin,
      ['exec', 'e2e-postgres', 'psql', '-U', 'musicops', '-d', 'musicops_test', '-c', PROMOTE_SQL],
      { timeout: 15000, stdio: 'pipe' }
    );
    return;
  } catch (e1: any) {
    console.warn('Could not promote admin user via docker exec:', e1.message);
  }

  // Fallback: try docker compose exec (handles different container name schemes)
  try {
    execFileSync(
      dockerBin,
      [
        'compose',
        '--env-file',
        '../.env.e2e',
        '-f',
        '../docker-compose.yml',
        '-f',
        '../docker-compose.e2e.yml',
        'exec',
        '-T',
        'postgres',
        'psql',
        '-U',
        'musicops',
        '-d',
        'musicops_test',
        '-c',
        PROMOTE_SQL,
      ],
      { cwd: path.resolve(__dirname, '..'), timeout: 15000, stdio: 'pipe' }
    );
  } catch (e2: any) {
    console.warn('Could not promote admin user via compose exec:', e2.message);
  }
}

export const test = base.extend<AuthFixtures>({
  authenticatedPage: async ({ browser }, use) => {
    const context = await browser.newContext();
    const page = await context.newPage();

    // Ensure user exists
    await ensureUserExists(page, TEST_USER);
    
    // Login fresh for this test
    await loginViaAPI(page, TEST_USER);

    await use(page);
    await context.close();
  },

  adminPage: async ({ browser }, use) => {
    const context = await browser.newContext();
    const page = await context.newPage();

    // Ensure admin exists
    await ensureUserExists(page, ADMIN_USER);

    // Promote admin role in database before generating the session
    promoteAdminUser();

    // Login fresh for this test (session will have admin role)
    await loginViaAPI(page, ADMIN_USER);

    await use(page);
    await context.close();
  },
});
