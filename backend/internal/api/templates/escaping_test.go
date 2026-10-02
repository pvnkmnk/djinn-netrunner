package templates

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file guards one invariant: every value a template renders is escaped
// exactly once.
//
// pongo2 autoescapes by default - `var autoescape = true` at package level,
// copied onto every execution context - and this engine never disarms it. The
// autoescape path *is* the escape filter, so an explicit `{{ x | escape }}`
// ran a second pass: an artist named `Converge & Chelsea Wolfe` reached the
// artists card as `Converge &amp; Chelsea Wolfe`, and 130 filters across 21
// templates did the same in text, attributes and aria-labels alike. The fix
// was to delete every one of them (DJI-590).
//
// Two guards hold that fix in place. Every partial is rendered with hostile
// data and must carry it escaped exactly once, and the template tree is
// scanned for every spelling that escapes twice or disarms the engine. The
// engine's own disarm switch (SetAutoescape(false)) is deliberately not
// scanned in Go code: with it off the render table fails, which is the same
// detection without a second walker to maintain.

// escapingProbe carries the five characters pongo2's escape filter touches
// (`&`, `>`, `<`, `"`, `'`) in text and in attribute position.
const escapingProbe = `Tom & Jerry <script>alert("x")</script> it's`

// escapingProbeWant is the probe after exactly one escape. A row that cannot
// find it skipped its value rather than escaped it.
const escapingProbeWant = "Tom &amp; Jerry"

// doubleEscapedMarkers are the second-pass forms of each escaped character:
// any one of them in a rendered partial means a value was escaped twice.
var doubleEscapedMarkers = []string{
	escapeHTML("&amp;"),
	escapeHTML("&lt;"),
	escapeHTML("&gt;"),
	escapeHTML("&quot;"),
	escapeHTML("&#39;"),
}

// htmlEntity matches a complete HTML character reference (named, decimal or
// hex). Every `&` in a rendered partial must start one: a bare `&` is data the
// engine did not escape, whichever filter emitted it.
var htmlEntity = regexp.MustCompile(`&(?:#[0-9]+|#[xX][0-9a-fA-F]+|[a-zA-Z][a-zA-Z0-9]*);`)

// partialProbes is the hostile-data row for every partial in the tree, keyed
// by template path. Every string in a context comes from escapingProbe, so a
// row exercises every value its partial renders. The test fails when a partial
// exists without a row, so adding a template is adding one entry here.
var partialProbes = map[string]map[string]any{
	"partials/acquire-form.html": {
		"error": escapingProbe, "artist": escapingProbe, "album": escapingProbe, "title": escapingProbe,
		"profiles": []map[string]any{probeProfile()},
	},
	"partials/admin_audit.html": {
		"Entries": []map[string]any{{
			"CreatedAt": time.Now(), "Action": escapingProbe, "ActorID": "7",
			"TargetType": escapingProbe, "TargetID": escapingProbe, "Metadata": escapingProbe,
		}},
	},
	"partials/admin_config.html": {
		"Settings": []map[string]any{{"Key": escapingProbe, "Value": escapingProbe}},
	},
	"partials/admin_users.html": {
		"Users": []map[string]any{{
			"ID": "user-1", "Email": escapingProbe, "Role": escapingProbe,
			"CreatedAt": time.Now(), "LastLoginAt": time.Time{},
		}},
	},
	"partials/artist-candidates.html": {
		"candidates": []map[string]any{{
			"ID": "mbid-1", "Name": escapingProbe,
			"Disambiguation": escapingProbe, "Country": escapingProbe, "Type": escapingProbe,
		}},
		"query": escapingProbe, "quality_profile_id": "profile-1", "retryEndpoint": "/api/artists/search",
	},
	"partials/artist-card.html": {"Artist": probeArtist()},
	"partials/artist-form.html": {"profiles": []map[string]any{probeProfile()}},
	"partials/artists.html":     {"artists": []map[string]any{probeArtist()}},
	"partials/job-logs.html": {
		"job_id": "job-1",
		"logs": []map[string]any{{
			"CreatedAt": time.Now(), "Level": "info", "Message": escapingProbe,
		}},
	},
	"partials/jobs.html": {
		"jobs": []map[string]any{
			{"ID": "job-queued", "Type": escapingProbe, "State": "queued",
				"RequestedAt": time.Now(), "CreatedBy": escapingProbe, "Summary": escapingProbe},
			{"ID": "job-failed", "Type": escapingProbe, "State": "failed",
				"RequestedAt": time.Now(), "CreatedBy": escapingProbe, "Summary": escapingProbe, "ErrorDetail": escapingProbe},
		},
		"QueueStatuses": map[string]any{"job-queued": map[string]any{"Position": 2, "Reason": escapingProbe}},
		"JobTypes":      []map[string]any{{"Value": "acquisition", "Label": escapingProbe}},
		"JobType":       "",
		"State":         "",
	},
	"partials/libraries.html": {
		"libraries": []map[string]any{{"ID": "library-1", "Name": escapingProbe, "Path": escapingProbe}},
	},
	"partials/library-browse.html": {
		"library": map[string]any{"ID": "library-1", "Name": escapingProbe},
		"tracks":  []map[string]any{probeTrack()},
		"total":   1, "search": escapingProbe, "sort_by": "title", "sort_dir": "asc",
		"sortToggle": map[string]any{"title": "desc", "artist": "desc", "album": "desc", "track_num": "desc", "format": "desc"},
		"page":       1, "total_pages": 2, "page_size": 50,
	},
	"partials/library-form.html": {"ID": "", "Name": escapingProbe, "Path": escapingProbe},
	"partials/playlists.html": {
		"playlists": []map[string]any{{"ID": "playlist-1", "Name": escapingProbe, "Description": escapingProbe, "Public": true}},
	},
	"partials/profile-form.html": {
		"ID": "", "IsNew": true, "Name": escapingProbe, "Description": escapingProbe,
		"AllowedFormats": escapingProbe, "MinBitrate": 320, "CoverArtSources": escapingProbe,
	},
	"partials/profiles.html":      {"profiles": []map[string]any{probeProfile()}},
	"partials/schedule-card.html": {"schedule": probeSchedule()},
	"partials/schedule-form.html": {
		"ID": "", "watchlists": []map[string]any{probeWatchlist()},
		"WatchlistID": "watchlist-1", "CronExpr": escapingProbe, "Enabled": true,
	},
	"partials/schedules.html":      {"schedules": []map[string]any{probeSchedule()}},
	"partials/stats.html":          {"StatsError": escapingProbe},
	"partials/track-detail.html":   {"track": probeTrack()},
	"partials/watchlist-card.html": {"watchlist": probeWatchlist()},
	"partials/watchlist-form.html": {
		"ID": "", "Name": escapingProbe, "SourceType": "spotify_playlist", "SourceURI": escapingProbe,
		"profiles": []map[string]any{probeProfile()}, "QualityProfileID": "profile-1", "Enabled": true,
	},
	"partials/watchlist-preview.html": {
		"Tracks": []map[string]any{probeTrack()}, "TotalCount": 1,
		"SourceType": "spotify_playlist", "Remaining": 0, "HasMore": false,
	},
	"partials/watchlists.html": {
		"watchlists": []map[string]any{probeWatchlist()}, "spDcLinked": false,
	},
}

// TestPartials_RenderEveryValueEscapedExactlyOnce renders every partial through
// the real engine with hostile data: the probe must reach the page escaped
// once, never raw, and never escaped twice. The completeness check at the end
// fails when a partial exists without a row, so a new template cannot be added
// without exercising it here.
func TestPartials_RenderEveryValueEscapedExactlyOnce(t *testing.T) {
	templatesDir, _ := webAssetPaths(t)
	engine := NewPongo2(templatesDir, ".html")

	for name, context := range partialProbes {
		t.Run(name, func(t *testing.T) {
			body := renderPartial(t, engine, name, context)

			assert.Contains(t, body, escapingProbeWant,
				"%s did not render the probe; the row proves nothing about escaping", name)
			assert.NotContains(t, body, escapingProbe,
				"%s rendered the probe as markup: an unescaped value reached the page", name)
			assert.NotContains(t, body, "<script",
				"%s rendered a raw script tag: data must not become markup", name)
			assert.Zero(t, strings.Count(body, "&")-len(htmlEntity.FindAllString(body, -1)),
				"%s has a bare ampersand: every `&` must start an entity", name)
			for _, marker := range doubleEscapedMarkers {
				assert.NotContains(t, body, marker,
					"%s contains %q: a value was escaped twice, so the operator reads a literal entity", name, marker)
			}
		})
	}

	entries, err := os.ReadDir(filepath.Join(templatesDir, "partials"))
	require.NoError(t, err)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".html") {
			continue
		}
		rel := "partials/" + entry.Name()
		assert.Contains(t, partialProbes, rel,
			"%s has no hostile-data row; add one to partialProbes so the partial is rendered, not just scanned", rel)
	}
}

// bannedEscapeSpellings matches every way a template can escape a value a
// second time or disarm the engine's escape:
//
//	| escape / |escape / | e   pongo2's filter and its `e` alias
//	| safe                     tells the engine not to escape that value
//	| truncatechars_html       marks its output safe (filters_builtin.go:244)
//	| truncatewords_html       marks its output safe (filters_builtin.go:314)
//	{% autoescape off %}       disarms the engine for a block
//	{% filter escape %}        escapes the block's already-escaped output
//
// `{% filter %}` is a real pongo2 tag (tags_filter.go), so `{% filter escape
// %}{{ x }}{% endfilter %}` double-escapes without a pipe the first
// alternative would see. The two `_html` truncators return AsSafeValue, so
// they are `| safe` under a different name.
var bannedEscapeSpellings = regexp.MustCompile(
	`\|\s*(?:escape|e|safe|truncatechars_html|truncatewords_html)\b` +
		`|\{%-?\s*autoescape\s+off\b` +
		`|\{%-?\s*filter\b[^%}]*\b(?:escape|e)\b`)

// stripTemplateNoise removes the three comment forms pongo2 ignores, so a
// template may discuss a filter without the scan flagging its prose.
func stripTemplateNoise(source string) string {
	for _, comment := range []*regexp.Regexp{
		regexp.MustCompile(`(?s)\{#.*?#\}`),
		regexp.MustCompile(`(?s)\{%-?\s*comment\s*-?%\}.*?\{%-?\s*endcomment\s*-?%\}`),
		regexp.MustCompile(`(?s)<!--.*?-->`),
	} {
		source = comment.ReplaceAllString(source, "")
	}
	return source
}

// bannedEscapeFindings lists every banned spelling in a template source,
// comments excluded.
func bannedEscapeFindings(source string) []string {
	return bannedEscapeSpellings.FindAllString(stripTemplateNoise(source), -1)
}

// TestTemplateScan_RejectsEveryBannedEscapeSpelling proves the scan catches
// each banned spelling and leaves comments and innocent filters alone.
func TestTemplateScan_RejectsEveryBannedEscapeSpelling(t *testing.T) {
	cases := []struct {
		name   string
		source string
		caught bool
	}{
		{"pipe escape", `{{ x | escape }}`, true},
		{"pipe escape without space", `{{ x |escape }}`, true},
		{"short alias", `{{ x | e }}`, true},
		{"safe filter", `{{ x | safe }}`, true},
		{"truncatechars_html filter", `{{ x | truncatechars_html:40 }}`, true},
		{"truncatewords_html filter", `{{ x | truncatewords_html:5 }}`, true},
		{"autoescape off block", `{% autoescape off %}{{ x }}{% endautoescape %}`, true},
		{"filter block", `{% filter escape %}{{ x }}{% endfilter %}`, true},
		{"filter chain", `{% filter lower|escape %}{{ x }}{% endfilter %}`, true},
		{"pongo2 comment", `{# {{ x | escape }} #}`, false},
		{"html comment", `<!-- {{ x | escape }} -->`, false},
		{"comment block", `{% comment %}{{ x | escape }}{% endcomment %}`, false},
		{"urlencode control", `{{ x | urlencode }}`, false},
		{"truncatechars control", `{{ x | truncatechars:40 }}`, false},
		{"upper filter block", `{% filter upper %}{{ x }}{% endfilter %}`, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.caught {
				assert.NotEmpty(t, bannedEscapeFindings(tc.source), "%q must be rejected", tc.source)
				return
			}
			assert.Empty(t, bannedEscapeFindings(tc.source), "%q must not be flagged", tc.source)
		})
	}
}

// TestTemplates_ApplyNoBannedEscapeSpelling scans the whole template tree -
// pages and layouts included, not only the partials the render table covers.
func TestTemplates_ApplyNoBannedEscapeSpelling(t *testing.T) {
	templatesDir, _ := webAssetPaths(t)

	checked := 0
	err := filepath.WalkDir(templatesDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".html") {
			return nil
		}
		checked++
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(templatesDir, path)
		rel = filepath.ToSlash(rel)
		for _, finding := range bannedEscapeFindings(string(raw)) {
			assert.Fail(t, "template applies a banned escape spelling",
				"%s: %q escapes a value a second time or disarms the engine's escape. The engine "+
					"escapes every value exactly once, so delete the filter (DJI-590).", rel, finding)
		}
		return nil
	})
	require.NoError(t, err)
	require.Greater(t, checked, 20, "expected to scan the whole template tree; found only %d templates", checked)
}

// renderPartial renders a partial through the real engine, the path a handler
// uses.
func renderPartial(t *testing.T, engine *Pongo2Engine, name string, context map[string]any) string {
	t.Helper()

	var buf bytes.Buffer
	require.NoError(t, engine.Render(&buf, name, context), "%s must render", name)
	require.NotEmpty(t, buf.String(), "%s rendered nothing", name)
	return buf.String()
}

// escapeHTML applies pongo2's escape filter: &, >, <, " and ' in that order
// (filters_builtin.go).
func escapeHTML(value string) string {
	return strings.NewReplacer(
		"&", "&amp;",
		">", "&gt;",
		"<", "&lt;",
		`"`, "&quot;",
		"'", "&#39;",
	).Replace(value)
}

func probeArtist() map[string]any {
	return map[string]any{
		"ID": "artist-1", "Name": escapingProbe, "MusicBrainzID": escapingProbe,
		"Monitored": true, "AcquiredReleases": 1, "TotalReleases": 3, "LastScanLabel": escapingProbe,
	}
}

func probeProfile() map[string]any {
	return map[string]any{
		"ID": "profile-1", "Name": escapingProbe, "Description": escapingProbe,
		"AllowedFormats": escapingProbe, "MinBitrate": 320, "CoverArtSources": escapingProbe,
		"IsDefault": true, "PreferLossless": true,
	}
}

func probeSchedule() map[string]any {
	return map[string]any{
		"ID": "schedule-1", "Watchlist": map[string]any{"Name": escapingProbe},
		"CronExpr": escapingProbe, "NextRunLabel": escapingProbe, "Enabled": true,
	}
}

func probeTrack() map[string]any {
	return map[string]any{
		"ID": "track-1", "Title": escapingProbe, "Artist": escapingProbe, "Album": escapingProbe,
		"TrackNum": 1, "DiscNum": 1, "Format": escapingProbe, "FileSize": int64(1048576),
		"Year": 2024, "Genre": escapingProbe, "Composer": escapingProbe, "FileHash": escapingProbe,
		"Fingerprint": escapingProbe, "EnrichmentProvenance": escapingProbe, "CoverURL": escapingProbe,
	}
}

func probeWatchlist() map[string]any {
	return map[string]any{
		"ID": "watchlist-1", "Name": escapingProbe, "SourceType": "spotify_playlist",
		"SourceURI": escapingProbe, "Enabled": true,
	}
}
