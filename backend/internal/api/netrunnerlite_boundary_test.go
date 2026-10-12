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
// tree rather than argue from memory: README.md's "Job wakeup" note,
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

// TestTheFastPathPublishesOnTheChannelsTheListenersRead took over from a test
// that asserted the opposite: until the sender was installed, this file held the
// four documents to "the NOTIFY path is dormant", and its failure was the
// checklist for making it real. The sender is real now -- the triggers are
// installed by database.Migrate on every Postgres boot
// (backend/internal/database/notify.go) -- so the claim worth guarding changed
// from "nothing publishes" to "the two halves agree". A publisher on a channel
// nobody subscribes to, or an installer nothing calls, looks the same as a
// working fast path from every other angle: the worker's poll would hide it and
// no test outside this file reads both ends.
func TestTheFastPathPublishesOnTheChannelsTheListenersRead(t *testing.T) {
	root := repoRoot(t)
	sources := nonTestGoSources(t, root)

	// The sender is Go-side by design: the image ships no ops/db/init, and
	// Docker's init scripts only ever run against a fresh database.
	compose := composeFiles(t, root)
	require.NotEmpty(t, compose, "no compose file found under the checkout root; this guard would pass vacuously")
	for _, rel := range compose {
		assert.NotContains(t, readRepoFile(t, rel), "db/init",
			"%s mounts ops/db/init/, whose bootstrap targets tables the runtime schema does not have "+
				"(it creates triggers on `joblogs` and `sources`): the sender has one owner, "+
				"database.Migrate, and %s describes it", rel, netrunnerLiteDoc)
	}

	// A sender that nothing calls is dormant in every way that matters, so the
	// installer and its call site are both required.
	var installers []string
	called := ""
	for rel, src := range sources {
		if strings.Contains(src, "pg_notify(") && strings.Contains(src, "EXECUTE FUNCTION") {
			installers = append(installers, rel)
		}
		if strings.Contains(src, "installNotifyTriggers(") && !strings.Contains(src, "func installNotifyTriggers(") {
			called = rel
		}
	}
	sort.Strings(installers)
	require.NotEmpty(t, installers, "no non-test Go file installs the notification triggers: the fast path is "+
		"dormant again, so the wakeup row in %s, the \"Job wakeup\" note in README.md, the deployment table in "+
		"docs/WHITEPAPER.md and ADR 0004 §2 all have to say so instead of promising event-driven wakeups",
		netrunnerLiteDoc)
	require.NotEmpty(t, called, "nothing calls installNotifyTriggers: the triggers exist as text and are "+
		"installed by no boot, which is the dormant state %s, README.md, docs/WHITEPAPER.md and ADR 0004 §2 "+
		"were corrected away from", netrunnerLiteDoc)

	// Each channel is checked on both ends at once: the SQL literal the installer
	// publishes on, and the Go literal a listener subscribes with. Either alone can
	// be renamed without the other, which is the failure this test exists for.
	for _, channel := range []string{"opswakeup", "opsevents"} {
		publisher := false
		for _, rel := range installers {
			if strings.Contains(readRepoFile(t, rel), "'"+channel+"'") {
				publisher = true
				break
			}
		}
		assert.True(t, publisher, "the installed triggers publish nothing on %q: %s names that channel and a "+
			"listener subscribes to it, so either the sender lost an event or the document and the listeners "+
			"have to move to whatever replaced it", channel, netrunnerLiteDoc)
		assert.Contains(t, readRepoFile(t, netrunnerLiteDoc), channel,
			"%s no longer names the %q channel", netrunnerLiteDoc, channel)
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
