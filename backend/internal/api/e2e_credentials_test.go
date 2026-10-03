package api

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

// The Playwright suite stopped being able to reach the product, and nothing
// said so. `e2e/fixtures/auth.fixture.ts` carried an 11-character password and
// an 8-character one; `POST /api/auth/register` enforces a 12-character floor
// (DefaultMinPasswordLength, added with DJI-544). `ensureUserExists` therefore
// got a 400 instead of a 201 and threw, so every spec using `authenticatedPage`
// or `adminPage` failed in fixture setup - while the specs that only drive the
// raw login page still passed. The suite read as partially working and the
// browser-level gate covered nothing at all.
//
// Two separate files then had to change together, which is what made it survive
// so long: the fixture's ADMIN_USER password and the bcrypt hash the e2e seed
// writes straight into the users table. The seed bypasses the register
// endpoint, so it can carry a password the endpoint would refuse, and the two
// halves only agree by hand.
//
// These tests read the e2e tree from the Go side because that is where the
// policy lives. A guard inside the Playwright suite could only assert that the
// fixture's own passwords are long enough, which is true by construction - the
// floor is a Go constant the TypeScript never sees. Reading it from here is the
// only place the two definitions meet.

// fixtureUserRE pulls the email and password out of one of the fixture's
// credential declarations.
var fixtureUserRE = regexp.MustCompile(
	`const\s+([A-Z_]+)\s*=\s*\{\s*email:\s*'([^']+)'\s*,\s*password:\s*'([^']+)'\s*\}`)

// seededHashRE pulls the bcrypt hash out of the e2e seed.
//
// The shell escapes every `$` inside that SQL string, so the file holds
// `\$2a\$10\$...` rather than the hash itself. Matching the bare form finds
// nothing and fails the test for a reason that has nothing to do with the
// password - which is exactly what the first version of this guard did, and
// why the escaping is commented here rather than left to be rediscovered.
var seededHashRE = regexp.MustCompile(`\\?\$2[aby]\\?\$\d{2}\\?\$[./A-Za-z0-9]{53}`)

// unescapeBCryptHash turns a hash as written in the shell script back into the
// hash as bcrypt sees it.
var bcryptEscapeRE = regexp.MustCompile(`\\\$`)

func unescapeBCryptHash(s string) string {
	return bcryptEscapeRE.ReplaceAllString(s, "$")
}

// repoRoot walks up from the test directory to the checkout root - the one
// holding `e2e/` - so these tests find the tree no matter how they are invoked.
func repoRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	require.NoError(t, err)

	for i := 0; i < 10; i++ {
		candidate := filepath.Join(dir, "e2e", "fixtures", "auth.fixture.ts")
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	t.Fatal("could not find e2e/fixtures/auth.fixture.ts above the test directory")
	return ""
}

func readRepoFile(t *testing.T, rel string) string {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join(repoRoot(t), rel))
	require.NoError(t, err, "%s must exist", rel)
	return string(raw)
}

// e2eCredentials returns the email and password the fixture declares for one
// of its credential constants.
func e2eCredentials(t *testing.T, name string) (email, password string) {
	t.Helper()

	for _, m := range fixtureUserRE.FindAllStringSubmatch(readRepoFile(t, "e2e/fixtures/auth.fixture.ts"), -1) {
		if m[1] == name {
			return m[2], m[3]
		}
	}

	t.Fatalf("auth.fixture.ts no longer declares %s; the e2e suite needs an account to log in with", name)
	return "", ""
}

// TestE2EFixtureCredentialsSatisfyTheRegistrationPolicy is the gate that was
// missing. Every credential the suite registers has to clear the floor the
// register endpoint enforces, or the suite cannot get as far as testing
// anything.
//
// Counted in runes, because that is what the handler counts - a floor enforced
// in bytes would be a different policy than the one this asserts.
func TestE2EFixtureCredentialsSatisfyTheRegistrationPolicy(t *testing.T) {
	for _, name := range []string{"TEST_USER", "ADMIN_USER"} {
		t.Run(name, func(t *testing.T) {
			email, password := e2eCredentials(t, name)
			require.NotEmpty(t, email, "%s has no email", name)

			assert.GreaterOrEqual(t, len([]rune(password)), DefaultMinPasswordLength,
				"the %s password is %d characters and POST /api/auth/register requires at "+
					"%d, so ensureUserExists gets a 400 and every spec using this fixture "+
					"fails during setup - the suite cannot reach the product at all",
				name, len([]rune(password)), DefaultMinPasswordLength)
		})
	}
}

// TestE2ESeededAdminHashMatchesTheFixturePassword pins the pair that has to
// change together. The admin account is inserted straight into the users table
// by e2e/setup-test-db.sh, so nothing enforces agreement between that hash and
// the password the fixture logs in with; they are two hand-maintained facts
// about one account.
//
// Verified with bcrypt rather than string-compared, because "the hash string in
// the script" and "the password that opens the account" are the two things that
// have to match, and only the comparison proves it.
func TestE2ESeededAdminHashMatchesTheFixturePassword(t *testing.T) {
	_, password := e2eCredentials(t, "ADMIN_USER")

	seed := readRepoFile(t, "e2e/setup-test-db.sh")
	hashes := seededHashRE.FindAllString(seed, -1)
	require.Len(t, hashes, 1,
		"expected exactly one bcrypt hash in the e2e seed, found %d; the admin "+
			"account is inserted directly and must carry exactly one password", len(hashes))

	assert.NoError(t, bcrypt.CompareHashAndPassword([]byte(unescapeBCryptHash(hashes[0])), []byte(password)),
		"the hash seeded for the e2e admin account does not open with the "+
			"ADMIN_USER password in auth.fixture.ts, so the admin seat cannot "+
			"log in and every adminPage test fails - regenerate the hash for "+
			"that password and update both in the same change")
}

// TestE2EAdminSeedReplacesThePasswordOnRerun guards the half of the pair that
// the hash comparison above cannot see.
//
// Comparing the seeded hash to the fixture password proves the script says the
// right thing. It cannot prove the database ends up that way, because the
// account may already exist from an earlier run against a carried-over volume.
// The seed used to guard its insert with WHERE NOT EXISTS, which meant a
// re-setup silently kept the previous password_hash - and because
// /api/auth/register answers 201 for an account that already exists,
// ensureUserExists reported success and every adminPage spec failed later, at
// the login, with nothing pointing at the cause. The same shape as the original
// defect: the gate reports health while carrying none.
//
// A static check, because the alternative is a test that needs a seeded
// database: what matters is that the seed overwrites an existing row rather
// than skipping it.
func TestE2EAdminSeedReplacesThePasswordOnRerun(t *testing.T) {
	seed := readRepoFile(t, "e2e/setup-test-db.sh")

	assert.NotContains(t, seed, "WHERE NOT EXISTS (SELECT 1 FROM users",
		"the e2e seed skips inserting the admin account when the row already "+
			"exists, so a database carried over from an earlier run keeps the "+
			"old password_hash and every adminPage spec fails at login while "+
			"ensureUserExists reports success")

	assert.Contains(t, seed, "ON CONFLICT (email) DO UPDATE SET password_hash",
		"the e2e seed must overwrite the admin account's password_hash on a "+
			"repeated setup run, so re-seeding is actually a reset")
}

// TestOnlyTheFixtureDeclaresTheSharedCredentials is the duplication guard. Both
// specs that need the test account used to restate it by hand, which is how the
// suite ended up with three copies of a stale password and one of them behind
// a Subsonic request that authenticates against the account password.
//
// Scoped to an email and a password on the *same line*, because the email alone
// is not duplication: admin.spec.ts legitimately asserts on a table row
// carrying the admin address, and permissions.spec.ts names both accounts in a
// comment. What must not happen is a second copy of the credential itself, and
// that is a per-line pairing - the shape both stale declarations had.
func TestOnlyTheFixtureDeclaresTheSharedCredentials(t *testing.T) {
	root := repoRoot(t)

	var offenders []string
	err := filepath.WalkDir(filepath.Join(root, "e2e"), func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".ts") {
			return err
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if rel == "e2e/fixtures/auth.fixture.ts" {
			return nil
		}

		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		for n, line := range strings.Split(string(raw), "\n") {
			hasEmail := strings.Contains(line, "e2e-test@netrunner.dev") ||
				strings.Contains(line, "e2e-admin@netrunner.dev")
			if hasEmail && strings.Contains(line, "password:") {
				offenders = append(offenders, fmt.Sprintf("%s:%d", rel, n+1))
			}
		}
		return nil
	})
	require.NoError(t, err)

	assert.Empty(t, offenders,
		"these lines restate the shared e2e credential instead of importing it "+
			"from e2e/fixtures/auth.fixture.ts: %v. A copy is how the suite ended "+
			"up sending a password the register endpoint refuses", offenders)
}
