package api

import (
	"os"
	"path/filepath"
	"regexp"
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

// TestTheConcurrencyLimitWarnsInsteadOfClamping holds the matrix's easiest row
// to falsify by accident. NetrunnerLite keeps whatever MAX_CONCURRENT_JOBS
// says, warns that concurrent workers are unsafe without PostgreSQL advisory
// locks, and runs anyway - so clamping the value to 1 would make the warning
// true and the row ("one; MAX_CONCURRENT_JOBS > 1 warns, it does not clamp")
// a lie in the same commit.
func TestTheConcurrencyLimitWarnsInsteadOfClamping(t *testing.T) {
	root := repoRoot(t)
	doc := readRepoFile(t, netrunnerLiteDoc)
	sources := nonTestGoSources(t, root)

	const worker = "backend/cmd/worker/main.go"
	src, ok := sources[worker]
	require.True(t, ok, "%s is gone; the concurrency row in %s cites it", worker, netrunnerLiteDoc)

	// The default the matrix states, and the wording the row quotes as its
	// evidence: a reworded warning leaves the document asserting a log line
	// nobody emits, and a changed default leaves the Postgres column wrong.
	assert.Regexp(t, `DefaultMaxConcurrentJobs\s*=\s*5\b`, src,
		"%s's matrix states \"MAX_CONCURRENT_JOBS (default 5)\"; the constant has moved", netrunnerLiteDoc)
	assert.Contains(t, doc, "SQLite detected with MaxConcurrentJobs > 1",
		"%s quotes the warning as its evidence", netrunnerLiteDoc)

	// Both halves of the gate, because only the pair makes the row true: on
	// SQLite AND with a cap above one.
	lines := strings.Split(src, "\n")
	gate := -1
	for i, line := range lines {
		if strings.Contains(line, "!database.IsPostgres(") && strings.Contains(line, "maxConcurrentJobs() > 1") {
			gate = i
			break
		}
	}
	require.NotEqual(t, -1, gate,
		"the concurrency warning is no longer gated on `!database.IsPostgres(...) && maxConcurrentJobs() > 1`; "+
			"%s's matrix row and \"What NetrunnerLite does not promise\" both describe that gate", netrunnerLiteDoc)

	warned := liteBlockBody(lines, gate)
	assert.Contains(t, warned, "SQLite detected with MaxConcurrentJobs > 1",
		"the gate no longer warns: it has to say why the configuration is unsafe, not just be a branch")
	assert.NotRegexp(t, liteConcurrencyClampRE, warned,
		"the SQLite branch narrows the job cap instead of warning about it. The warning is meant to be the "+
			"edition's limit, not its enforcement, so %s needs its row and its \"not enforced\" note rewritten",
		netrunnerLiteDoc)

	// And nowhere else either: a clamp written in a different edition branch
	// would keep the warning printing while the promise above it stopped being
	// true, which is the one way this row can be false and still look honest.
	for rel, source := range sources {
		sourceLines := strings.Split(source, "\n")
		for i, line := range sourceLines {
			if !strings.Contains(line, "IsPostgres(") || !strings.HasSuffix(strings.TrimSpace(line), "{") {
				continue
			}
			assert.NotRegexp(t, liteConcurrencyClampRE, liteBlockBody(sourceLines, i),
				"%s lowers the job cap inside an IsPostgres branch (line %d): %s says the limit warns and does "+
					"not clamp", rel, i+1, netrunnerLiteDoc)
		}
	}
}

// TestSubsonicIsGatedByConfigNotByEdition holds the row that keeps the Subsonic
// surface out of the editions story: the whole /rest group hangs off
// cfg.Subsonic.Enabled, so a Lite deployment that switches it on gets it -
// and turning that gate into an edition check would make the row wrong while
// looking like a tightening.
func TestSubsonicIsGatedByConfigNotByEdition(t *testing.T) {
	root := repoRoot(t)
	doc := readRepoFile(t, netrunnerLiteDoc)
	sources := nonTestGoSources(t, root)

	const server = "backend/cmd/server/main.go"
	src, ok := sources[server]
	require.True(t, ok, "%s is gone; the Subsonic row in %s cites it", server, netrunnerLiteDoc)

	assert.Contains(t, doc, "cfg.Subsonic.Enabled",
		"%s no longer names the gate for the Subsonic surface", netrunnerLiteDoc)

	lines := strings.Split(src, "\n")
	gate := -1
	for i, line := range lines {
		if strings.Contains(line, "if cfg.Subsonic.Enabled {") {
			gate = i
			break
		}
	}
	require.NotEqual(t, -1, gate,
		"the Subsonic group is no longer registered inside `if cfg.Subsonic.Enabled {`; %s's row cites that gate",
		netrunnerLiteDoc)

	// The surface has to be inside the block, not merely after it: this is what
	// makes \"gated by config\" a promise rather than a reading of the if.
	gated := liteBlockBody(lines, gate)
	assert.Contains(t, gated, `app.Group("/rest")`,
		"the /rest group has to sit inside the config gate, or \"enabled\" only hides routes while the "+
			"surface still answers requests")
	assert.Contains(t, gated, `"/ping.view"`,
		"the gated block no longer registers the Subsonic surface %s describes", netrunnerLiteDoc)

	// And the gate itself has to stay a config check. An edition check here
	// would make the row's \"identical\" cell false for Lite.
	assert.NotContains(t, gated, "IsPostgres",
		"the Subsonic group is now inside an edition check; %s's row has to say so instead", netrunnerLiteDoc)
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

// liteConcurrencyClampRE matches an assignment to the job cap, in either of the
// two names it goes by: the config field (cfg.MaxConcurrentJobs) and the
// orchestrator's resolved value (w.maxJobs). The trailing [^=] keeps a
// comparison such as `maxJobs == 1` out of it.
var liteConcurrencyClampRE = regexp.MustCompile(`(?i)\b(maxjobs|maxconcurrentjobs)\s*=[^=]`)

// liteBlockBody returns the text between the brace that opens on line start and
// the brace that closes it, so an assertion can be scoped to a gate instead of
// to the whole file - "the registration is inside that if" is a claim about a
// block, and a file-wide grep cannot tell it from "the registration comes
// later". Braces are counted per line, which is enough for the branches this
// file reads; a brace inside a string literal on those lines would throw the
// count off, and the count ends at zero, so that shows up as an assertion
// against a truncated or empty window rather than as a silent pass.
func liteBlockBody(lines []string, start int) string {
	depth := 0
	var body strings.Builder
	for i := start; i < len(lines); i++ {
		depth += strings.Count(lines[i], "{") - strings.Count(lines[i], "}")
		if i > start {
			body.WriteString(lines[i])
			body.WriteByte('\n')
		}
		if depth <= 0 && i > start {
			break
		}
	}
	return body.String()
}
