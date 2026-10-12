package api

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// docs/NETRUNNERLITE.md is the audited per-feature boundary of the reduced
// NetrunnerLite edition, and the sentences the repo now leans on cite this
// tree rather than argue from memory: README.md's "dormant, not removed" note,
// docs/WHITEPAPER.md's deployment table, and ADR 0004 §2's
// one-line-per-edition rows. A citation can be checked, so these tests check
// it instead of trusting it. A failure is not "the text changed": it names the
// document that has to be corrected in the same PR that moved the code.
//
// They live beside e2e_credentials_test.go -- the repo's other test that
// asserts a checked-in artifact against the code -- and reuse its repoRoot and
// readRepoFile helpers.

const netrunnerLiteDoc = "docs/NETRUNNERLITE.md"

// TestNetrunnerLiteDocIsStillTheBoundaryAudit guards the document itself. It is
// the only per-feature statement of what NetrunnerLite promises, so an emptied
// or deleted audit turns ADR 0004 §2 and the README table back into a direction
// with nothing behind it.
func TestNetrunnerLiteDocIsStillTheBoundaryAudit(t *testing.T) {
	doc := readRepoFile(t, netrunnerLiteDoc)

	for _, section := range []string{
		"## How the edition is selected",
		"## The matrix",
		"## What NetrunnerLite does not promise",
		"## Gaps that are not edition differences",
		"## How this was verified",
		"## Keeping it true",
	} {
		assert.Contains(t, doc, section, "%s lost a section", netrunnerLiteDoc)
	}

	// The selection mechanism is the one claim the rest of the document rests
	// on: if the editions were ever split by a flag or a build tag instead of
	// by DATABASE_URL, every row under it would need re-deriving.
	assert.Contains(t, doc, "database.IsPostgres(DATABASE_URL)",
		"%s must keep naming how the edition is selected", netrunnerLiteDoc)
}

// TestEngineDivergenceIsConfinedToTheAuditedPlaces holds the document's
// strongest claim -- "Anything not on that list behaves identically on both
// editions" -- to the code. Every non-test Go file that branches on
// database.IsPostgres has to be one the document names, because a branch
// anywhere else is a new edition difference, and the matrix owes it a verdict
// row.
func TestEngineDivergenceIsConfinedToTheAuditedPlaces(t *testing.T) {
	root := repoRoot(t)
	doc := readRepoFile(t, netrunnerLiteDoc)

	var branching []string
	for rel, src := range nonTestGoSources(t, root) {
		if callsIsPostgres(src) {
			branching = append(branching, rel)
		}
	}
	sort.Strings(branching)

	require.NotEmpty(t, branching, "nothing branches on database.IsPostgres any more: either the "+
		"edition split moved somewhere this test cannot see, or %s describes a tree that no longer exists",
		netrunnerLiteDoc)

	for _, rel := range branching {
		assert.Contains(t, doc, filepath.Base(rel),
			"%s branches on database.IsPostgres but %s never names it: add the feature and its verdict to the matrix",
			rel, netrunnerLiteDoc)
	}
}

// TestTheFastPathIsStillDormant guards the sentence README.md,
// docs/WHITEPAPER.md and ADR 0004 all now carry: the NOTIFY sender is
// installed nowhere, so both editions poll on a 5-second loop. Two things would
// make it false -- a compose file mounting ops/db/init/, or Go code publishing
// a notification itself -- and the documents, not this test, are what would
// then have to change.
func TestTheFastPathIsStillDormant(t *testing.T) {
	root := repoRoot(t)
	sources := nonTestGoSources(t, root)

	// The SQL bootstrap is the only NOTIFY sender in the tree, so a compose
	// file mounting it is the activation path the backlog describes.
	compose := composeFiles(t, root)
	require.NotEmpty(t, compose, "no compose file found under the checkout root; this guard would pass vacuously")
	for _, rel := range compose {
		assert.NotContains(t, readRepoFile(t, rel), "db/init",
			"%s mounts ops/db/init/, so the NOTIFY sender is installed now: the \"Job wakeup\" note in "+
				"README.md, the deployment table in docs/WHITEPAPER.md, ADR 0004 §2 and %s all need correcting",
			rel, netrunnerLiteDoc)
	}

	// Publishing from the application is the other documented way to make the
	// fast path real.
	for rel, src := range sources {
		for _, line := range strings.Split(src, "\n") {
			trimmed := strings.TrimSpace(line)
			// Comment-only lines are skipped so prose about the dormant path
			// ("LISTEN/NOTIFY is never installed") cannot trip the guard; a
			// trailing comment that carries NOTIFY still would, which is the
			// loud direction for a guard to be wrong in.
			if strings.HasPrefix(trimmed, "//") {
				continue
			}
			assert.False(t, strings.Contains(line, "pg_notify") || strings.Contains(line, "NOTIFY "),
				"%s issues a notification (line %q): if the fast path is real now, say so in %s, README.md and ADR 0004 §2",
				rel, trimmed, netrunnerLiteDoc)
		}
	}
}

// TestWakeupChannelsAreStillTheOnesTheDocNames keeps the audit's evidence true
// at the one place it could rot silently: the two channel names. The worker
// listens on opswakeup and the web server on opsevents, and a rename would
// leave the document citing channels nothing subscribes to.
func TestWakeupChannelsAreStillTheOnesTheDocNames(t *testing.T) {
	root := repoRoot(t)
	doc := readRepoFile(t, netrunnerLiteDoc)
	sources := nonTestGoSources(t, root)

	for _, channel := range []string{"opswakeup", "opsevents"} {
		assert.Contains(t, doc, channel, "%s no longer names the %q channel", netrunnerLiteDoc, channel)

		listened := false
		for _, src := range sources {
			if strings.Contains(src, `"`+channel+`"`) {
				listened = true
				break
			}
		}
		assert.True(t, listened, "no Go file listens on %q any more: the channel was renamed, so %s and its "+
			"\"Keeping it true\" note are stale", channel, netrunnerLiteDoc)
	}
}

// nonTestGoSources maps every non-test .go file under backend/ to its contents,
// keyed by repo-relative slash path. The claims here are about sources the
// tests cannot import, so they are read as text -- the same choice
// ui_honesty_test.go makes when it reads templates instead of served HTML.
func nonTestGoSources(t *testing.T, root string) map[string]string {
	t.Helper()

	backend := filepath.Join(root, "backend")
	sources := map[string]string{}

	err := filepath.WalkDir(backend, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			switch entry.Name() {
			case "vendor", "testdata", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		sources[filepath.ToSlash(rel)] = string(raw)
		return nil
	})

	require.NoError(t, err)
	require.NotEmpty(t, sources, "%s is not a Go tree; the guard would pass vacuously", backend)
	return sources
}

// composeFiles lists every file a deployment could be brought up from, as
// repo-relative slash paths: the compose files at the checkout root plus any
// under ops/.
func composeFiles(t *testing.T, root string) []string {
	t.Helper()

	var found []string

	ops := filepath.Join(root, "ops")
	err := filepath.WalkDir(ops, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if !strings.HasSuffix(entry.Name(), ".yml") && !strings.HasSuffix(entry.Name(), ".yaml") {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		found = append(found, filepath.ToSlash(rel))
		return nil
	})
	require.NoError(t, err)

	atRoot, err := filepath.Glob(filepath.Join(root, "docker-compose*.yml"))
	require.NoError(t, err)
	for _, path := range atRoot {
		rel, relErr := filepath.Rel(root, path)
		require.NoError(t, relErr)
		found = append(found, filepath.ToSlash(rel))
	}

	sort.Strings(found)
	return found
}

// callsIsPostgres reports whether Go source branches on IsPostgres, ignoring
// the declaration and the comment-only lines that merely mention the name.
func callsIsPostgres(src string) bool {
	for _, line := range strings.Split(src, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "func IsPostgres(") {
			continue
		}
		if strings.Contains(line, "IsPostgres(") {
			return true
		}
	}
	return false
}
