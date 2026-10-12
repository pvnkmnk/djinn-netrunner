#!/usr/bin/env node
// UI visual harness -- capture one named batch of surfaces from a running
// NetRunner stack.
//
// It asserts nothing about the app: it records what the app rendered, so a
// before/after pair can be diffed after a UI change (see compare.mjs). The
// surface list is data, not code -- e2e/visual/surfaces.json.
//
// Entry point:  scripts/ui-visual.sh capture <label>
// Direct:       node e2e/visual/capture.mjs --label before --base-url http://127.0.0.1:8080
//
// Auth, first match wins:
//   1. --session <id> / UI_VIS_SESSION    a session_id cookie value
//   2. <out-root>/.session                written by `ui-visual.sh mint-session`
//   3. UI_VIS_EMAIL + UI_VIS_PASSWORD     log in as that account
//   4. register + log in the default harness account
//
// Output: <out-root>/<label>/*.png + manifest.json

import { chromium } from 'playwright';
import { createHash } from 'node:crypto';
import { execFileSync } from 'node:child_process';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
const REPO_ROOT = path.resolve(here, '..', '..');
const DEFAULT_OUT_ROOT = path.join(REPO_ROOT, 'e2e', 'visual-output');
const VIEWPORTS = {
  desktop: { width: 1280, height: 800 },
  mobile: { width: 390, height: 844 },
};
// The pink Playwright paints over masked regions. Fixed so a before/after pair
// can never differ merely because the mask colour moved.
const MASK_COLOR = '#ff00ff';

// Values the server re-mints on every response. Left in, they make every page's
// markup hash differ between two captures of an unchanged page -- measured: the
// CSRF token in the sign-out form was the only difference between two loads.
const VOLATILE_IN_DOM = ['input[name="csrf_token"]', 'meta[name="csrf-token"]'];
const NAV_TIMEOUT_MS = 30_000;
const SETTLE_MS = 350;
const DEFAULT_USER = { email: 'ui-visual@netrunner.dev', password: 'uiVisualPass1234' };

let browser;

function fail(message) {
  console.error(`\n  error: ${message}\n`);
  process.exit(2);
}

function printHelp() {
  console.log(`
  capture.mjs -- record a named batch of UI surfaces

    --label <name>        batch name: before / after / anything (required)
    --base-url <url>      running stack (default $UI_VIS_BASE_URL or http://127.0.0.1:8080)
    --viewports <list>    comma list from: ${Object.keys(VIEWPORTS).join(', ')} (default both)
    --only <list>         capture just these surface names
    --session <id>        session_id cookie value for a pre-provisioned session
    --out <dir>           output root (default e2e/visual-output)
`);
}

function readSessionFile(outRoot) {
  const file = path.join(outRoot, '.session');
  if (!fs.existsSync(file)) return null;
  const value = fs.readFileSync(file, 'utf8').trim();
  return value || null;
}

function parseArgs(argv) {
  const args = {
    label: null,
    baseUrl: null,
    viewports: Object.keys(VIEWPORTS),
    only: null,
    session: null,
    outRoot: process.env.UI_VIS_OUT || DEFAULT_OUT_ROOT,
  };
  for (let i = 0; i < argv.length; i++) {
    const flag = argv[i];
    const value = () => argv[++i];
    if (flag === '--label') args.label = value();
    else if (flag === '--base-url') args.baseUrl = value();
    else if (flag === '--viewports') args.viewports = value().split(',').map((s) => s.trim()).filter(Boolean);
    else if (flag === '--only') args.only = value().split(',').map((s) => s.trim()).filter(Boolean);
    else if (flag === '--session') args.session = value();
    else if (flag === '--out') args.outRoot = value();
    else if (flag === '--help' || flag === '-h') {
      printHelp();
      process.exit(0);
    } else fail(`unknown argument ${flag}`);
  }

  args.baseUrl = (args.baseUrl || process.env.UI_VIS_BASE_URL || 'http://127.0.0.1:8080').replace(/\/+$/, '');
  args.session = args.session || process.env.UI_VIS_SESSION || readSessionFile(args.outRoot);
  if (!args.label) fail('--label <name> is required (e.g. --label before)');
  for (const viewport of args.viewports) {
    if (!VIEWPORTS[viewport]) fail(`unknown viewport "${viewport}" (have: ${Object.keys(VIEWPORTS).join(', ')})`);
  }
  return args;
}

async function assertStack(baseUrl) {
  let response;
  try {
    // Generous on purpose: /api/health probes the downstream services itself,
    // and an unreachable one (slskd down) makes the endpoint take its full
    // client timeout -- a shorter deadline here reads as "no stack" and sends
    // the operator hunting for a stack that is running.
    response = await fetch(`${baseUrl}/api/health`, { signal: AbortSignal.timeout(20_000) });
  } catch {
    fail(
      `no stack answered at ${baseUrl}\n` +
        '  bring one up (docs/DEPLOYMENT.md) or point --base-url / UI_VIS_BASE_URL at a running one.',
    );
  }
  if (!response.ok) fail(`${baseUrl}/api/health answered ${response.status}`);
  const body = await response.json().catch(() => ({}));
  if (body.status !== 'ok') fail(`${baseUrl}/api/health reported status=${body.status}`);
}

// Returns the auth mode actually used. A minted session is preferred when one
// exists, because the seeded content lives with the seeded account and the
// register+login path would land a new account on an empty library.
async function authenticate(context, page, baseUrl, sessionId) {
  if (sessionId) {
    await context.addCookies([{ name: 'session_id', value: sessionId, url: baseUrl }]);
    return 'session';
  }
  const user = {
    email: process.env.UI_VIS_EMAIL || DEFAULT_USER.email,
    password: process.env.UI_VIS_PASSWORD || DEFAULT_USER.password,
  };
  await page.goto(`${baseUrl}/`, { waitUntil: 'domcontentloaded', timeout: NAV_TIMEOUT_MS });
  const csrf = (await context.cookies(baseUrl)).find((c) => c.name === 'csrf_')?.value || '';
  // Register answers 201 whether or not the account already exists (it is
  // deliberately not an account oracle), so this is idempotent by contract.
  const register = await page.request.post(`${baseUrl}/api/auth/register`, {
    data: user,
    headers: { 'X-CSRF-Token': csrf },
  });
  if (!register.ok()) {
    const body = (await register.text().catch(() => '')).slice(0, 200);
    fail(`register ${user.email} -> ${register.status()} ${body}\n  the password must clear DefaultMinPasswordLength (12)`);
  }
  const login = await page.request.post(`${baseUrl}/api/auth/login`, {
    data: user,
    headers: { 'X-CSRF-Token': csrf },
  });
  if (!login.ok()) fail(`login ${user.email} -> ${login.status()}`);
  return 'credentials';
}

async function verifyAuth(page, baseUrl, mode) {
  const response = await page.request.get(`${baseUrl}/api/libraries/`);
  if (response.status() !== 200) {
    fail(`authenticated request failed with ${response.status()} (auth mode: ${mode})`);
  }
}

async function readIdentity(page, baseUrl) {
  try {
    await page.goto(`${baseUrl}/`, { waitUntil: 'domcontentloaded', timeout: NAV_TIMEOUT_MS });
    const el = page.locator('.signed-in-as').first();
    if ((await el.count()) === 0) return null;
    return (await el.textContent())?.trim() || null;
  } catch {
    return null;
  }
}

function gitInfo() {
  const run = (args) => execFileSync('git', args, { cwd: REPO_ROOT, encoding: 'utf8' }).trim();
  try {
    const status = run(['status', '--porcelain']);
    return {
      rev: run(['rev-parse', '--short', 'HEAD']),
      branch: run(['rev-parse', '--abbrev-ref', 'HEAD']),
      dirtyFiles: status ? status.split('\n').filter(Boolean).length : 0,
    };
  } catch {
    return { rev: null, branch: null, dirtyFiles: null };
  }
}

async function settle(page) {
  // Deadlines are deliberately tolerant: a page that never reaches networkidle
  // (the dashboard holds a websocket open) must still be captured, and the
  // settle below is what makes the shot reproducible, not the load state.
  await page.waitForLoadState('load', { timeout: 10_000 }).catch(() => {});
  await page.waitForLoadState('networkidle', { timeout: 8_000 }).catch(() => {});
  await page.evaluate(() => (document.fonts ? document.fonts.ready : Promise.resolve())).catch(() => {});
  await page.waitForTimeout(SETTLE_MS);
}

async function runStep(page, step) {
  if (step.goto) {
    await page.goto(new URL(step.goto, page.url()).href, { waitUntil: 'domcontentloaded', timeout: NAV_TIMEOUT_MS });
  } else if (step.click) {
    await page.locator(step.click).nth(step.nth || 0).click({ timeout: 15_000 });
    await settle(page);
  } else if (step.waitFor || step.waitForSelector) {
    const selector = step.waitFor || step.waitForSelector;
    await page.waitForSelector(selector, { state: 'visible', timeout: 15_000 });
  } else if (step.waitForContent) {
    await page.waitForFunction(
      (selector) => {
        const el = document.querySelector(selector);
        return !!el && el.textContent.trim().length > 0;
      },
      step.waitForContent,
      { timeout: 15_000 },
    );
  } else if (step.waitForLoadState) {
    await page.waitForLoadState(step.waitForLoadState, { timeout: 15_000 }).catch(() => {});
  } else {
    throw new Error(`unknown step ${JSON.stringify(step)}`);
  }
}

async function captureSurface(page, baseUrl, surface, viewportName, viewportSize, outDir, defaultMask) {
  const url = new URL(surface.path || surface.mount || '/', baseUrl).href;
  const response = await page.goto(url, { waitUntil: 'domcontentloaded', timeout: NAV_TIMEOUT_MS });
  const httpStatus = response ? response.status() : null;
  // An error page is not the surface. Recording a 404 as a captured shot would
  // hand a reviewer two error pages that diff at 0% -- a clean bill of health for
  // a page that never rendered. A surface that declares the status it expects
  // (admin answers 403 to a non-admin) passes on that status.
  const expected = surface.expectStatus || null;
  if (httpStatus === null || (httpStatus >= 400 && httpStatus !== expected)) {
    throw new Error(`HTTP ${httpStatus} from ${url}${expected ? ` (expected ${expected})` : ''}`);
  }

  await settle(page);
  if (surface.waitFor) {
    await page.waitForSelector(surface.waitFor, { state: 'attached', timeout: 15_000 }).catch(() => {});
  }
  for (const step of surface.steps || []) await runStep(page, step);
  if (surface.kind === 'partial') {
    // A fragment served alone carries no stylesheet; the app serves one to the
    // pages that host it, so inject the same file rather than capture bare HTML.
    await page.addStyleTag({ url: '/static/css/styles.css' });
  }
  await settle(page);

  const maskSelectors = [...new Set([...defaultMask, ...(surface.mask || [])])];
  const mask = [];
  for (const selector of maskSelectors) {
    const locator = page.locator(selector);
    if ((await locator.count()) > 0) mask.push(locator.first());
  }
  // Hash the markup the shot shows, with the masked regions removed. It tells
  // "the DOM moved" apart from "the pixels moved", which is how a reviewer
  // separates a diff caused by the change under review from one caused by the
  // data behind the surface (the seeded library is not frozen while a UI pass
  // runs). Masked regions are stripped for the same reason they are masked:
  // the live console changes every second for no design reason.
  const domHash = createHash('sha256')
    .update(
      await page.evaluate(
        ({ selectors, volatile }) => {
          const clone = document.documentElement.cloneNode(true);
          for (const selector of [...selectors, ...volatile]) {
            for (const el of clone.querySelectorAll(selector)) el.remove();
          }
          return clone.outerHTML;
        },
        { selectors: maskSelectors, volatile: VOLATILE_IN_DOM },
      ),
    )
    .digest('hex')
    .slice(0, 16);

  const file = path.join(outDir, `${surface.name}.${viewportName}.png`);
  const image = await page.screenshot({
    fullPage: true,
    animations: 'disabled',
    caret: 'hide',
    mask,
    maskColor: MASK_COLOR,
  });
  fs.writeFileSync(file, image);

  return {
    name: surface.name,
    kind: surface.kind,
    requires: surface.requires || null,
    viewport: viewportName,
    viewportSize,
    url,
    httpStatus,
    note: surface.note || null,
    file: path.relative(REPO_ROOT, file).split(path.sep).join('/'),
    bytes: image.length,
    imageHash: createHash('sha256').update(image).digest('hex').slice(0, 16),
    domHash,
  };
}

async function main() {
  const args = parseArgs(process.argv.slice(2));
  const manifest = JSON.parse(fs.readFileSync(path.join(here, 'surfaces.json'), 'utf8'));
  const surfaces = args.only ? manifest.surfaces.filter((s) => args.only.includes(s.name)) : manifest.surfaces;
  if (!surfaces.length) fail('no surfaces selected');

  await assertStack(args.baseUrl);
  const outDir = path.join(args.outRoot, args.label);
  fs.mkdirSync(outDir, { recursive: true });

  browser = await chromium.launch();
  const context = await browser.newContext({
    deviceScaleFactor: 1,
    reducedMotion: 'reduce',
    colorScheme: 'dark',
    locale: 'en-US',
    timezoneId: 'UTC',
  });

  const authPage = await context.newPage();
  const authMode = await authenticate(context, authPage, args.baseUrl, args.session);
  await verifyAuth(authPage, args.baseUrl, authMode);
  const identity = await readIdentity(authPage, args.baseUrl);
  await authPage.close();

  console.log(`\n  batch "${args.label}" -> ${path.relative(REPO_ROOT, outDir)}`);
  console.log(`  stack ${args.baseUrl} | auth ${authMode}${identity ? ` as ${identity}` : ''}`);

  const records = [];
  const failures = [];
  for (const viewportName of args.viewports) {
    const viewportSize = VIEWPORTS[viewportName];
    const page = await context.newPage();
    await page.setViewportSize(viewportSize);
    for (const surface of surfaces) {
      try {
        const record = await captureSurface(page, args.baseUrl, surface, viewportName, viewportSize, outDir, manifest.defaultMask || []);
        records.push(record);
        console.log(`    ok   ${surface.name} (${viewportName}) [${record.httpStatus}]`);
      } catch (error) {
        const message = String(error?.message || error).split('\n')[0];
        failures.push({ name: surface.name, viewport: viewportName, error: message });
        console.log(`    FAIL ${surface.name} (${viewportName}) ${message}`);
      }
    }
    await page.close();
  }

  const batch = {
    label: args.label,
    capturedAt: new Date().toISOString(),
    baseUrl: args.baseUrl,
    authMode,
    identity,
    image: process.env.UI_VIS_IMAGE || null,
    viewports: Object.fromEntries(args.viewports.map((name) => [name, VIEWPORTS[name]])),
    maskColor: MASK_COLOR,
    defaultMask: manifest.defaultMask || [],
    git: gitInfo(),
    surfaceCount: records.length,
    surfaces: records,
    failures,
  };
  fs.writeFileSync(path.join(outDir, 'manifest.json'), `${JSON.stringify(batch, null, 2)}\n`);

  console.log(`\n  ${records.length} captured, ${failures.length} failed\n`);
  if (failures.length) process.exitCode = 1;
}

try {
  await main();
} catch (error) {
  console.error(`\n  error: ${error?.stack || error}\n`);
  process.exitCode = 2;
} finally {
  await browser?.close().catch(() => {});
}
