# Instructions

- Following Playwright test failed.
- Explain why, be concise, respect Playwright best practices.
- Provide a snippet of code with the fix, if possible.

# Test info

- Name: profiles.spec.ts >> Quality Profiles CRUD (DJI-427) >> profile form modal opens
- Location: tests/profiles.spec.ts:56:7

# Error details

```
Error: expect(locator).toBeVisible() failed

Locator:  locator('#modal-container')
Expected: visible
Received: hidden
Timeout:  5000ms

Call log:
  - Expect "toBeVisible" with timeout 5000ms
  - waiting for locator('#modal-container')
    14 × locator resolved to <div class="" id="modal-container">…</div>
       - unexpected value "hidden"

```

```yaml
- link "Skip to content":
  - /url: "#main-content"
- banner:
  - heading "NETRUNNER" [level=1]
  - navigation "Primary":
    - link "Dashboard":
      - /url: /
    - link "Watchlists":
      - /url: /watchlists
    - link "Libraries":
      - /url: /libraries
    - link "Profiles":
      - /url: /profiles
    - link "Schedules":
      - /url: /schedules
    - link "Artists":
      - /url: /artists
    - link "Playlists":
      - /url: /playlists
    - link "Jobs":
      - /url: /jobs
    - link "Admin":
      - /url: /admin
- main:
  - heading "Quality Profiles" [level=2]
  - heading "Quality Profiles" [level=2]
  - button "Add new quality profile": Add Profile
  - text: "Default Default Standard quality profile for music acquisition Lossless: True Formats: FLAC,ALAC,WAV Min Bitrate: 320 Cover Art: source,musicbrainz,discogs"
  - button "Edit profile Default": Edit
  - button "Delete profile Default": Delete
- contentinfo: NetRunner v1.0.0 | Music Acquisition Pipeline
```

# Test source

```ts
  1  | import { expect } from '@playwright/test';
  2  | import { test } from '../fixtures/auth.fixture';
  3  | 
  4  | test.describe('Quality Profiles CRUD (DJI-427)', () => {
  5  |   test('profiles page loads', async ({ authenticatedPage: page }) => {
  6  |     await page.goto('/profiles');
  7  | 
  8  |     // Wait for page to load
  9  |     await expect(page.locator('.page-header')).toBeVisible();
  10 | 
  11 |     // Wait for HTMX to load profiles region
  12 |     await page.waitForTimeout(1000);
  13 | 
  14 |     // Verify profiles region exists
  15 |     const profilesRegion = page.locator('#profiles-region');
  16 |     await expect(profilesRegion).toBeVisible();
  17 | 
  18 |     // Verify "Add Profile" button exists
  19 |     const addButton = page.locator('button:has-text("Add Profile")');
  20 |     await expect(addButton).toBeVisible();
  21 |   });
  22 | 
  23 |   test('empty state shows when no profiles', async ({ authenticatedPage: page }) => {
  24 |     await page.goto('/profiles');
  25 |     await page.waitForTimeout(1000);
  26 | 
  27 |     // Check for empty state (may or may not be present depending on existing data)
  28 |     const emptyState = page.locator('.empty-state, text=No profiles');
  29 |     const isEmpty = await emptyState.isVisible().catch(() => false);
  30 | 
  31 |     // Either empty state or profile cards should be visible
  32 |     if (isEmpty) {
  33 |       await expect(emptyState.first()).toBeVisible();
  34 |     } else {
  35 |       const profileCards = page.locator('.profile-card');
  36 |       const count = await profileCards.count();
  37 |       expect(count).toBeGreaterThanOrEqual(0);
  38 |     }
  39 |   });
  40 | 
  41 |   test('navigation to profiles page works', async ({ authenticatedPage: page }) => {
  42 |     // Start from dashboard
  43 |     await page.goto('/');
  44 |     await expect(page.locator('.dashboard')).toBeVisible();
  45 | 
  46 |     // Click Profiles nav link
  47 |     await page.locator('nav#primary-nav a:has-text("Profiles")').click();
  48 | 
  49 |     // Wait for navigation
  50 |     await page.waitForTimeout(1000);
  51 | 
  52 |     // Verify we're on profiles page
  53 |     await expect(page.locator('.page-header h2')).toHaveText('Profiles');
  54 |   });
  55 | 
  56 |   test('profile form modal opens', async ({ authenticatedPage: page }) => {
  57 |     await page.goto('/profiles');
  58 |     await page.waitForTimeout(1000);
  59 | 
  60 |     // Click Add Profile button
  61 |     await page.locator('button:has-text("Add Profile")').click();
  62 | 
  63 |     // Wait for modal to appear
  64 |     await page.waitForTimeout(500);
  65 | 
  66 |     // Verify modal is visible
  67 |     const modal = page.locator('#modal-container');
> 68 |     await expect(modal).toBeVisible();
     |                         ^ Error: expect(locator).toBeVisible() failed
  69 | 
  70 |     // Verify form fields exist
  71 |     const nameInput = page.locator('#profile-name, input[name="name"]');
  72 |     await expect(nameInput.first()).toBeVisible();
  73 |   });
  74 | 
  75 |   test('jobs page accessible from profiles', async ({ authenticatedPage: page }) => {
  76 |     await page.goto('/profiles');
  77 |     await page.waitForTimeout(1000);
  78 | 
  79 |     // Navigate to jobs page
  80 |     await page.locator('nav#primary-nav a:has-text("Jobs")').click();
  81 |     await page.waitForTimeout(1000);
  82 | 
  83 |     // Verify jobs page loads
  84 |     await expect(page.locator('.page-header h2')).toHaveText('Jobs');
  85 |   });
  86 | });
  87 | 
```