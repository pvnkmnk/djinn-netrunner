package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/pvnkmnk/netrunner/backend/internal/api/templates"
	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/pvnkmnk/netrunner/backend/internal/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/html"
	"gorm.io/gorm"
)

// A partial owns controls; handlers own answers. This contract walks every
// htmx-bearing element in the rendered Playlists, Jobs, Libraries and Schedules
// list regions, issues its declared request against the production handler, and
// derives the required target/swap from the response that handler actually
// returns. Values come from hx-vals or the selected controls hx-include names --
// never from a request body the test invented.
//
// This is deliberately wider than four hand-maintained button tables. A new
// action rendered in one of these regions is automatically issued; if its
// response shape is new, the classifier fails loudly rather than leaving the
// target unchecked. Post-request checks cover values applied to rows and jobs,
// not just strings that appeared in a response.
func TestListRegionControls_MatchTheirHandlersAndAppliedValues(t *testing.T) {
	t.Run("playlists", testPlaylistRegionControls)
	t.Run("jobs", testJobsRegionControls)
	t.Run("libraries", testLibraryRegionControls)
	t.Run("schedules", testScheduleRegionControls)
	t.Run("watchlists", testWatchlistRegionControls)
}

// The Watchlists region joined this contract late, and it had already paid for
// the gap. Delete carried hx-target="#watchlist-<id>" -- its own card -- while
// DeleteWatchlist answers with the whole region. Swapping a region into a card's
// slot nests a second one inside the first: one delete rendered a duplicate
// region, a duplicate Add button and every remaining card twice, inside a page
// whose ids were duplicated with it. No test noticed, because the specs asserted
// the deleted card was gone, and it was -- twice over.
//
// The walker derives each control's required target and swap from the response
// the handler actually returns, which is why adding this region is what caught
// it. The preview is covered the same way, end to end: the control is issued
// against the production handler, and the body asserted here is the provider's
// tracks as the card would show them.
func testWatchlistRegionControls(t *testing.T) {
	app, db, _, watchlist, _, _, _ := listRegionTestApp(t)

	region, _ := issueRenderedControl(t, app, renderedHTMXControl{method: "GET", path: "/partials/watchlists"})
	controls := renderedControls(t, region)
	assertRenderedControlSet(t, controls,
		"Add new watchlist",
		"Sync watchlist "+watchlist.Name,
		"Preview tracks for "+watchlist.Name,
		"Edit watchlist "+watchlist.Name,
		"Delete watchlist "+watchlist.Name,
		"Disable monitoring for "+watchlist.Name,
	)

	// Delete is issued last: it removes the row every other control in the card
	// names, so a preview issued after it would answer 404 and the walker would
	// report a missing handler instead of the contract it is here to check.
	sort.SliceStable(controls, func(i, j int) bool {
		if controls[i].method == "DELETE" {
			return false
		}
		return controls[j].method == "DELETE"
	})

	for _, ctl := range controls {
		body, requestURL := issueRenderedControl(t, app, ctl)
		assertControlResponseTarget(t, ctl, body)
		switch {
		case strings.HasSuffix(requestURL, "/preview"):
			resourceID := strings.TrimSuffix(strings.TrimPrefix(requestURL, "/api/watchlists/"), "/preview")
			assert.Equal(t, watchlist.ID.String(), resourceID, "the preview URL must carry the watchlist ID its card rendered")
			assert.Equal(t, "watchlist-"+resourceID, ctl.cardID, "the preview URL ID must belong to the card that rendered this control")
			// The round trip, not the button: what lands in the region is the
			// provider's tracks, capped, with a count of what is left out.
			assert.Contains(t, body, fmt.Sprintf("%d of %d tracks", previewLimit, previewLimit+2),
				"the preview must count every track the source returned")
			assert.Contains(t, body, "Track 01", "the preview must render the tracks themselves")
			assert.NotContains(t, body, fmt.Sprintf("Track %02d", previewLimit+1),
				"the card preview must stop at the cap, or a large watchlist renders in full")
			assert.NotContains(t, body, fmt.Sprintf("Track %02d", previewLimit+2))
			assert.Contains(t, body, fmt.Sprintf("+ %d more tracks", 2), "and it must say how many it left out")
		case strings.HasSuffix(requestURL, "/sync"):
			assert.Contains(t, body, "Sync triggered for watchlist "+watchlist.Name,
				"the Sync control reports the watchlist its own card rendered")
			var count int64
			require.NoError(t, db.Model(&database.Job{}).
				Where("job_type = ? AND scope_type = ? AND scope_id = ?", "sync", "watchlist", watchlist.ID.String()).
				Count(&count).Error)
			assert.EqualValues(t, 1, count, "the Sync control must queue the sync job for its rendered watchlist ID")
		case strings.HasSuffix(requestURL, "/toggle"):
			resourceID := strings.TrimSuffix(strings.TrimPrefix(requestURL, "/api/watchlists/"), "/toggle")
			assert.Equal(t, "watchlist-"+resourceID, ctl.cardID, "the toggle URL ID must belong to the card that rendered this control")
			var after database.Watchlist
			require.NoError(t, db.First(&after, "id = ?", resourceID).Error)
			assert.False(t, after.Enabled, "toggle applies to the watchlist ID in the rendered control")
			assert.Contains(t, body, "Enable monitoring", "the response card reflects the state the toggle applied")
		case strings.HasSuffix(requestURL, "/form"):
			assert.Contains(t, body, "modal-overlay")
			if strings.Contains(requestURL, "?id=") {
				parsed, err := url.Parse(requestURL)
				require.NoError(t, err)
				editID := parsed.Query().Get("id")
				assert.Equal(t, "watchlist-"+editID, ctl.cardID, "the edit query ID must belong to the card that rendered this control")
				assert.Contains(t, body, `value="`+editID+`"`, "the edit form must carry the watchlist ID from the control query")
			}
		case ctl.method == "DELETE":
			resourceID := strings.TrimPrefix(requestURL, "/api/watchlists/")
			assert.Equal(t, "watchlist-"+resourceID, ctl.cardID, "the delete URL ID must belong to the card that rendered this control")
			assert.Equal(t, watchlist.ID.String(), resourceID, "the delete URL must carry the watchlist ID rendered on the card")
			var count int64
			require.NoError(t, db.Model(&database.Watchlist{}).Where("id = ?", resourceID).Count(&count).Error)
			assert.Zero(t, count, "the delete control removes the watchlist its URL names")
			// The response IS the region, so the target check above is what keeps
			// it out of a card's slot; this is the other half of the same promise:
			// the card it deleted must not come back inside that region.
			assert.NotContains(t, body, `id="watchlist-`+resourceID+`"`, "the region must not re-render the card it deleted")
		}
	}
}

type renderedHTMXControl struct {
	method   string
	path     string
	target   string
	swap     string
	label    string
	cardID   string
	values   map[string]string
	params   url.Values
	include  string
	isSelect bool
}

func listRegionTestApp(t *testing.T) (*fiber.App, *gorm.DB, database.User, database.Watchlist, database.Library, database.Playlist, database.Schedule) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.Migrate(db))

	user := database.User{Email: "region-contract@example.test", PasswordHash: "hashed", Role: "admin"}
	require.NoError(t, db.Create(&user).Error)
	profile := database.QualityProfile{Name: "Region Contract", IsDefault: true}
	require.NoError(t, db.Create(&profile).Error)
	// The watchlist fixture reads a real file rather than a network source.
	// local_file is the provider the production service registers, so the preview
	// control below is answered by the production path with a source that needs
	// nobody's availability. Two tracks over the preview cap make the cap and its
	// "+ N more" line observable rather than assumed.
	watchlistFile := filepath.Join(t.TempDir(), "region-contract.m3u")
	var playlistBody strings.Builder
	for i := 1; i <= previewLimit+2; i++ {
		fmt.Fprintf(&playlistBody, "Contract Artist - Track %02d\n", i)
	}
	require.NoError(t, os.WriteFile(watchlistFile, []byte(playlistBody.String()), 0o644))

	watchlist := database.Watchlist{
		ID: uuid.New(), Name: "Contract Watchlist", SourceType: "local_file",
		SourceURI: watchlistFile, QualityProfileID: profile.ID,
		OwnerUserID: &user.ID,
	}
	require.NoError(t, db.Create(&watchlist).Error)

	playlist := database.Playlist{ID: uuid.New(), Name: "Delete Me", OwnerUserID: &user.ID}
	require.NoError(t, db.Create(&playlist).Error)
	library := database.Library{ID: uuid.New(), Name: "Contract Library", Path: filepath.Clean(t.TempDir()), OwnerUserID: &user.ID}
	require.NoError(t, db.Create(&library).Error)
	schedule := database.Schedule{WatchlistID: watchlist.ID, CronExpr: "0 0 * * *", Timezone: "UTC", Enabled: true}
	require.NoError(t, db.Create(&schedule).Error)

	engine := templates.NewPongo2(filepath.Join("..", "..", "..", "ops", "web", "templates"), ".html")
	require.NoError(t, engine.LoadFromDir())
	app := fiber.New(fiber.Config{Views: engine})
	app.Use(func(c fiber.Ctx) error { c.Locals("user", user); return c.Next() })

	playlists := NewPlaylistHandler(db)
	app.Get("/partials/playlists", playlists.RenderPlaylistsPartial)
	app.Post("/api/playlists", playlists.Create)
	app.Delete("/api/playlists/:id", playlists.Delete)

	jobs := NewJobHandler(db)
	stats := NewStatsHandler(db)
	app.Get("/partials/jobs", stats.RenderJobsPartial)
	app.Get("/partials/job-logs", stats.RenderJobLogsPartial)
	app.Post("/api/jobs/:id/cancel", jobs.Cancel)
	app.Post("/api/jobs/:id/retry", jobs.Retry)

	libraries := NewLibraryHandler(db)
	app.Get("/partials/libraries", libraries.RenderLibrariesPartial)
	app.Get("/partials/libraries/:id/browse", libraries.BrowseTracks)
	app.Get("/api/libraries/form", libraries.GetForm)
	app.Post("/api/libraries", libraries.CreateLibrary)
	app.Post("/api/libraries/:id/adopt", libraries.AdoptLibrary)
	app.Post("/api/libraries/:id/scan", libraries.TriggerScan)
	app.Post("/api/libraries/:id/enrich", libraries.TriggerEnrich)
	app.Delete("/api/libraries/:id", libraries.DeleteLibrary)

	schedules := NewSchedulesHandler(db)
	app.Get("/partials/schedules", schedules.RenderSchedulesPartial)
	app.Get("/api/schedules/form", schedules.GetForm)
	app.Post("/api/schedules", schedules.Create)
	app.Patch("/api/schedules/:id/toggle", schedules.Toggle)
	app.Delete("/api/schedules/:id", schedules.Delete)

	watchlistService := services.NewWatchlistService(db, NewSpotifyAuthHandler(db), &config.Config{})
	watchlists := NewWatchlistHandler(db, watchlistService)
	preview := NewWatchlistPreviewHandler(db, watchlistService)
	app.Get("/partials/watchlists", watchlists.RenderWatchlistsPartial)
	app.Get("/api/watchlists/form", watchlists.GetForm)
	app.Get("/api/watchlists/:id/preview", preview.GetPreview)
	app.Post("/api/watchlists/:id/sync", watchlists.SyncWatchlist)
	app.Patch("/api/watchlists/:id/toggle", watchlists.ToggleWatchlist)
	app.Delete("/api/watchlists/:id", watchlists.DeleteWatchlist)

	return app, db, user, watchlist, library, playlist, schedule
}

func renderedControls(t *testing.T, source string) []renderedHTMXControl {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(source))
	require.NoError(t, err, "handler response must be parseable HTML")
	attr := func(n *html.Node, key string) string {
		for _, a := range n.Attr {
			if a.Key == key {
				return a.Val
			}
		}
		return ""
	}
	text := func(n *html.Node) string {
		var b strings.Builder
		var walk func(*html.Node)
		walk = func(node *html.Node) {
			if node.Type == html.TextNode {
				b.WriteString(node.Data)
			}
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				walk(child)
			}
		}
		walk(n)
		return strings.TrimSpace(b.String())
	}
	selectedValue := func(selectNode *html.Node) string {
		var value string
		var found bool
		var walk func(*html.Node)
		walk = func(node *html.Node) {
			if node.Type == html.ElementNode && node.Data == "option" && hasHTMLAttr(node, "selected") {
				value, found = attr(node, "value"), true
			}
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				walk(child)
			}
		}
		walk(selectNode)
		if found {
			return value
		}
		return ""
	}

	var controls []renderedHTMXControl
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode {
			for _, method := range []string{"GET", "POST", "PATCH", "PUT", "DELETE"} {
				if path := attr(node, "hx-"+strings.ToLower(method)); path != "" {
					ctl := renderedHTMXControl{
						method: method, path: path, target: attr(node, "hx-target"),
						swap: attr(node, "hx-swap"), label: attr(node, "aria-label"),
						cardID: enclosingCardID(node), include: attr(node, "hx-include"), params: url.Values{},
					}
					if ctl.swap == "" {
						// htmx's default for an element target is innerHTML. A control that
						// does not say otherwise is asking for that, and this contract has
						// to compare what the browser will do rather than what the
						// attribute happens to spell out.
						ctl.swap = "innerHTML"
					}
					if ctl.label == "" {
						ctl.label = text(node)
					}
					if raw := attr(node, "hx-vals"); raw != "" {
						var values map[string]string
						require.NoErrorf(t, json.Unmarshal([]byte(raw), &values), "control %q has invalid/non-object hx-vals: %s", ctl.label, raw)
						ctl.values = values
					}
					if node.Data == "select" {
						ctl.isSelect = true
						ctl.params.Set(attr(node, "name"), selectedValue(node))
						// htmx hx-include="closest .filters" serializes the
						// other filter select too; derive those values from the
						// same parsed group rather than hard-coding either name.
						if ctl.include != "" {
							group := closestClass(node, "filters")
							if group != nil {
								collectNamedSelects(group, ctl.params, attr, selectedValue)
							}
						}
					}
					controls = append(controls, ctl)
					break
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)
	return controls
}

func enclosingCardID(node *html.Node) string {
	for current := node.Parent; current != nil; current = current.Parent {
		class := htmlAttr(current, "class")
		if classHas(class, "playlist-card") || classHas(class, "job-card") || classHas(class, "library-card") || classHas(class, "schedule-card") || classHas(class, "watchlist-card") {
			return htmlAttr(current, "id")
		}
	}
	return ""
}

func hasHTMLAttr(node *html.Node, key string) bool {
	for _, attr := range node.Attr {
		if attr.Key == key {
			return true
		}
	}
	return false
}

func closestClass(node *html.Node, class string) *html.Node {
	for current := node.Parent; current != nil; current = current.Parent {
		for _, token := range strings.Fields(htmlAttr(current, "class")) {
			if token == class {
				return current
			}
		}
	}
	return nil
}

func htmlAttr(node *html.Node, key string) string {
	for _, attr := range node.Attr {
		if attr.Key == key {
			return attr.Val
		}
	}
	return ""
}

func collectNamedSelects(group *html.Node, params url.Values, attr func(*html.Node, string) string, selected func(*html.Node) string) {
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == "select" {
			if name := attr(node, "name"); name != "" {
				params.Set(name, selected(node))
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(group)
}

func issueRenderedControl(t *testing.T, app *fiber.App, ctl renderedHTMXControl) (string, string) {
	t.Helper()
	parsed, err := url.Parse(ctl.path)
	require.NoError(t, err, "invalid htmx URL on %q", ctl.label)
	query := parsed.Query()
	for key, values := range ctl.params {
		for _, value := range values {
			query.Add(key, value)
		}
	}
	if ctl.method == "GET" && len(ctl.values) != 0 {
		for key, value := range ctl.values {
			query.Set(key, value)
		}
	}
	parsed.RawQuery = query.Encode()
	var body io.Reader
	contentType := ""
	if ctl.method == "POST" || ctl.method == "PATCH" || ctl.method == "PUT" {
		contentType = "application/x-www-form-urlencoded"
		if len(ctl.values) != 0 {
			form := url.Values{}
			for key, value := range ctl.values {
				form.Set(key, value)
			}
			body = strings.NewReader(form.Encode())
		}
	}
	req := httptest.NewRequest(ctl.method, parsed.String(), body)
	req.Header.Set("HX-Request", "true")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := app.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	response := string(raw)
	require.Lessf(t, resp.StatusCode, 400, "%s %s from %q returned %d: %s", ctl.method, parsed.String(), ctl.label, resp.StatusCode, response)
	return response, parsed.String()
}

func assertControlResponseTarget(t *testing.T, ctl renderedHTMXControl, response string) {
	t.Helper()
	require.NotEmpty(t, ctl.target, "%q must declare an hx-target", ctl.label)
	require.NotEmpty(t, ctl.swap, "%q must declare an hx-swap", ctl.label)
	root := responseRoot(t, response)
	class := htmlAttr(root, "class")
	id := htmlAttr(root, "id")
	pairs := map[string][]string{}
	switch {
	case classHas(class, "playlists-region"):
		pairs["#playlists-region"] = []string{"innerHTML"}
	case classHas(class, "jobs-region"):
		pairs["#jobs-region"] = []string{"innerHTML"}
	case classHas(class, "libraries-region"):
		pairs["#libraries-region"] = []string{"innerHTML"}
		pairs["closest .libraries-region"] = []string{"outerHTML"}
	case classHas(class, "schedules-region"):
		pairs["#schedules-region"] = []string{"innerHTML"}
	case classHas(class, "browse-region"):
		pairs["#libraries-region"] = []string{"innerHTML"}
	case classHas(class, "job-logs"):
		pairs["#job-logs-container"] = []string{"innerHTML"}
	case classHas(class, "scan-status"):
		pairs["#notice"] = []string{"innerHTML"}
	case classHas(class, "console-entry"):
		pairs["#notice"] = []string{"innerHTML"}
	case classHas(class, "watchlists-region"):
		pairs["#watchlists-region"] = []string{"innerHTML"}
		pairs["closest .watchlists-region"] = []string{"outerHTML"}
	case classHas(class, "watchlist-card") && id != "":
		pairs["#"+id] = []string{"outerHTML"}
	case classHas(class, "preview-header"):
		// The preview answers the tracks, and the region it belongs to is named
		// after the card that asked: derived from the rendered card, not a
		// hard-coded watchlist ID.
		pairs["#watchlist-preview-"+strings.TrimPrefix(ctl.cardID, "watchlist-")] = []string{"innerHTML"}
	case classHas(class, "modal-overlay"):
		pairs["#modal-container"] = []string{"innerHTML"}
	case classHas(class, "schedule-card") && id != "":
		pairs["#"+id] = []string{"outerHTML"}
	case strings.HasPrefix(strings.TrimSpace(response), "<p class=\"text-secondary\">"):
		pairs["#job-logs-container"] = []string{"innerHTML"}
	case classHas(class, "artist-card") && id != "":
		pairs["#"+id] = []string{"outerHTML"}
	default:
		require.Failf(t, "unclassified handler response", "%q returned root <%s class=%q id=%q>; add its real response shape to this classifier", ctl.label, root.Data, class, id)
		return
	}
	wantSwaps, ok := pairs[ctl.target]
	require.Truef(t, ok, "%q targets %q, but response root class=%q id=%q requires one of %v", ctl.label, ctl.target, class, id, keysOf(pairs))
	assert.Containsf(t, wantSwaps, ctl.swap, "%q swaps %q but this response shape requires %v", ctl.label, ctl.swap, wantSwaps)
}

func responseRoot(t *testing.T, body string) *html.Node {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(body))
	require.NoError(t, err)
	var find func(*html.Node) *html.Node
	find = func(node *html.Node) *html.Node {
		if node.Type == html.ElementNode && node.Data == "body" {
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				if child.Type == html.ElementNode {
					return child
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			if found := find(child); found != nil {
				return found
			}
		}
		return nil
	}
	root := find(doc)
	require.NotNil(t, root, "response has no element root: %s", body)
	return root
}

func classHas(classes, want string) bool {
	for _, class := range strings.Fields(classes) {
		if class == want {
			return true
		}
	}
	return false
}

func keysOf(values map[string][]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}

func renderedControlID(t *testing.T, label string) string {
	t.Helper()
	marker := strings.LastIndex(label, "#")
	require.GreaterOrEqual(t, marker, 0, "control label %q should expose the rendered row ID", label)
	id := strings.TrimSpace(label[marker+1:])
	require.NotEmpty(t, id, "control label %q must carry a row ID", label)
	return id
}

func assertRenderedControlSet(t *testing.T, controls []renderedHTMXControl, wantLabels ...string) {
	t.Helper()
	got := make([]string, 0, len(controls))
	for _, control := range controls {
		got = append(got, control.label)
	}
	for _, label := range wantLabels {
		assert.Containsf(t, got, label, "rendered control %q was not parsed/covered; found %v", label, got)
	}
	require.NotEmpty(t, controls, "the renderer must expose HTMX controls or this contract is vacuous")
}

func testPlaylistRegionControls(t *testing.T) {
	app, db, _, _, _, playlist, _ := listRegionTestApp(t)
	resp, _ := issueRenderedControl(t, app, renderedHTMXControl{method: "GET", path: "/partials/playlists"})
	controls := renderedControls(t, resp)
	assertRenderedControlSet(t, controls, "Create new playlist", "Delete playlist Delete Me")
	for _, ctl := range controls {
		response, requestURL := issueRenderedControl(t, app, ctl)
		assertControlResponseTarget(t, ctl, response)
		if ctl.method == "POST" && strings.HasSuffix(requestURL, "/api/playlists") {
			var created database.Playlist
			require.NoError(t, db.Where("name = ?", ctl.values["name"]).First(&created).Error, "hx-vals name must be the value the handler applies")
			assert.Equal(t, ctl.values["name"], created.Name)
		}
		if ctl.method == "DELETE" && strings.Contains(requestURL, "/api/playlists/") {
			id := strings.TrimPrefix(requestURL, "/api/playlists/")
			assert.Equal(t, "playlist-"+id, ctl.cardID, "the request ID must belong to the rendered playlist card")
			assert.Equal(t, playlist.ID.String(), id, "the delete URL must carry the playlist ID from the rendered card")
			var count int64
			require.NoError(t, db.Model(&database.Playlist{}).Where("id = ?", id).Count(&count).Error)
			assert.Zero(t, count, "the delete request must delete the playlist ID carried by its control")
		}
	}
}

func testJobsRegionControls(t *testing.T) {
	app, db, _, _, _, _, _ := listRegionTestApp(t)
	queued := database.Job{Type: "acquisition", State: "queued", RequestedAt: time.Now()}
	filteredQueued := database.Job{Type: "acquisition", State: "queued", RequestedAt: time.Now().Add(-time.Second)}
	failed := database.Job{Type: "sync", State: "failed", RequestedAt: time.Now().Add(-2 * time.Second)}
	require.NoError(t, db.Create(&queued).Error)
	require.NoError(t, db.Create(&filteredQueued).Error)
	require.NoError(t, db.Create(&failed).Error)

	// Non-default filters make the request values observable: each select
	// includes both named selects, and the handler must apply both values and
	// return them selected in the replacement region.
	filtered, _ := issueRenderedControl(t, app, renderedHTMXControl{
		method: "GET", path: "/partials/jobs?job_type=acquisition&state=queued",
	})
	filterControls := renderedControls(t, filtered)
	assertRenderedControlSet(t, filterControls, "Filter by job type", "Filter by job state")
	for _, ctl := range filterControls {
		if !ctl.isSelect {
			continue
		}
		body, requestURL := issueRenderedControl(t, app, ctl)
		assertControlResponseTarget(t, ctl, body)
		request, err := url.Parse(requestURL)
		require.NoError(t, err)
		for key, values := range ctl.params {
			for _, value := range values {
				assert.Equal(t, value, request.Query().Get(key), "the selected %s value must be sent by the control", key)
				if value != "" {
					assert.Containsf(t, body, `value="`+value+`" selected`, "the handler must preserve %s=%s in the response", key, value)
				}
			}
		}
		assert.Contains(t, body, "job-"+strconv.FormatUint(queued.ID, 10), "handler must render a row matching both selected filters")
		assert.Contains(t, body, "job-"+strconv.FormatUint(filteredQueued.ID, 10), "handler must render every row matching both selected filters")
		assert.NotContains(t, body, "job-"+strconv.FormatUint(failed.ID, 10), "handler must exclude a row outside the selected filters")
	}

	// Card actions rendered under active filters have to preserve those filter
	// values while applying the action to the card's own job ID.
	var filteredCancel *renderedHTMXControl
	for i := range filterControls {
		if strings.Contains(filterControls[i].path, "/cancel?") {
			filteredCancel = &filterControls[i]
			break
		}
	}
	require.NotNil(t, filteredCancel, "the filtered queued card must expose its cancel action")
	filteredCancelBody, filteredCancelURL := issueRenderedControl(t, app, *filteredCancel)
	assertControlResponseTarget(t, *filteredCancel, filteredCancelBody)
	cancelRequest, err := url.Parse(filteredCancelURL)
	require.NoError(t, err)
	assert.Equal(t, "acquisition", cancelRequest.Query().Get("job_type"), "cancel must preserve the job_type control value")
	assert.Equal(t, "queued", cancelRequest.Query().Get("state"), "cancel must preserve the state control value")
	assert.Contains(t, filteredCancelBody, `value="acquisition" selected`, "cancel response must preserve job_type")
	assert.Contains(t, filteredCancelBody, `value="queued" selected`, "cancel response must preserve state")
	filteredCancelID := strings.TrimSuffix(strings.TrimPrefix(cancelRequest.Path, "/api/jobs/"), "/cancel")
	assert.Equal(t, "job-"+filteredCancelID, filteredCancel.cardID, "cancel URL ID must belong to its rendered card")
	var filteredAfter database.Job
	require.NoError(t, db.First(&filteredAfter, "id = ?", filteredCancelID).Error)
	assert.Equal(t, "cancelled", filteredAfter.State, "the rendered cancel control must apply to its own job ID")

	// The unfiltered region renders every state-dependent action so those
	// requests can be issued too.
	response, _ := issueRenderedControl(t, app, renderedHTMXControl{method: "GET", path: "/partials/jobs"})
	controls := renderedControls(t, response)
	assertRenderedControlSet(t, controls, "View logs for job #"+strconv.FormatUint(queued.ID, 10), "Cancel job #"+strconv.FormatUint(filteredQueued.ID, 10), "Retry job #"+strconv.FormatUint(failed.ID, 10))
	for _, ctl := range controls {
		if ctl.isSelect {
			continue
		}
		body, requestURL := issueRenderedControl(t, app, ctl)
		assertControlResponseTarget(t, ctl, body)
		request, err := url.Parse(requestURL)
		require.NoError(t, err)
		switch {
		case strings.Contains(request.Path, "/job-logs"):
			jobID := request.Query().Get("job_id")
			assert.Equal(t, "job-"+jobID, ctl.cardID, "the query ID must belong to the card that rendered this control")
			assert.Equal(t, renderedControlID(t, ctl.label), jobID, "the job ID in the control label must reach the log handler")
			var selected database.Job
			require.NoError(t, db.First(&selected, "id = ?", jobID).Error, "the query value must identify an existing rendered job")
			assert.Contains(t, body, "Logs for Job #"+jobID, "the response must identify the job selected by the control query")
		case strings.HasSuffix(request.Path, "/cancel"):
			jobID := strings.TrimSuffix(strings.TrimPrefix(request.Path, "/api/jobs/"), "/cancel")
			assert.Equal(t, "job-"+jobID, ctl.cardID, "the cancel URL ID must belong to the card that rendered this control")
			assert.Equal(t, renderedControlID(t, ctl.label), jobID, "the cancel URL must carry the ID from the rendered label")
			var after database.Job
			require.NoError(t, db.First(&after, "id = ?", jobID).Error, "cancel URL must identify an existing rendered job")
			assert.Equal(t, "cancelled", after.State, "cancel must apply to the job ID in the control URL")
		case strings.HasSuffix(request.Path, "/retry"):
			jobID := strings.TrimSuffix(strings.TrimPrefix(request.Path, "/api/jobs/"), "/retry")
			assert.Equal(t, "job-"+jobID, ctl.cardID, "the retry URL ID must belong to the card that rendered this control")
			assert.Equal(t, renderedControlID(t, ctl.label), jobID, "the retry URL must carry the ID from the rendered label")
			var after database.Job
			require.NoError(t, db.First(&after, "id = ?", jobID).Error, "retry URL must identify an existing rendered job")
			assert.Equal(t, "queued", after.State, "retry must apply to the job ID in the control URL")
		}
	}
}

func testLibraryRegionControls(t *testing.T) {
	app, db, user, _, library, _, _ := listRegionTestApp(t)
	response, _ := issueRenderedControl(t, app, renderedHTMXControl{method: "GET", path: "/partials/libraries"})
	controls := renderedControls(t, response)
	assertRenderedControlSet(t, controls, "Add new library", "Browse library Contract Library", "Scan library Contract Library", "Enrich library Contract Library", "Edit library Contract Library", "Delete library Contract Library")
	for _, ctl := range controls {
		body, requestURL := issueRenderedControl(t, app, ctl)
		assertControlResponseTarget(t, ctl, body)
		switch {
		case strings.Contains(requestURL, "/browse"):
			resourceID := strings.TrimSuffix(strings.TrimPrefix(requestURL, "/partials/libraries/"), "/browse")
			assert.Equal(t, "library-"+resourceID, ctl.cardID, "the browse URL ID must belong to the card that rendered this control")
			assert.Equal(t, library.ID.String(), resourceID, "the browse URL must carry the library ID rendered on its card")
			assert.Contains(t, body, "browse-region")
			assert.Contains(t, body, library.Name, "the handler response must show the library selected by the control URL")
		case strings.HasSuffix(requestURL, "/scan"), strings.HasSuffix(requestURL, "/enrich"):
			var count int64
			kind := "scan"
			if strings.HasSuffix(requestURL, "/enrich") {
				kind = "enrich"
			}
			resourceID := strings.TrimSuffix(strings.TrimSuffix(strings.TrimPrefix(requestURL, "/api/libraries/"), "/scan"), "/enrich")
			assert.Equal(t, "library-"+resourceID, ctl.cardID, "the job action URL ID must belong to the card that rendered this control")
			assert.Equal(t, library.ID.String(), resourceID, "the job action URL must carry the card's library ID")
			require.NoError(t, db.Model(&database.Job{}).Where("scope_id = ? AND job_type = ?", resourceID, kind).Count(&count).Error)
			assert.EqualValues(t, 1, count, "the library control must create the %s job for its rendered library ID", kind)
		case strings.HasSuffix(requestURL, "/form"):
			assert.Contains(t, body, "modal-overlay")
			if strings.Contains(requestURL, "?id=") {
				assert.Contains(t, body, `name="id" value="`+library.ID.String()+`"`, "edit form must carry the library ID from the control query")
				assert.Contains(t, body, library.Path, "edit form must load the library identified by the control")
			}
		case ctl.method == "DELETE" && strings.Contains(requestURL, "/api/libraries/"):
			resourceID := strings.TrimPrefix(requestURL, "/api/libraries/")
			assert.Equal(t, "library-"+resourceID, ctl.cardID, "the delete URL ID must belong to the card that rendered this control")
			assert.Equal(t, library.ID.String(), resourceID, "the delete URL must carry this rendered library ID")
			var count int64
			require.NoError(t, db.Model(&database.Library{}).Where("id = ?", resourceID).Count(&count).Error)
			assert.Zero(t, count, "the delete action removes the library ID in its URL")
		}
	}

	// Exercise the conditional adoption offer through CreateLibrary's real
	// duplicate-path branch; the control is absent from an ordinary region.
	orphan := database.Library{Name: "Orphan", Path: filepath.Clean(t.TempDir())}
	require.NoError(t, db.Create(&orphan).Error)
	form := url.Values{"name": {"Claim orphan"}, "path": {orphan.Path}}
	request := httptest.NewRequest("POST", "/api/libraries", strings.NewReader(form.Encode()))
	request.Header.Set("HX-Request", "true")
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	conflict, err := app.Test(request)
	require.NoError(t, err)
	require.Equal(t, 200, conflict.StatusCode)
	conflictBody, err := io.ReadAll(conflict.Body)
	require.NoError(t, err)
	adoptControls := renderedControls(t, string(conflictBody))
	var adopt *renderedHTMXControl
	for i := range adoptControls {
		if strings.Contains(adoptControls[i].path, "/adopt") {
			adopt = &adoptControls[i]
			break
		}
	}
	require.NotNil(t, adopt, "the ownerless conflict response must expose the adopt control")
	adoptBody, _ := issueRenderedControl(t, app, *adopt)
	assertControlResponseTarget(t, *adopt, adoptBody)
	adoptRequest, err := url.Parse(adopt.path)
	require.NoError(t, err)
	adoptedID := strings.TrimSuffix(strings.TrimPrefix(adoptRequest.Path, "/api/libraries/"), "/adopt")
	assert.Equal(t, orphan.ID.String(), adoptedID, "the adoption control URL must carry the orphan ID")
	var claimed database.Library
	require.NoError(t, db.First(&claimed, "id = ?", adoptedID).Error)
	require.NotNil(t, claimed.OwnerUserID)
	assert.Equal(t, user.ID, *claimed.OwnerUserID, "the button's URL must identify the row the handler applies adoption to")
}

func testScheduleRegionControls(t *testing.T) {
	app, db, _, _, _, _, schedule := listRegionTestApp(t)
	second := database.Schedule{WatchlistID: schedule.WatchlistID, CronExpr: "0 12 * * *", Timezone: "UTC", Enabled: true}
	require.NoError(t, db.Create(&second).Error)
	response, _ := issueRenderedControl(t, app, renderedHTMXControl{method: "GET", path: "/partials/schedules"})
	controls := renderedControls(t, response)
	assertRenderedControlSet(t, controls, "Add new schedule", "Disable schedule for Contract Watchlist", "Edit schedule for Contract Watchlist", "Delete schedule for Contract Watchlist")
	for _, ctl := range controls {
		body, requestURL := issueRenderedControl(t, app, ctl)
		assertControlResponseTarget(t, ctl, body)
		if strings.HasSuffix(requestURL, "/toggle") {
			var after database.Schedule
			id := strings.TrimSuffix(strings.TrimPrefix(requestURL, "/api/schedules/"), "/toggle")
			assert.Equal(t, "schedule-"+id, ctl.cardID, "the toggle URL ID must belong to the card that rendered this control")
			require.NoError(t, db.First(&after, "id = ?", id).Error)
			assert.False(t, after.Enabled, "toggle applies to the schedule id in the rendered control")
			assert.Contains(t, body, "Enable schedule", "the response card reflects the state applied by the toggle")
		}
		if strings.HasSuffix(requestURL, "/form") {
			assert.Contains(t, body, "modal-overlay")
			if strings.Contains(requestURL, "?id=") {
				request, err := url.Parse(requestURL)
				require.NoError(t, err)
				scheduleID := request.Query().Get("id")
				assert.Equal(t, "schedule-"+scheduleID, ctl.cardID, "edit query ID must belong to the card that rendered this control")
				assert.Contains(t, body, `name="id" value="`+scheduleID+`"`, "edit form must carry the schedule ID from its control URL")
				var selectedSchedule database.Schedule
				require.NoError(t, db.First(&selectedSchedule, "id = ?", scheduleID).Error)
				assert.Contains(t, body, `value="`+selectedSchedule.CronExpr+`"`, "edit form must load the cron value for the schedule ID in the control URL")
				assert.Contains(t, body, `value="`+selectedSchedule.WatchlistID.String()+`" selected`, "edit form must select that schedule's watchlist")
			}
		}
		if ctl.method == "DELETE" && strings.Contains(requestURL, "/api/schedules/") {
			id := strings.TrimPrefix(requestURL, "/api/schedules/")
			assert.Equal(t, "schedule-"+id, ctl.cardID, "the delete URL ID must belong to the card that rendered this control")
			var count int64
			require.NoError(t, db.Model(&database.Schedule{}).Where("id = ?", id).Count(&count).Error)
			assert.Zero(t, count, "delete applies to its rendered schedule id")
		}
	}
}
