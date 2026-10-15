import { expect } from '@playwright/test';
import { test } from '../fixtures/auth.fixture';

/**
 * Pressing Pause has to pause (DJI-640, item 2).
 *
 * The card's Pause control carried no values at all, so htmx sent an empty body
 * (Content-Type: application/x-www-form-urlencoded, body ""). PATCH
 * /api/artists/:id therefore parsed `monitored` as nil, skipped the write and
 * re-rendered the same card: pressing Pause left the artist monitored, with the
 * label still reading "Pause" and the API still reporting Monitored: true.
 *
 * Nothing caught it, for two separate reasons. The Go tests DO reach that route
 * (handlers_test.go, artists_auth_test.go) but hand it a body they wrote
 * themselves, so the route was covered and the control was not. And the two
 * specs that touch an artist card call the endpoints instead of clicking -- so
 * this one clicks, and asserts what the click did to the SERVER as well as to
 * the DOM. A swap that repaints the same state is precisely the failure.
 *
 * The names are the e2e MusicBrainz stand-in's own roster entries, so the
 * creates here do not depend on a search resolving anything outside it.
 */
const ARTIST = 'Death';
const OTHER = 'Napalm Death';

test.describe('Pausing and resuming a monitored artist (DJI-640)', () => {
  const created: string[] = [];

  async function getCsrfToken(page: any): Promise<string> {
    const cookies = await page.context().cookies();
    return cookies.find((c: any) => c.name === 'csrf_')?.value || '';
  }

  async function createViaApi(page: any, name: string): Promise<{ id: string; body: any }> {
    const csrf = await getCsrfToken(page);
    const res = await page.request.post('/api/artists', {
      data: { name },
      headers: { 'X-CSRF-Token': csrf },
    });
    expect(
      res.status(),
      `the e2e MusicBrainz stand-in did not answer the create for "${name}": check MUSICBRAINZ_URL is set on ops-web and that fake-musicbrainz is healthy`
    ).toBe(201);
    const body = await res.json();
    created.push(body.ID);
    return { id: body.ID, body };
  }

  /**
   * The row for one artist as `GET /api/artists` reports it, or undefined.
   *
   * Undefined is a real answer here rather than a lookup that missed: that route
   * answers with the MONITORED set (`GetMonitoredArtists` filters
   * `monitored = true`), so a paused artist is absent from it. Absence is only
   * readable as "the pause landed" next to an assertion that the row still
   * exists, which is what the card still being on the page supplies.
   */
  async function rowPerApi(page: any, id: string): Promise<any | undefined> {
    const rows = await (await page.request.get('/api/artists')).json();
    return rows.find((r: any) => r.ID === id);
  }

  // A local run reuses the stack AND its database, so an artist left behind is
  // read as residue by artists.spec.ts's empty-state assertion next run. Cleanup
  // runs even when an assertion above failed, which is the case where a row is
  // most likely to be stranded.
  test.afterEach(async ({ authenticatedPage: page }) => {
    const csrf = await getCsrfToken(page);
    while (created.length) {
      const id = created.pop()!;
      const res = await page.request.delete(`/api/artists/${id}`, {
        headers: { 'X-CSRF-Token': csrf },
      });
      expect([200, 404]).toContain(res.status());
    }
  });

  test('the Pause control sends the state it wants, and the server takes it', async ({
    authenticatedPage: page,
  }) => {
    const { id } = await createViaApi(page, ARTIST);

    await page.goto('/artists');
    const card = page.locator(`#artist-${id}`);
    await expect(card).toBeVisible();
    await expect(card.locator('.badge-disabled'), 'a fresh artist is being monitored').toHaveCount(0);
    await expect(card.locator('button:has-text("Pause")')).toHaveCount(1);

    // Captured rather than inferred: the defect was the SHAPE of this request, so
    // asserting the label alone would pass on a control that sends nothing and a
    // page that repainted from stale state.
    const sent: { method: string; path: string; body: string }[] = [];
    page.on('request', (r) => {
      if (!r.url().includes('/api/artists')) return;
      sent.push({
        method: r.method(),
        path: r.url().replace(/^https?:\/\/[^/]+/, ''),
        body: r.postData() || '',
      });
    });

    await card.locator('button:has-text("Pause")').click();

    const patch = sent.find((r) => r.method === 'PATCH' && r.path === `/api/artists/${id}`);
    expect(patch, 'pressing Pause must issue PATCH /api/artists/:id').toBeTruthy();
    expect(
      patch!.body,
      'an empty body is the defect: the handler parses `monitored` and treats nil as "change nothing", so the write is skipped and the card re-renders unchanged'
    ).toContain('monitored=false');

    // The swap, which is what the operator sees.
    await expect(card.locator('button:has-text("Resume")')).toHaveCount(1);
    await expect(card.locator('.badge-disabled'), 'a paused artist says so').toHaveText('Paused');
    await expect(card.locator('button:has-text("Pause")')).toHaveCount(0);

    // And the state itself, which a swap cannot fake. The card is still rendered
    // -- so the row exists -- while the list of monitored artists no longer
    // contains it, which is the column having moved.
    await expect(card, 'a pause keeps the artist, it stops monitoring it').toBeVisible();
    expect(await rowPerApi(page, id), 'the row must have left the monitored set').toBeUndefined();

    // Then the other direction, from the control the swap just produced.
    await card.locator('button:has-text("Resume")').click();

    const resume = sent.filter((r) => r.method === 'PATCH').pop();
    expect(resume!.body, 'Resume must send the opposite state').toContain('monitored=true');
    await expect(card.locator('button:has-text("Pause")')).toHaveCount(1);
    await expect(card.locator('.badge-disabled')).toHaveCount(0);
    const resumed = await rowPerApi(page, id);
    expect(resumed, 'Resume must put the artist back in the monitored set').toBeTruthy();
    expect(resumed.Monitored, 'and the row itself must read as monitored').toBe(true);
  });

  test('Pause moves only the card it was pressed on', async ({ authenticatedPage: page }) => {
    const { id } = await createViaApi(page, ARTIST);
    const { id: otherId } = await createViaApi(page, OTHER);

    await page.goto('/artists');
    const card = page.locator(`#artist-${id}`);
    const other = page.locator(`#artist-${otherId}`);

    await expect(card.locator('button:has-text("Pause")')).toHaveCount(1);
    await card.locator('button:has-text("Pause")').click();
    await expect(card.locator('button:has-text("Resume")')).toHaveCount(1);

    // The control names its own row and re-renders only its own card. The
    // bystander is what proves it, since a PATCH that named no id would hit
    // whatever the route resolved first.
    await expect(other.locator('button:has-text("Pause")'), 'the other artist is untouched').toHaveCount(1);
    await expect(other.locator('.badge-disabled')).toHaveCount(0);
    expect(await rowPerApi(page, otherId), 'only the pressed row left the monitored set').toBeTruthy();
    expect(await rowPerApi(page, id)).toBeUndefined();
  });
});
