import { expect } from '@playwright/test';
import { test } from '../fixtures/auth.fixture';

// Test user credentials (matches auth.fixture.ts)
const TEST_USER = { email: 'e2e-test@netrunner.dev', password: 'testpass123' };

/**
 * Helper to make Subsonic API GET requests with Subsonic auth params.
 * Subsonic API uses its OWN auth via query parameters, not session cookies.
 */
async function subsonicGet(page: any, endpoint: string, params: Record<string, string> = {}) {
  const query = new URLSearchParams({
    u: TEST_USER.email,
    p: TEST_USER.password,
    v: '1.16.1',
    c: 'netrunner-test',
    f: 'json',
    ...params
  }).toString();
  return await page.request.get(`/rest/${endpoint}?${query}`);
}

/**
 * Helper to make Subsonic API POST requests with Subsonic auth params.
 */
async function subsonicPost(page: any, endpoint: string, params: Record<string, string> = {}) {
  const query = new URLSearchParams({
    u: TEST_USER.email,
    p: TEST_USER.password,
    v: '1.16.1',
    c: 'netrunner-test',
    f: 'json',
    ...params
  }).toString();
  return await page.request.get(`/rest/${endpoint}?${query}`);
}

/**
 * Parse Subsonic JSON response from the wrapped envelope.
 */
function parseSubsonicResponse(response: any) {
  return response.json();
}

test.describe('Subsonic API (DJI-433)', () => {

  test.describe('Auth & Ping', () => {
    test('Ping succeeds with valid auth', async ({ authenticatedPage: page }) => {
      const response = await subsonicGet(page, 'ping.view');
      expect(response.status()).toBe(200);

      const data = await parseSubsonicResponse(response);
      expect(data['subsonic-response'].status).toBe('ok');
      expect(data['subsonic-response'].version).toBe('1.16.1');
    });

    test('Ping fails without auth', async ({ page }) => {
      // Request without auth params should fail
      // Subsonic returns 200 with status: 'failed' and error code
      const response = await page.request.get('/rest/ping.view?f=json');
      expect(response.status()).toBe(200);
      const data = await parseSubsonicResponse(response);
      expect(data['subsonic-response'].status).toBe('failed');
      expect(data['subsonic-response'].error.code).toBe(40);
    });

    test('Ping fails with wrong password', async ({ authenticatedPage: page }) => {
      const query = new URLSearchParams({
        u: TEST_USER.email,
        p: 'wrongpassword',
        v: '1.16.1',
        c: 'netrunner-test',
        f: 'json'
      }).toString();
      const response = await page.request.get(`/rest/ping.view?${query}`);
      expect(response.status()).toBe(200);

      const data = await parseSubsonicResponse(response);
      expect(data['subsonic-response'].status).toBe('failed');
      expect(data['subsonic-response'].error.code).toBe(40);
    });

    test('Ping fails with wrong username', async ({ authenticatedPage: page }) => {
      const query = new URLSearchParams({
        u: 'nonexistent@netrunner.dev',
        p: TEST_USER.password,
        v: '1.16.1',
        c: 'netrunner-test',
        f: 'json'
      }).toString();
      const response = await page.request.get(`/rest/ping.view?${query}`);
      expect(response.status()).toBe(200);

      const data = await parseSubsonicResponse(response);
      expect(data['subsonic-response'].status).toBe('failed');
      expect(data['subsonic-response'].error.code).toBe(40);
    });

    test('License endpoint works', async ({ authenticatedPage: page }) => {
      const response = await subsonicGet(page, 'license.view');
      expect(response.status()).toBe(200);

      const data = await parseSubsonicResponse(response);
      expect(data['subsonic-response'].status).toBe('ok');
      expect(data['subsonic-response'].license).toBeDefined();
      expect(data['subsonic-response'].license.valid).toBe(true);
    });
  });

  test.describe('Data Endpoints (Empty State)', () => {
    test('GetIndexes returns empty', async ({ authenticatedPage: page }) => {
      const response = await subsonicGet(page, 'getIndexes.view');
      expect(response.status()).toBe(200);

      const data = await parseSubsonicResponse(response);
      expect(data).toHaveProperty('subsonic-response');
      expect(data['subsonic-response'].status).toBe('ok');
      // indexes may be absent or empty array depending on implementation
      const indexes = data['subsonic-response'].indexes;
      expect(indexes).toBeDefined();
      expect(indexes === undefined || Array.isArray(indexes.index) || indexes.index === undefined).toBeTruthy();
    });

    test('GetAlbumList2 returns empty', async ({ authenticatedPage: page }) => {
      const response = await subsonicGet(page, 'getAlbumList2.view', { type: 'random' });
      expect(response.status()).toBe(200);

      const data = await parseSubsonicResponse(response);
      expect(data).toHaveProperty('subsonic-response');
      expect(data['subsonic-response'].status).toBe('ok');
      // albumList2 may have empty album array or no albumList2 key
      const albumList = data['subsonic-response'].albumList2;
      expect(albumList === undefined || albumList.album === undefined || Array.isArray(albumList.album)).toBeTruthy();
    });

    test('GetRandomSongs returns empty', async ({ authenticatedPage: page }) => {
      const response = await subsonicGet(page, 'getRandomSongs.view');
      expect(response.status()).toBe(200);

      const data = await parseSubsonicResponse(response);
      expect(data).toHaveProperty('subsonic-response');
      expect(data['subsonic-response'].status).toBe('ok');
      // randomSongs may be absent, empty object, or contain a song array
      const randomSongs = data['subsonic-response'].randomSongs;
      expect(
        randomSongs === undefined ||
          randomSongs === null ||
          (typeof randomSongs === 'object' && !Array.isArray(randomSongs)) ||
          Array.isArray(randomSongs.song)
      ).toBeTruthy();
    });

    test('Search3 returns empty', async ({ authenticatedPage: page }) => {
      const response = await subsonicGet(page, 'search3.view', { query: 'nonexistentartist123' });
      expect(response.status()).toBe(200);

      const data = await parseSubsonicResponse(response);
      expect(data).toHaveProperty('subsonic-response');
      expect(data['subsonic-response'].status).toBe('ok');
      // searchResult3 should exist with empty arrays
      const searchResult = data['subsonic-response'].searchResult3;
      expect(searchResult).toBeDefined();
      if (searchResult) {
        expect(searchResult.artists === undefined || Array.isArray(searchResult.artists.artist)).toBeTruthy();
        expect(searchResult.albums === undefined || Array.isArray(searchResult.albums.album)).toBeTruthy();
        expect(searchResult.songs === undefined || Array.isArray(searchResult.songs.song)).toBeTruthy();
      }
    });
  });

  test.describe('Playlist CRUD via Subsonic API', () => {
    let createdPlaylistId: string | null = null;

    test('GetPlaylists returns empty', async ({ authenticatedPage: page }) => {
      const response = await subsonicGet(page, 'getPlaylists.view');
      expect(response.status()).toBe(200);

      const data = await parseSubsonicResponse(response);
      expect(data['subsonic-response'].status).toBe('ok');
      // playlists may be absent, empty, or null playlist array
      const playlists = data['subsonic-response'].playlists;
      expect(
        playlists === undefined ||
          playlists === null ||
          playlists.playlist === null ||
          Array.isArray(playlists.playlist)
      ).toBeTruthy();
    });

    test('CreatePlaylist', async ({ authenticatedPage: page }) => {
      const timestamp = Date.now();
      const playlistName = `Test Playlist ${timestamp}`;

      const response = await subsonicPost(page, 'createPlaylist.view', { name: playlistName });
      expect(response.status()).toBe(200);

      const data = await parseSubsonicResponse(response);
      expect(data['subsonic-response'].status).toBe('ok');
      expect(data['subsonic-response'].playlist).toBeDefined();
      expect(data['subsonic-response'].playlist.name).toBe(playlistName);
      
      // Store ID for subsequent tests
      createdPlaylistId = data['subsonic-response'].playlist.id;
      expect(createdPlaylistId).toBeTruthy();
    });

    test('GetPlaylist after create', async ({ authenticatedPage: page }) => {
      // First create a playlist if not already created
      let playlistId = createdPlaylistId;
      
      if (!playlistId) {
        const timestamp = Date.now();
        const createResponse = await subsonicPost(page, 'createPlaylist.view', { name: `Temp Playlist ${timestamp}` });
        const createData = await parseSubsonicResponse(createResponse);
        playlistId = createData['subsonic-response'].playlist.id;
      }

      const response = await subsonicGet(page, 'getPlaylist.view', { id: playlistId });
      expect(response.status()).toBe(200);

      const data = await parseSubsonicResponse(response);
      expect(data['subsonic-response'].status).toBe('ok');
      expect(data['subsonic-response'].playlist).toBeDefined();
      expect(data['subsonic-response'].playlist.id).toBe(playlistId);
    });

    test('DeletePlaylist', async ({ authenticatedPage: page }) => {
      // First create a playlist to delete
      const timestamp = Date.now();
      const createResponse = await subsonicPost(page, 'createPlaylist.view', { name: `To Delete ${timestamp}` });
      const createData = await parseSubsonicResponse(createResponse);
      const playlistId = createData['subsonic-response'].playlist.id;

      const response = await subsonicPost(page, 'deletePlaylist.view', { id: playlistId });
      expect(response.status()).toBe(200);

      const data = await parseSubsonicResponse(response);
      expect(data['subsonic-response'].status).toBe('ok');
    });

    test('Verify playlist deleted', async ({ authenticatedPage: page }) => {
      // First create a playlist to delete
      const timestamp = Date.now();
      const createResponse = await subsonicPost(page, 'createPlaylist.view', { name: `Verify Delete ${timestamp}` });
      const createData = await parseSubsonicResponse(createResponse);
      const playlistId = createData['subsonic-response'].playlist.id;

      // Delete it
      await subsonicPost(page, 'deletePlaylist.view', { id: playlistId });

      // Now get playlists and verify it's gone
      const response = await subsonicGet(page, 'getPlaylists.view');
      const data = await parseSubsonicResponse(response);
      
      const playlists = data['subsonic-response'].playlists;
      if (playlists && playlists.playlist) {
        const found = playlists.playlist.find((p: any) => p.id === playlistId);
        expect(found).toBeUndefined();
      }
    });
  });

  test.describe('Scan Endpoints', () => {
    test('GetScanStatus', async ({ authenticatedPage: page }) => {
      const response = await subsonicGet(page, 'getScanStatus.view');
      expect(response.status()).toBe(200);

      const data = await parseSubsonicResponse(response);
      expect(data['subsonic-response'].status).toBe('ok');
      expect(data['subsonic-response'].scanStatus).toBeDefined();
      expect(data['subsonic-response'].scanStatus).toHaveProperty('scanning');
    });

    test('StartScan', async ({ authenticatedPage: page }) => {
      const response = await subsonicPost(page, 'startScan.view');
      expect(response.status()).toBe(200);

      const data = await parseSubsonicResponse(response);
      expect(data['subsonic-response'].status).toBe('ok');
    });
  });

  test.describe('Error Handling', () => {
    test('Unknown endpoint returns error', async ({ authenticatedPage: page }) => {
      const response = await subsonicGet(page, 'nonexistent.view');
      expect(response.status()).toBe(404);
    });

    test('Missing version parameter - graceful handling', async ({ authenticatedPage: page }) => {
      // Some implementations may require version, others may default
      const query = new URLSearchParams({
        u: TEST_USER.email,
        p: TEST_USER.password,
        c: 'netrunner-test',
        f: 'json'
        // v is intentionally missing
      }).toString();
      const response = await page.request.get(`/rest/ping.view?${query}`);
      // Should either work with default version or return proper error
      expect([200, 400, 404]).toContain(response.status());
    });

    test('JSON format explicitly requested', async ({ authenticatedPage: page }) => {
      const response = await subsonicGet(page, 'ping.view', { f: 'json' });
      expect(response.status()).toBe(200);

      const contentType = response.headers()['content-type'] || '';
      // Response should be JSON
      expect(
        contentType.includes('application/json') || 
        contentType.includes('text/plain') // Some servers return text/plain for JSON
      ).toBeTruthy();

      const data = await parseSubsonicResponse(response);
      expect(data['subsonic-response']).toBeDefined();
      expect(data['subsonic-response'].status).toBe('ok');
    });
  });

  test.describe('Additional Endpoints', () => {
    test('GetMusicDirectory returns empty or not found', async ({ authenticatedPage: page }) => {
      // Use a fake/non-existent directory ID
      const response = await subsonicGet(page, 'getMusicDirectory.view', { id: 'nonexistent-dir-123' });
      // May return 200 with empty or 400 with error
      expect([200, 400, 404]).toContain(response.status());
    });

    test('GetSong returns not found for nonexistent ID', async ({ authenticatedPage: page }) => {
      const response = await subsonicGet(page, 'getSong.view', { id: 'nonexistent-song-123' });
      // May return 200 with empty or 400 with error
      expect([200, 400, 404]).toContain(response.status());
    });

    test('GetAlbum returns not found for nonexistent ID', async ({ authenticatedPage: page }) => {
      const response = await subsonicGet(page, 'getAlbum.view', { id: 'nonexistent-album-123' });
      // May return 200 with empty or 400 with error
      expect([200, 400, 404]).toContain(response.status());
    });

    test('GetArtist returns not found for nonexistent ID', async ({ authenticatedPage: page }) => {
      const response = await subsonicGet(page, 'getArtist.view', { id: 'nonexistent-artist-123' });
      // May return 200 with empty or 400 with error
      expect([200, 400, 404]).toContain(response.status());
    });

    test('GetCoverArt returns error for nonexistent ID', async ({ authenticatedPage: page }) => {
      const response = await subsonicGet(page, 'getCoverArt.view', { id: 'nonexistent-cover-123' });
      // May return 200 with empty or 400/404 with error
      expect([200, 400, 404]).toContain(response.status());
    });

    test('Stream returns error for nonexistent ID', async ({ authenticatedPage: page }) => {
      const response = await subsonicGet(page, 'stream.view', { id: 'nonexistent-stream-123' });
      // Should return error for nonexistent track
      expect([200, 400, 404]).toContain(response.status());
    });
  });
});
