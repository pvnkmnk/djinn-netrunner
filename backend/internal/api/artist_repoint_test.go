package api

import (
	"io"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/pvnkmnk/netrunner/backend/internal/api/templates"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/pvnkmnk/netrunner/backend/internal/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// repointFixture wires an ArtistsHandler whose MusicBrainz lookup is a stub, so
// nothing here touches the network. The stub is a field on the real service
// rather than an interface, which is why the tests can set TestGetArtist
// instead of inventing a second seam.
func repointFixture(t *testing.T, db *gorm.DB, owner database.User, get func(string) (*services.MusicBrainzArtist, error)) (*fiber.App, *ArtistsHandler) {
	t.Helper()

	mb := services.NewMusicBrainzService(nil)
	mb.TestGetArtist = get

	at := services.NewArtistTrackingService(db, mb)
	handler := NewArtistsHandler(db, at, mb)

	// Repoint answers with the artists region, which is what the picker's row
	// swaps. Without a views engine the render 500s, so this exercises the real
	// response path rather than a JSON stand-in.
	dir := filepath.Join("..", "..", "..", "ops", "web", "templates")

	app := fiber.New(fiber.Config{Views: templates.NewPongo2(dir, ".html")})
	app.Use(func(c fiber.Ctx) error {
		c.Locals("user", owner)
		return c.Next()
	})
	app.Patch("/api/artists/:id/repoint", handler.Repoint)
	return app, handler
}

func repointUser(t *testing.T, db *gorm.DB, email string, role string) database.User {
	t.Helper()
	u := database.User{Email: email, PasswordHash: "hash", Role: role}
	require.NoError(t, db.Create(&u).Error)
	return u
}

func repointProfile(t *testing.T, db *gorm.DB, owner *database.User) database.QualityProfile {
	t.Helper()
	p := database.QualityProfile{Name: "repoint-profile-" + owner.Email, OwnerUserID: &owner.ID}
	require.NoError(t, db.Create(&p).Error)
	return p
}

func repointGet(id string) func(string) (*services.MusicBrainzArtist, error) {
	return func(got string) (*services.MusicBrainzArtist, error) {
		if got != id {
			return nil, services.ErrArtistNotFound
		}
		return &services.MusicBrainzArtist{
			ID: id, Name: "Napalm Death", SortName: "Napalm Death",
			Country: "United Kingdom", Type: "Group",
		}, nil
	}
}

func postRepoint(t *testing.T, app *fiber.App, artistID, mbid string) int {
	t.Helper()
	req := httptest.NewRequest("PATCH",
		"/api/artists/"+artistID+"/repoint",
		strings.NewReader(`{"musicbrainz_id":"`+mbid+`"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	return resp.StatusCode
}

// A re-point swaps the identity fields and resets the counters, because those
// counters describe the entity being replaced. The monitoring flags are NOT
// reset: the operator chose to monitor this row, not to re-decide the policy.
func TestArtistsHandler_RepointResetsCountersAndKeepsFlags(t *testing.T) {
	db := setupAPITestDB(t)
	owner := repointUser(t, db, "repoint-owner@test.local", "user")
	profile := repointProfile(t, db, &owner)

	lastScan := time.Now().Add(-48 * time.Hour)
	artist := database.MonitoredArtist{
		MusicBrainzID: "old-mbid", Name: "Death",
		SortName: "Death", Country: "Sweden", ArtistType: "Group",
		QualityProfileID: profile.ID, OwnerUserID: &owner.ID,
		Monitored: true, MonitorEPs: false, MonitorSingles: true,
		AcquiredReleases: 12, TotalReleases: 48, LastScanDate: &lastScan,
	}
	require.NoError(t, db.Create(&artist).Error)
	// MonitorEPs carries gorm:"default:true", so Create with false omits the
	// column and the database default wins. Set it explicitly, or the flag
	// this test claims to preserve was never the value it seeded.
	require.NoError(t, db.Model(&database.MonitoredArtist{}).
		Where("id = ?", artist.ID).Update("monitor_e_ps", false).Error)

	app, _ := repointFixture(t, db, owner, repointGet("new-mbid"))
	require.Equal(t, 200, postRepoint(t, app, artist.ID.String(), "new-mbid"))

	var after database.MonitoredArtist
	require.NoError(t, db.First(&after, "id = ?", artist.ID).Error)

	assert.Equal(t, "new-mbid", after.MusicBrainzID)
	assert.Equal(t, "Napalm Death", after.Name)
	assert.Equal(t, "United Kingdom", after.Country)
	assert.Equal(t, "Group", after.ArtistType)

	assert.Zero(t, after.AcquiredReleases, "counters describe the OLD entity")
	assert.Zero(t, after.TotalReleases, "counters describe the OLD entity")
	assert.Nil(t, after.LastScanDate, "the scan timestamp describes the OLD entity")

	assert.False(t, after.MonitorEPs, "monitoring flags survive a re-point")
	assert.True(t, after.MonitorSingles, "monitoring flags survive a re-point")
	assert.True(t, after.Monitored, "monitoring flags survive a re-point")
}

// An admin may pause or delete anyone's monitored row as an operator action.
// Silently re-pointing someone else's row is not that: it rewrites what their
// dashboard claims to be tracking. This is the BOLA shape #285 just fixed on
// acquire, and it deliberately does NOT follow the sibling routes' isAdmin
// pattern.
func TestArtistsHandler_RepointIsOwnerScopedAndNotAdminExempt(t *testing.T) {
	for _, role := range []string{"user", "admin"} {
		t.Run(role+" cannot re-point another user's row", func(t *testing.T) {
			db := setupAPITestDB(t)
			victim := repointUser(t, db, "repoint-victim-"+role+"@test.local", "user")
			attacker := repointUser(t, db, "repoint-attacker-"+role+"@test.local", role)
			profile := repointProfile(t, db, &victim)

			artist := database.MonitoredArtist{
				MusicBrainzID: "victim-mbid", Name: "Victim Artist",
				Country:          "Sweden",
				QualityProfileID: profile.ID, OwnerUserID: &victim.ID,
				Monitored: true, AcquiredReleases: 12, TotalReleases: 48,
			}
			require.NoError(t, db.Create(&artist).Error)

			app, _ := repointFixture(t, db, attacker, repointGet("attacker-mbid"))
			status := postRepoint(t, app, artist.ID.String(), "attacker-mbid")
			assert.Contains(t, []int{403, 404}, status,
				"a %s must not re-point someone else's row", role)

			var after database.MonitoredArtist
			require.NoError(t, db.First(&after, "id = ?", artist.ID).Error)
			assert.Equal(t, "victim-mbid", after.MusicBrainzID, "the row must be untouched")
			assert.Equal(t, "Victim Artist", after.Name)
			assert.Equal(t, "Sweden", after.Country)
			assert.Equal(t, 12, after.AcquiredReleases, "counters must be untouched")
		})
	}
}

// Re-pointing at the entity already being monitored is a no-op. Wiping the
// counters here would lose real discography data for no reason, and re-queueing
// a scan would re-fetch what we already have.
func TestArtistsHandler_RepointToSameEntityPreservesCounters(t *testing.T) {
	db := setupAPITestDB(t)
	owner := repointUser(t, db, "repoint-same@test.local", "user")
	profile := repointProfile(t, db, &owner)

	lastScan := time.Now().Add(-time.Hour)
	artist := database.MonitoredArtist{
		MusicBrainzID: "same-mbid", Name: "Napalm Death",
		QualityProfileID: profile.ID, OwnerUserID: &owner.ID,
		Monitored: true, AcquiredReleases: 12, TotalReleases: 48, LastScanDate: &lastScan,
	}
	require.NoError(t, db.Create(&artist).Error)

	app, _ := repointFixture(t, db, owner, repointGet("same-mbid"))
	require.Equal(t, 200, postRepoint(t, app, artist.ID.String(), "same-mbid"))

	var after database.MonitoredArtist
	require.NoError(t, db.First(&after, "id = ?", artist.ID).Error)
	assert.Equal(t, 12, after.AcquiredReleases, "a no-op re-point must not discard the discography")
	assert.Equal(t, 48, after.TotalReleases)
	require.NotNil(t, after.LastScanDate)
}

// The scan is queued against a scope keyed on the artist, and the worker takes
// an advisory lock on it. Two live jobs for one artist serialise for nothing,
// and an operator pressing two buttons deserves one scan. queueArtistScan
// already answers an active job; re-point must go through it rather than
// queueing a second.
func TestArtistsHandler_RepointAnswersAnAlreadyQueuedScan(t *testing.T) {
	db := setupAPITestDB(t)
	owner := repointUser(t, db, "repoint-scan@test.local", "user")
	profile := repointProfile(t, db, &owner)

	artist := database.MonitoredArtist{
		MusicBrainzID: "scan-mbid", Name: "Scanned",
		QualityProfileID: profile.ID, OwnerUserID: &owner.ID, Monitored: true,
	}
	require.NoError(t, db.Create(&artist).Error)

	existing := database.Job{
		Type: "artist_scan", State: "running",
		ScopeType: "artist", ScopeID: artist.ID.String(),
	}
	require.NoError(t, db.Create(&existing).Error)

	app, _ := repointFixture(t, db, owner, repointGet("scan-mbid-2"))
	require.Equal(t, 200, postRepoint(t, app, artist.ID.String(), "scan-mbid-2"))

	var count int64
	require.NoError(t, db.Model(&database.Job{}).
		Where("scope_type = ? AND scope_id = ?", "artist", artist.ID.String()).
		Count(&count).Error)
	assert.EqualValues(t, 1, count,
		"a scan is already active for this artist; re-point must answer it, not queue a second")
}

// An MBID that no longer resolves must leave the row exactly as it was.
// Storing a blank entity would silently un-monitor a working artist and make
// the card claim provenance it does not have.
func TestArtistsHandler_RepointUnresolvableMBIDLeavesTheRowIntact(t *testing.T) {
	db := setupAPITestDB(t)
	owner := repointUser(t, db, "repoint-gone@test.local", "user")
	profile := repointProfile(t, db, &owner)

	lastScan := time.Now().Add(-time.Hour)
	artist := database.MonitoredArtist{
		MusicBrainzID: "live-mbid", Name: "Live Artist",
		Country: "United Kingdom", ArtistType: "Group",
		QualityProfileID: profile.ID, OwnerUserID: &owner.ID,
		Monitored: true, AcquiredReleases: 12, TotalReleases: 48, LastScanDate: &lastScan,
	}
	require.NoError(t, db.Create(&artist).Error)

	app, _ := repointFixture(t, db, owner, func(string) (*services.MusicBrainzArtist, error) {
		return nil, services.ErrArtistNotFound
	})
	status := postRepoint(t, app, artist.ID.String(), "deleted-mbid")
	assert.Equal(t, 404, status)

	var after database.MonitoredArtist
	require.NoError(t, db.First(&after, "id = ?", artist.ID).Error)
	assert.Equal(t, "live-mbid", after.MusicBrainzID, "the row must still point at the old entity")
	assert.Equal(t, "Live Artist", after.Name)
	assert.Equal(t, "United Kingdom", after.Country)
	assert.Equal(t, "Group", after.ArtistType)
	assert.Equal(t, 12, after.AcquiredReleases)
	assert.Equal(t, 48, after.TotalReleases)
	require.NotNil(t, after.LastScanDate)
}

// The response must be the artists REGION, because that is what the picker's
// row swaps (#artists-region, innerHTML). A lone card replaces the region's
// entire contents -- its .section-header Add button and every other card go with
// it -- and JSON leaves the operator looking at stale provenance.
func TestArtistsHandler_RepointReturnsTheArtistsRegion(t *testing.T) {
	db := setupAPITestDB(t)
	owner := repointUser(t, db, "repoint-card@test.local", "user")
	profile := repointProfile(t, db, &owner)

	artist := database.MonitoredArtist{
		MusicBrainzID: "old-card-mbid", Name: "Old",
		QualityProfileID: profile.ID, OwnerUserID: &owner.ID, Monitored: true,
	}
	require.NoError(t, db.Create(&artist).Error)

	app, _ := repointFixture(t, db, owner, repointGet("new-card-mbid"))

	req := httptest.NewRequest("PATCH", "/api/artists/"+artist.ID.String()+"/repoint",
		strings.NewReader(`{"musicbrainz_id":"new-card-mbid"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)

	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	body := string(raw)

	// The region, not a lone card and not JSON: this is the partial the picker
	// row swaps, and it is the only shape that leaves the list's header and the
	// other cards standing.
	assert.Contains(t, body, `class="artists-region"`,
		"the response must be the artists region, not a lone card: the row swaps #artists-region's innerHTML")
	assert.Contains(t, body, `id="artists-list"`,
		"the region partial must carry the list the cards live in")
	assert.Contains(t, body, "Napalm Death", "the response must carry the updated card")
	assert.Contains(t, body, "United Kingdom",
		"the card must carry the NEW entity's provenance; the field has to be SELECTED, not merely stored")
	assert.Contains(t, body, "Group", "the card must carry the new entity's type")
	assert.Contains(t, body, "new-card-mbid", "the card must carry the new MBID")
	assert.NotContains(t, body, "old-card-mbid", "the card must not still name the old entity")
}

// The picker row's swap target and this handler's response have to agree.
// Nothing else compares them, and when they disagreed the region was replaced by
// one card -- the Add button and every other artist disappeared, with no error
// anywhere. Asserted as a pair, so a change to either side fails HERE.
func TestArtistsHandler_RepointResponseMatchesThePickerSwapTarget(t *testing.T) {
	// What the row declares.
	p := newRepointPickerTestApp(t)
	p.stub.artists = []services.MusicBrainzArtist{
		{ID: "mbid-a", Name: "Napalm Death", Country: "United Kingdom", Type: "Group"},
	}
	p.stub.byID["mbid-a"] = p.stub.artists[0]

	row := bodyOf(t, postForm(t, p.app, "/api/artists/search", map[string]string{
		"name": "death", "repoint_for": "artist-uuid-123",
	}, false))
	assert.Contains(t, row, `hx-target="#artists-region"`,
		"the picker row must address the region")
	assert.Contains(t, row, `hx-swap="innerHTML"`,
		"and replace its contents")

	// What the handler answers.
	db := setupAPITestDB(t)
	owner := repointUser(t, db, "repoint-agree@test.local", "user")
	profile := repointProfile(t, db, &owner)

	artist := database.MonitoredArtist{
		MusicBrainzID: "agree-mbid", Name: "Agree",
		QualityProfileID: profile.ID, OwnerUserID: &owner.ID, Monitored: true,
	}
	require.NoError(t, db.Create(&artist).Error)

	app, _ := repointFixture(t, db, owner, repointGet("agree-mbid-2"))

	req := httptest.NewRequest("PATCH", "/api/artists/"+artist.ID.String()+"/repoint",
		strings.NewReader(`{"musicbrainz_id":"agree-mbid-2"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)

	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	assert.Contains(t, string(raw), `class="artists-region"`,
		"#artists-region's innerHTML must BE the region partial; a lone card would delete the section header and every other card")
	assert.Equal(t, "closeModal", resp.Header.Get("HX-Trigger"),
		"the picker is open over the list it just changed, so the pick must close it")
}
