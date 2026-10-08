package main

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// coverageRow matches one endpoint row of docs/OPENSUBSONIC_COVERAGE.md and
// captures the endpoint name plus the status column, so the document and the
// route table are compared by parsing rather than by trusting either side.
var coverageRow = regexp.MustCompile("^\\|\\s*`([^`]+)`[^|]*\\|[^|]*\\|[^|]*\\|[^|]*\\|\\s*(impl|gap|unowned)\\s*\\|")

// documentedCoverage reads docs/OPENSUBSONIC_COVERAGE.md and returns every
// endpoint marked `impl`. It walks up to the repo root the way the templates
// tests do, rather than guessing a relative path that depends on the package's
// working directory.
func documentedCoverage(t *testing.T) []string {
	t.Helper()

	dir, err := os.Getwd()
	require.NoError(t, err)

	var path string
	for i := 0; i < 10; i++ {
		candidate := filepath.Join(dir, "docs", "OPENSUBSONIC_COVERAGE.md")
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			path = candidate
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	require.NotEmpty(t, path, "could not find docs/OPENSUBSONIC_COVERAGE.md above the working directory")

	raw, err := os.ReadFile(path)
	require.NoError(t, err)

	var names []string
	for _, line := range strings.Split(string(raw), "\n") {
		if m := coverageRow.FindStringSubmatch(line); m != nil && m[2] == "impl" {
			names = append(names, m[1])
		}
	}
	require.NotEmpty(t, names, "the coverage document lists no implemented endpoints")
	return names
}

// registeredRestEndpoints reads the real route table. The /rest group is
// registered only when cfg.Subsonic.Enabled is set, so a probe that skips that
// reports zero routes and reads as "the whole Subsonic surface is missing".
func registeredRestEndpoints(t *testing.T) []string {
	t.Helper()

	cfg := baseRouteTestConfig()
	cfg.Subsonic.Enabled = true
	cfg.Subsonic.Password = "coverage-guard-password"
	app := newRouteTestApp(t, cfg)

	seen := map[string]bool{}
	for _, r := range app.GetRoutes() {
		if r.Method != fiber.MethodGet || !strings.HasPrefix(r.Path, "/rest/") {
			continue
		}
		name := strings.TrimSuffix(strings.TrimPrefix(r.Path, "/rest/"), ".view")
		seen[name] = true
	}

	names := make([]string, 0, len(seen))
	for n := range seen {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// TestCoverageDocumentMatchesRouteTable is the guard that keeps the coverage
// number in docs/OPENSUBSONIC_COVERAGE.md from decaying into a claim. The
// document is generated, so it is only trustworthy if the generated `impl`
// rows equal the routes the server actually registers.
func TestCoverageDocumentMatchesRouteTable(t *testing.T) {
	documented := documentedCoverage(t)
	registered := registeredRestEndpoints(t)

	// license is a compatibility alias rather than a spec endpoint: it serves
	// getLicense and appears in the document's alias table, never as a matrix
	// row. Drop it from both sides so the comparison is spec endpoints only.
	const alias = "license"

	without := func(in []string) []string {
		var out []string
		for _, n := range in {
			if n != alias {
				out = append(out, n)
			}
		}
		sort.Strings(out)
		return out
	}

	assert.Equal(t, without(registered), without(documented),
		"docs/OPENSUBSONIC_COVERAGE.md and app.GetRoutes() disagree; "+
			"re-run scripts/opensubsonic_coverage.py after changing either")
}

// TestLicenseRouteIsReachableByItsSpecName pins Finding 1: the handler existed
// under a name no conformant client calls, and every test agreed the feature
// worked. Both the spec name and the back-compat alias must stay registered.
func TestLicenseRouteIsReachableByItsSpecName(t *testing.T) {
	registered := registeredRestEndpoints(t)
	assert.Contains(t, registered, "getLicense",
		"the spec spells this endpoint getLicense; a client asking for it must not 404")
	assert.Contains(t, registered, "license",
		"the license.view alias must stay registered for clients that learned the old name")
}

// TestSubsonicSurfaceIsNotVacuous guards the measurement itself. A coverage
// assertion that compares against an empty route table passes for the wrong
// reason, so assert the /rest group is non-empty under the same config the
// coverage tests use.
func TestSubsonicSurfaceIsNotVacuous(t *testing.T) {
	assert.GreaterOrEqual(t, len(registeredRestEndpoints(t)), 18,
		"the /rest route table collapsed; a coverage check against it would pass vacuously")
}
