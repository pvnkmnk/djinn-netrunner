package testapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// The seed endpoint is the seam every worker-exercising e2e probe drives
// (egress-refusal.spec.ts, ga-probes.spec.ts, ops/fake-slskd's roster) and the
// cleanup endpoint is what lets those probes rerun. These tests pin that
// contract so a change here fails a Go test instead of five browser specs.

type fixture struct {
	app  *fiber.App
	db   *gorm.DB
	cfg  *config.Config
	user database.User
}

func setup(t *testing.T, opts ...func(*fixture)) fixture {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.Migrate(db))
	cfg := &config.Config{
		E2EEnableTestAPI: true,
		MusicLibraryPath: t.TempDir(),
	}
	f := fixture{db: db, cfg: cfg}
	for _, opt := range opts {
		opt(&f)
	}
	// One user per fixture, created once: the middleware runs per request, and
	// a per-request INSERT hits the UNIQUE email constraint on request two.
	f.user = database.User{Email: "probe@test.com", Role: "admin"}
	require.NoError(t, db.Create(&f.user).Error)

	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user", f.user)
		return c.Next()
	})
	Mount(app.Group("/api"), f.cfg, f.db)
	f.app = app
	return f
}

// TestTestAPI_RouteContract pins the exact registered surface: nothing may be
// added or renamed without a deliberate change here, because e2e specs and the
// fake slskd's roster agree with these paths by name.
func TestTestAPI_RouteContract(t *testing.T) {
	f := setup(t)
	var routes []string
	for _, r := range f.app.GetRoutes() {
		if strings.HasPrefix(r.Path, "/api/test") {
			routes = append(routes, r.Method+" "+r.Path)
		}
	}
	sort.Strings(routes)
	assert.Equal(t, []string{
		"POST /api/test/create-dir",
		"POST /api/test/seed-fallback-refusal",
		"POST /api/test/seed-fallback-refusal/cleanup",
	}, routes)
}

// TestTestAPI_GateClosedWithoutEnv: the 403 must come from the gate itself,
// not from missing auth — an authenticated session without the env var still
// gets nothing.
func TestTestAPI_GateClosedWithoutEnv(t *testing.T) {
	for _, path := range []string{"/api/test/create-dir", "/api/test/seed-fallback-refusal", "/api/test/seed-fallback-refusal/cleanup"} {
		t.Run(path, func(t *testing.T) {
			f := setup(t, func(x *fixture) { x.cfg.E2EEnableTestAPI = false })
			req := httptest.NewRequest("POST", path, strings.NewReader(`{"artist":"x"}`))
			req.Header.Set("Content-Type", "application/json")
			resp, err := f.app.Test(req)
			require.NoError(t, err)
			assert.Equal(t, 403, resp.StatusCode)
		})
	}
}

// TestTestAPI_RequiresAuth: gate open but no user in Locals — each endpoint
// 401s rather than acting anonymously.
func TestTestAPI_RequiresAuth(t *testing.T) {
	f := setup(t)
	app := fiber.New() // no user-setting middleware on purpose
	Mount(app.Group("/api"), f.cfg, f.db)
	for _, path := range []string{"/api/test/create-dir", "/api/test/seed-fallback-refusal", "/api/test/seed-fallback-refusal/cleanup"} {
		req := httptest.NewRequest("POST", path, strings.NewReader(`{"artist":"x"}`))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, 401, resp.StatusCode, path)
	}
}

func seed(t *testing.T, f fixture, body string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest("POST", "/api/test/seed-fallback-refusal", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := f.app.Test(req)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)
	var out map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	jobID, ok := out["job_id"].(float64)
	require.True(t, ok, "job_id must be numeric")
	return int(jobID), out
}

func TestTestAPI_SeedScopeContract(t *testing.T) {
	f := setup(t)

	id1, _ := seed(t, f, `{"artist":"Probe Artist","album":"Probe Album","no_fallback":true}`)
	id2, _ := seed(t, f, `{"artist":"Probe Artist","album":"Probe Album","no_fallback":true}`)

	var job database.Job
	require.NoError(t, f.db.First(&job, id1).Error)
	assert.Equal(t, "acquisition", job.Type)
	assert.Equal(t, "queued", job.State)
	// Unique scope per seed — the advisory-lock contract. Two identical seeds
	// must never contend on one key.
	assert.Equal(t, "probe", job.ScopeType)
	assert.NotEqual(t, job.ScopeID, func() string {
		var j2 database.Job
		require.NoError(t, f.db.First(&j2, id2).Error)
		return j2.ScopeID
	}())
	assert.True(t, strings.HasPrefix(job.ScopeID, "fallback-refusal-"), job.ScopeID)

	var items []database.JobItem
	require.NoError(t, f.db.Where("job_id = ?", id1).Find(&items).Error)
	require.Len(t, items, 1, "exactly one item, attached to the job (not orphaned)")
	item := items[0]
	assert.Equal(t, "Probe Artist", item.Artist)
	assert.Equal(t, "Probe Album", item.Album)
	assert.Equal(t, "Probe Artist Probe Album", item.NormalizedQuery)
	assert.Empty(t, item.SourceURL, "no_fallback must produce an empty source_url")
	assert.Equal(t, 3, job.MaxAttempts, "default retry budget")
}

// TestTestAPI_SeedSourceURLPrecedence pins the three source_url cases: the
// Soulseek entrance (no_fallback), an explicit probe URL, and the DJI-501
// default (public, non-allowlisted httpbin).
func TestTestAPI_SeedSourceURLPrecedence(t *testing.T) {
	f := setup(t)

	id, _ := seed(t, f, `{"artist":"A","url":"http://example.test/x.flac"}`)
	var withURL database.JobItem
	require.NoError(t, f.db.Where("job_id = ?", id).First(&withURL).Error)
	assert.Equal(t, "http://example.test/x.flac", withURL.SourceURL)

	// no_fallback wins over an explicit URL: the Soulseek entrance means NO
	// source_url at all.
	id, _ = seed(t, f, `{"artist":"A","url":"http://example.test/x.flac","no_fallback":true}`)
	var noFallback database.JobItem
	require.NoError(t, f.db.Where("job_id = ?", id).First(&noFallback).Error)
	assert.Empty(t, noFallback.SourceURL)

	id, _ = seed(t, f, `{"artist":"A"}`)
	var byDefault database.JobItem
	require.NoError(t, f.db.Where("job_id = ?", id).First(&byDefault).Error)
	assert.Equal(t, "https://httpbin.org/bytes/1024", byDefault.SourceURL)
}

// TestTestAPI_SeedValidation: bad payloads 400; max_attempts clamps into
// 1..10 instead of poisoning the job.
func TestTestAPI_SeedValidation(t *testing.T) {
	f := setup(t)

	post := func(body string) int {
		req := httptest.NewRequest("POST", "/api/test/seed-fallback-refusal", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := f.app.Test(req)
		require.NoError(t, err)
		return resp.StatusCode
	}

	assert.Equal(t, 400, post(`{}`), "artist required")
	assert.Equal(t, 400, post(`not json`))

	// Fresh variables per lookup: GORM carries conditions from a populated
	// struct into the next query (First(&job, 2) on a job{ID:1} searches
	// "id = 2 AND id = 1" and finds nothing).
	_, out := seed(t, f, `{"artist":"A","max_attempts":1}`)
	var jobA database.Job
	require.NoError(t, f.db.First(&jobA, int(out["job_id"].(float64))).Error)
	assert.Equal(t, 1, jobA.MaxAttempts)

	// Out of range → clamped back to the default, never 0 or 99.
	_, out = seed(t, f, `{"artist":"A","max_attempts":99}`)
	var jobB database.Job
	require.NoError(t, f.db.First(&jobB, int(out["job_id"].(float64))).Error)
	assert.Equal(t, 3, jobB.MaxAttempts)
}

// TestTestAPI_SeedPeerForwarding pins the peer-roster contract: a spec rides
// the seed payload; with no fake slskd reachable the seed still succeeds (the
// roster is best-effort), and the marker must be a lowercase substring of the
// item's normalized query for the fake to ever pick the peer.
func TestTestAPI_SeedPeerForwarding(t *testing.T) {
	f := setup(t)

	body := `{"artist":"Wrong Work Probe","album":"Unrelated Record","no_fallback":true,` +
		`"peer":{"username":"on-demand-peer","filename":"CD01/08 - Somebody Else's Song.flac",` +
		`"tag_artist":"On Demand Band","tag_album":"Their LP","tag_title":"Song","marker":"unrelated record"}}`
	id, _ := seed(t, f, body)

	var item database.JobItem
	require.NoError(t, f.db.Where("job_id = ?", id).First(&item).Error)
	// The marker drives the fake's query matching — a spec sets it to a
	// substring of its own request (artist+album), so the fake picks the peer
	// for THIS item and not for another's.
	assert.Contains(t, strings.ToLower(item.NormalizedQuery), "unrelated record")
}

// TestTestAPI_SeedPeerLocalDerivation: Local omitted is accepted (the server
// derives the staging basename); forwarding must not fail the seed when no
// fake is listening on the compose network.
func TestTestAPI_SeedPeerLocalDerivation(t *testing.T) {
	f := setup(t)

	id, _ := seed(t, f, `{"artist":"P","album":"A","no_fallback":true,` +
		`"peer":{"username":"u","filename":"CD01/08 - Song.flac","tag_artist":"P","tag_album":"A","tag_title":"S","marker":"song"}}`)
	assert.NotZero(t, id, "seed with peer must succeed without a fake slskd on the stack")
}

// TestTestAPI_CleanupCoversDeclaredSeeds pins DJI-502's contract: a seed's own
// names (request artist + peer TAG artist) must reach the cleanup roster, so a
// new acceptance clause's residue is removed without editing any fixture list.
// The decoy scenario is the one that hurts: a peer tagged "On Demand Band"
// sails through the gate (a matching work), imports, and its residue would
// short-circuit the next probe via the recording-dedup path if cleanup never
// learned the name.
func TestTestAPI_CleanupCoversDeclaredSeeds(t *testing.T) {
	f := setup(t)
	body := `{"artist":"On Demand Probe","album":"Fresh Clause","no_fallback":true,` +
		`"peer":{"username":"on-demand","filename":"CD/01 - Song.flac",` +
		`"tag_artist":"On Demand Band","tag_album":"Their LP","tag_title":"Song","marker":"fresh clause"}}`
	seed(t, f, body)
	roster := cleanupRoster()
	assert.Contains(t, roster, "On Demand Probe", "the request artist must be declared")
	assert.Contains(t, roster, "On Demand Band", "the peer's TAG artist must be declared (import lands under it)")
	// Baseline roster still present.
	assert.Contains(t, roster, "Clean Success Artist")
	// The full cleanup removes the declared names' rows and folders too.
	lib := database.Library{Name: "D Lib", Path: filepath.Join(f.cfg.MusicLibraryPath, "d-lib")}
	require.NoError(t, f.db.Create(&lib).Error)
	require.NoError(t, f.db.Create(&database.Track{LibraryID: lib.ID, Artist: "On Demand Band", Album: "x", Title: "t", Path: filepath.Join(f.cfg.MusicLibraryPath, "On Demand Band", "a.flac")}).Error)
	require.NoError(t, os.MkdirAll(filepath.Join(f.cfg.MusicLibraryPath, "On Demand Band"), 0o755))
	req := httptest.NewRequest("POST", "/api/test/seed-fallback-refusal/cleanup", nil)
	resp, err := f.app.Test(req)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)
	var n int64
	require.NoError(t, f.db.Model(&database.Track{}).Where("artist = ?", "On Demand Band").Count(&n).Error)
	assert.Zero(t, n, "the declared peer artist's rows must be cleaned")
	_, statErr := os.Stat(filepath.Join(f.cfg.MusicLibraryPath, "On Demand Band"))
	assert.True(t, os.IsNotExist(statErr), "the declared peer artist's folder must be removed")
}

// TestTestAPI_CleanupRosterAdditive: roster entries are only ever added — a
// cleanup run must not trim the map, or a name declared between a spec's runs
// would be forgotten and the residue hazard would return.
func TestTestAPI_CleanupRosterAdditive(t *testing.T) {
	f := setup(t)
	seed(t, f, `{"artist":"Additive Probe","album":"A","no_fallback":true}`)
	before := len(cleanupRoster())
	req := httptest.NewRequest("POST", "/api/test/seed-fallback-refusal/cleanup", nil)
	resp, err := f.app.Test(req)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, before, len(cleanupRoster()), "cleanup must not shrink the roster")
	// A second seed with the SAME names is idempotent (no duplicate entries):
	// the name is already in the set, so the roster stays the same size.
	seed(t, f, `{"artist":"Additive Probe","album":"A","no_fallback":true}`)
	assert.Equal(t, before, len(cleanupRoster()), "identical re-seed must not grow the roster")
}

// TestTestAPI_CleanupRemovesFixtures: rows AND the on-disk folders go, other
// artists' data survives, and a failed removal is reported loudly.
func TestTestAPI_CleanupRemovesFixtures(t *testing.T) {
	f := setup(t)

	fixtureArtists := []string{"Totally Different Band", "Wrong Work Probe", "Clean Success Artist"}
	lib := database.Library{Name: "Probe Lib", Path: filepath.Join(f.cfg.MusicLibraryPath, "probe-lib")}
	require.NoError(t, f.db.Create(&lib).Error)
	for _, artist := range fixtureArtists {
		require.NoError(t, f.db.Create(&database.Track{LibraryID: lib.ID, Artist: artist, Album: "x", Title: "t", Path: filepath.Join(f.cfg.MusicLibraryPath, artist, "a.flac")}).Error)
		require.NoError(t, f.db.Create(&database.Acquisition{JobID: 1, JobItemID: 1, Artist: artist, Album: "x", TrackTitle: "t", OriginalPath: "o", FinalPath: "f"}).Error)
		dir := filepath.Join(f.cfg.MusicLibraryPath, artist)
		require.NoError(t, os.MkdirAll(dir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "a.flac"), []byte("x"), 0o644))
	}
	// A bystander that cleanup must leave alone.
	require.NoError(t, f.db.Create(&database.Track{LibraryID: lib.ID, Artist: "Real Library Artist", Album: "x", Title: "t", Path: filepath.Join(f.cfg.MusicLibraryPath, "Real Library Artist", "a.flac")}).Error)

	req := httptest.NewRequest("POST", "/api/test/seed-fallback-refusal/cleanup", nil)
	resp, err := f.app.Test(req)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)

	for _, artist := range fixtureArtists {
		var n int64
		require.NoError(t, f.db.Model(&database.Track{}).Where("artist = ?", artist).Count(&n).Error)
		assert.Zero(t, n, "track rows for %s", artist)
		require.NoError(t, f.db.Model(&database.Acquisition{}).Where("artist = ?", artist).Count(&n).Error)
		assert.Zero(t, n, "acquisition rows for %s", artist)
		_, statErr := os.Stat(filepath.Join(f.cfg.MusicLibraryPath, artist))
		assert.True(t, os.IsNotExist(statErr), "folder for %s must be gone", artist)
	}
	var n int64
	require.NoError(t, f.db.Model(&database.Track{}).Where("artist = ?", "Real Library Artist").Count(&n).Error)
	assert.Equal(t, int64(1), n, "bystander data must survive cleanup")
}

// TestTestAPI_CreateDir: library paths materialize under the allowed prefixes
// only; traversal is refused before MkdirAll runs.
func TestTestAPI_CreateDir(t *testing.T) {
	f := setup(t)

	post := func(body string) (*http.Response, string) {
		req := httptest.NewRequest("POST", "/api/test/create-dir", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := f.app.Test(req)
		require.NoError(t, err)
		raw, _ := io.ReadAll(resp.Body)
		return resp, string(raw)
	}

	resp, body := post(fmt.Sprintf(`{"path":%q}`, filepath.Join(f.cfg.MusicLibraryPath, "New Lib")))
	assert.Equal(t, 200, resp.StatusCode, body)
	_, err := os.Stat(filepath.Join(f.cfg.MusicLibraryPath, "New Lib"))
	require.NoError(t, err)

	// Second allowed prefix. The production list is "/tmp/" (POSIX deployments)
	// plus the library root; on a Windows dev host /tmp is MSYS-virtual, so the
	// test exercises the second prefix through a configured temp root instead.
	second := t.TempDir()
	f.cfg.MusicLibraryPath = second
	app2 := fiber.New()
	app2.Use(func(c *fiber.Ctx) error { c.Locals("user", f.user); return c.Next() })
	Mount(app2.Group("/api"), f.cfg, f.db)
	req := httptest.NewRequest("POST", "/api/test/create-dir", strings.NewReader(`{"path":"`+strings.ReplaceAll(filepath.ToSlash(filepath.Join(second, "Sub", "Dir")), `"`, `\"`)+`"}`))
	req.Header.Set("Content-Type", "application/json")
	resp2, err := app2.Test(req)
	require.NoError(t, err)
	assert.Equal(t, 200, resp2.StatusCode)
	_, err = os.Stat(filepath.Join(second, "Sub", "Dir"))
	require.NoError(t, err, "nested creation under a configured prefix must work")

	// Traversal that survives filepath.Clean (it stays ".."-prefixed) must be
	// refused; an empty path is refused too.
	resp, _ = post(`{"path":"/tmp/../outside"}`)
	assert.Equal(t, 400, resp.StatusCode, "traversal refused")
	resp, _ = post(`{"path":""}`)
	assert.Equal(t, 400, resp.StatusCode, "empty path refused")
}
