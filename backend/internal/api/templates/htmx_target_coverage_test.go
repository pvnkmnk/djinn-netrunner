package templates

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file guards where an htmx partial may send its response.
//
// The watchlist Sync button carried hx-target="#console-logs". That id is
// declared by index.html — the dashboard — and by nothing else. On the
// dashboard the button worked; on /watchlists, the page that actually lists
// watchlists, htmx found no element matching the target and dropped the request
// before it was ever sent. No request, no error, no visible change: the Sync
// button on a watchlist did nothing at all, and nothing in the codebase, CI or
// e2e suite could see it, because every Playwright assertion about Sync was
// written against the dashboard.
//
// The rule this file enforces: a partial may not target an id that only a
// single page declares. Such a target works on that page and is silently inert
// everywhere the partial is actually used. Response targets have to live in
// layouts/base.html, so they exist on every page.

var (
	idAttrRE = regexp.MustCompile(`\bid="([^"]+)"`)
	// hx-target / hx-swap-oob / hx-select-oob all name a swap destination.
	// Only hx-target matters here: the others describe the response, not where
	// it lands.
	hxTargetRE = regexp.MustCompile(`hx-target\s*=\s*"#([^"]+)"`)
)

// TestPartialsDoNotTargetPageLocalElements is the guard. Ids declared by a page
// template (a file directly under ops/web/templates) are page-local by
// definition; a partial targeting one of them can only work on that page.
func TestPartialsDoNotTargetPageLocalElements(t *testing.T) {
	templatesDir, _ := webAssetPaths(t)

	pageIds := pageLocalIDs(t, templatesDir)
	require.NotEmpty(t, pageIds, "no page-local ids found — the scan itself is broken")

	type offence struct {
		template string
		target   string
		page     string
	}

	var offences []offence
	err := filepath.WalkDir(filepath.Join(templatesDir, "partials"), func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".html") {
			return err
		}
		rel, relErr := filepath.Rel(templatesDir, path)
		if relErr != nil {
			return relErr
		}
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		for _, match := range hxTargetRE.FindAllStringSubmatch(string(body), -1) {
			if owner, ok := pageIds[match[1]]; ok {
				offences = append(offences, offence{rel, match[1], owner})
			}
		}
		return nil
	})
	require.NoError(t, err)

	if len(offences) > 0 {
		var b strings.Builder
		b.WriteString("these partials target an element only one page declares.\n")
		b.WriteString("They work on that page and do nothing anywhere else:\n")
		sort.Slice(offences, func(i, j int) bool { return offences[i].template < offences[j].template })
		for _, o := range offences {
			fmt.Fprintf(&b, "\n  %s\n      hx-target=\"#%s\"\n      declared only by: %s\n",
				o.template, o.target, o.page)
		}
		b.WriteString("\nMove the target element into layouts/base.html so it exists on\n")
		b.WriteString("every page, or swap the response into an id the partial owns.\n")
		t.Error(b.String())
	}
}

// TestNoticeRegionIsInTheBaseLayout keeps the fix honest. The Sync buttons now
// report into #notice, and the guard above only proves #notice is not
// page-local. This proves it is actually there.
func TestNoticeRegionIsInTheBaseLayout(t *testing.T) {
	templatesDir, _ := webAssetPaths(t)

	base, err := os.ReadFile(filepath.Join(templatesDir, "layouts", "base.html"))
	require.NoError(t, err)

	assert.Contains(t, string(base), `id="notice"`,
		"layouts/base.html must declare #notice — the Sync buttons post into it, "+
			"so without it those buttons go inert on every page again")
}

// pageLocalIDs maps each id declared by a page template to the file declaring
// it. Ids in layouts/ and partials/ are deliberately excluded: those are the
// shared surfaces, and targeting them is the correct thing to do.
func pageLocalIDs(t *testing.T, templatesDir string) map[string]string {
	t.Helper()

	entries, err := os.ReadDir(templatesDir)
	require.NoError(t, err)

	ids := map[string]string{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".html") {
			continue
		}
		body, readErr := os.ReadFile(filepath.Join(templatesDir, entry.Name()))
		if readErr != nil {
			t.Fatalf("reading %s: %v", entry.Name(), readErr)
		}
		for _, match := range idAttrRE.FindAllStringSubmatch(string(body), -1) {
			// First declaration wins; a duplicate id across pages is its own
			// problem and not what this test is here to decide.
			if _, seen := ids[match[1]]; !seen {
				ids[match[1]] = entry.Name()
			}
		}
	}
	return ids
}