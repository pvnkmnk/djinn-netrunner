package services

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// DJI-542. The playtest instance had sixteen accounts and zero admins, ever:
// registration hardcodes the user role, so the admin surface was unreachable
// without editing the database by hand.
//
// The criterion that needs the most care is "a later manual role change is not
// reverted on the next boot". That is why the promotion writes an audit marker
// rather than simply re-applying the role: a naive implementation re-promotes
// on every boot forever, which quietly undoes an operator's demotion.

func bootstrapTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/bootstrap.db"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.Migrate(db))

	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { sqlDB.Close() })

	return db
}

// makeUser creates the account the operator intends: one that has proved it
// holds the enrollment secret, which is what the boot promotion now requires.
// Every test that expects a promotion builds its fixture here, so the
// dangerous case has to be asked for by name (makeUnprovenUser).
func makeUser(t *testing.T, db *gorm.DB, email, role string) database.User {
	t.Helper()
	u := makeUnprovenUser(t, db, email, role)
	require.NoError(t, MarkBootstrapEnrolled(db, u.ID))
	return u
}

// makeUnprovenUser creates what any stranger's registration produces: an
// account with no record of ever presenting the enrollment secret.
func makeUnprovenUser(t *testing.T, db *gorm.DB, email, role string) database.User {
	t.Helper()
	u := database.User{
		ID:           uint64(uuid.New()[0]) | 1,
		Email:        email,
		PasswordHash: "x",
		Role:         role,
	}
	require.NoError(t, db.Create(&u).Error)
	return u
}

func roleOf(t *testing.T, db *gorm.DB, email string) string {
	t.Helper()
	var u database.User
	require.NoError(t, db.Where("LOWER(TRIM(email)) = ?", email).First(&u).Error)
	return u.Role
}

func markerCount(t *testing.T, db *gorm.DB, email string) int64 {
	t.Helper()
	var n int64
	require.NoError(t, db.Model(&database.AuditLog{}).
		Where("action = ? AND target_id = ?", BootstrapAdminAction, NormalizeBootstrapEmail(email)).
		Count(&n).Error)
	return n
}

// Criterion: "Configuring a bootstrap admin email in the environment promotes
// the matching account to admin at startup."
func TestBootstrapAdmin_PromotesMatchingAccount(t *testing.T) {
	db := bootstrapTestDB(t)
	makeUser(t, db, "operator@example.com", "user")

	result, err := BootstrapAdmin(db, "operator@example.com")
	require.NoError(t, err)

	assert.True(t, result.Promoted, "the matching account should have been promoted")
	assert.False(t, result.AlreadyAdmin)
	assert.Equal(t, AdminRole, roleOf(t, db, "operator@example.com"))
	assert.Contains(t, result.String(), "promoted")
}

func TestBootstrapAdmin_NormalizesTheConfiguredAddress(t *testing.T) {
	db := bootstrapTestDB(t)
	makeUser(t, db, "operator@example.com", "user")

	// The environment is hand-edited, so casing and padding are realistic.
	result, err := BootstrapAdmin(db, "  Operator@Example.COM  ")
	require.NoError(t, err)

	assert.True(t, result.Promoted)
	assert.Equal(t, AdminRole, roleOf(t, db, "operator@example.com"))
}

// A display name is a legal thing to paste into a config value, and
// registration would have stored the bare address. The bootstrap has to reach
// the same account that registration created, or the operator is left with an
// admin surface nobody can reach.
func TestBootstrapAdmin_NormalizesAnRFC5322DisplayName(t *testing.T) {
	db := bootstrapTestDB(t)
	makeUser(t, db, "operator@example.com", "user")

	result, err := BootstrapAdmin(db, "Ops Admin <Operator@Example.com>")
	require.NoError(t, err)

	assert.True(t, result.Promoted)
	assert.Equal(t, "operator@example.com", result.Email)
	assert.Equal(t, AdminRole, roleOf(t, db, "operator@example.com"))
}

// Criterion: "Promotion is idempotent across restarts: an account already at
// admin is left alone, and a later manual role change is not reverted on the
// next boot."
func TestBootstrapAdmin_SecondBootIsANoOp(t *testing.T) {
	db := bootstrapTestDB(t)
	makeUser(t, db, "operator@example.com", "user")

	first, err := BootstrapAdmin(db, "operator@example.com")
	require.NoError(t, err)
	require.True(t, first.Promoted)

	second, err := BootstrapAdmin(db, "operator@example.com")
	require.NoError(t, err)

	assert.False(t, second.Promoted)
	assert.True(t, second.AlreadyBootstrapped)
	assert.Equal(t, AdminRole, roleOf(t, db, "operator@example.com"))
	assert.Equal(t, int64(1), markerCount(t, db, "operator@example.com"),
		"the marker must be written once, not on every boot")
}

// This is the half of idempotence that is easy to get wrong: the operator
// demotes the account, restarts, and the bootstrap must not undo it.
func TestBootstrapAdmin_DoesNotRevertALaterManualDemotion(t *testing.T) {
	db := bootstrapTestDB(t)
	makeUser(t, db, "operator@example.com", "user")

	_, err := BootstrapAdmin(db, "operator@example.com")
	require.NoError(t, err)
	require.Equal(t, AdminRole, roleOf(t, db, "operator@example.com"))

	// An operator deliberately takes the admin role away.
	require.NoError(t, db.Model(&database.User{}).
		Where("email = ?", "operator@example.com").
		Update("role", "user").Error)

	result, err := BootstrapAdmin(db, "operator@example.com")
	require.NoError(t, err)

	assert.True(t, result.AlreadyBootstrapped)
	assert.Equal(t, "user", roleOf(t, db, "operator@example.com"),
		"a manual role change must survive the next boot")
}

// Criterion: "If no account matches, nothing is promoted and the boot log says
// so, naming the address it looked for."
func TestBootstrapAdmin_UnknownAccountPromotesNothingAndNamesTheAddress(t *testing.T) {
	db := bootstrapTestDB(t)
	makeUser(t, db, "someone-else@example.com", "user")

	result, err := BootstrapAdmin(db, "later@example.com")
	require.NoError(t, err)

	assert.True(t, result.NoAccount)
	assert.False(t, result.Promoted)
	assert.Equal(t, "later@example.com", result.Email)
	assert.Contains(t, result.String(), "later@example.com",
		"the operator must be told which address was looked for")
	assert.Equal(t, "user", roleOf(t, db, "someone-else@example.com"))
	assert.Equal(t, int64(0), markerCount(t, db, "later@example.com"),
		"a failed attempt must not burn the marker, or the account would never be promoted")
}

// Criterion: "Configuring a bootstrap email for an account that does not exist
// yet promotes it once that account registers." The other half is that the
// pending attempt is remembered, and completes when the account appears.
func TestBootstrapAdmin_PromotesAnAccountThatAppearsLater(t *testing.T) {
	db := bootstrapTestDB(t)

	pending, err := BootstrapAdmin(db, "later@example.com")
	require.NoError(t, err)
	require.True(t, pending.NoAccount)

	makeUser(t, db, "later@example.com", "user")

	result, err := BootstrapAdmin(db, "later@example.com")
	require.NoError(t, err)

	assert.True(t, result.Promoted)
	assert.Equal(t, AdminRole, roleOf(t, db, "later@example.com"))
}

// The variable is optional, and an unset value must be completely silent.
func TestBootstrapAdmin_UnsetDoesNothing(t *testing.T) {
	db := bootstrapTestDB(t)
	makeUser(t, db, "operator@example.com", "user")

	for _, empty := range []string{"", "   "} {
		result, err := BootstrapAdmin(db, empty)
		require.NoError(t, err)
		assert.False(t, result.Configured)
		assert.False(t, result.Promoted)
		assert.Equal(t, "not configured", result.String())
	}

	assert.Equal(t, "user", roleOf(t, db, "operator@example.com"))
	assert.Equal(t, int64(0), markerCount(t, db, ""))
}

// An account that is already an admin still gets the marker, so that a later
// demotion is not undone by the following restart.
func TestBootstrapAdmin_ExistingAdminGetsTheMarkerWithoutARoleChange(t *testing.T) {
	db := bootstrapTestDB(t)
	makeUser(t, db, "root@example.com", AdminRole)

	result, err := BootstrapAdmin(db, "root@example.com")
	require.NoError(t, err)

	assert.True(t, result.AlreadyAdmin)
	assert.False(t, result.Promoted)
	assert.Equal(t, int64(1), markerCount(t, db, "root@example.com"))

	require.NoError(t, db.Model(&database.User{}).
		Where("email = ?", "root@example.com").
		Update("role", "user").Error)

	second, err := BootstrapAdmin(db, "root@example.com")
	require.NoError(t, err)
	assert.True(t, second.AlreadyBootstrapped)
	assert.Equal(t, "user", roleOf(t, db, "root@example.com"))
}

// The promotion must be recorded, so an operator can see who became an admin
// and why without reading the database by hand.
func TestBootstrapAdmin_RecordsAnAuditEntry(t *testing.T) {
	db := bootstrapTestDB(t)
	makeUser(t, db, "operator@example.com", "user")

	_, err := BootstrapAdmin(db, "operator@example.com")
	require.NoError(t, err)

	var entry database.AuditLog
	require.NoError(t, db.Where("action = ?", BootstrapAdminAction).First(&entry).Error)
	assert.Equal(t, SystemActorID, entry.ActorID)
	assert.Equal(t, "user", entry.TargetType)
	assert.Equal(t, "operator@example.com", entry.TargetID)
	assert.Contains(t, entry.Metadata, "BOOTSTRAP_ADMIN_EMAIL")
	assert.Contains(t, entry.Metadata, `"previous_role":"user"`)
}

// A different address is a different bootstrap, and is not blocked by the
// marker left for the first one.
func TestBootstrapAdmin_ASecondAddressIsNotBlockedByTheFirst(t *testing.T) {
	db := bootstrapTestDB(t)
	makeUser(t, db, "first@example.com", "user")
	makeUser(t, db, "second@example.com", "user")

	_, err := BootstrapAdmin(db, "first@example.com")
	require.NoError(t, err)

	result, err := BootstrapAdmin(db, "second@example.com")
	require.NoError(t, err)

	assert.True(t, result.Promoted)
	assert.Equal(t, AdminRole, roleOf(t, db, "second@example.com"))
}

// Only the exact configured address is affected. A prefix or a near-miss must
// not be promoted.
func TestBootstrapAdmin_OnlyTheExactAddressIsPromoted(t *testing.T) {
	db := bootstrapTestDB(t)
	makeUser(t, db, "operator@example.com", "user")
	makeUser(t, db, "operator@example.com.evil.test", "user")

	_, err := BootstrapAdmin(db, "operator@example.com")
	require.NoError(t, err)

	assert.Equal(t, AdminRole, roleOf(t, db, "operator@example.com"))
	assert.Equal(t, "user", roleOf(t, db, "operator@example.com.evil.test"))
}

// DJI-641. An account at the configured address is not the operator's account.
// The registration trigger refuses one that never presented the code, and this
// is the half that a live probe caught still open: the account stayed `user` at
// registration and became `admin` at the next restart, because the boot
// compared an address and nothing else. The hole was deferred, not closed.
func TestBootstrapAdmin_DoesNotPromoteAnAccountThatNeverProvedTheSecret(t *testing.T) {
	db := bootstrapTestDB(t)
	makeUnprovenUser(t, db, "attacker@example.com", "user")

	result, err := BootstrapAdmin(db, "attacker@example.com")
	require.NoError(t, err)

	assert.True(t, result.NotEnrolled,
		"the boot must report that the account never proved the secret")
	assert.False(t, result.Promoted)
	assert.Equal(t, "user", roleOf(t, db, "attacker@example.com"),
		"knowing nothing but the address must not survive a restart")
	assert.Equal(t, int64(0), markerCount(t, db, "attacker@example.com"),
		"an unproven account must not spend the address, or the real admin could never be promoted")
}

// The other direction: presenting the code is what makes the boot willing, so a
// registration whose promotion failed (a transient write error, say) still
// completes on the next start.
func TestBootstrapAdmin_PromotesAnAccountThatProvedTheSecret(t *testing.T) {
	db := bootstrapTestDB(t)
	makeUser(t, db, "operator@example.com", "user")

	result, err := BootstrapAdmin(db, "operator@example.com")
	require.NoError(t, err)

	assert.True(t, result.Promoted)
	assert.False(t, result.NotEnrolled)
	assert.Equal(t, AdminRole, roleOf(t, db, "operator@example.com"))
}

// DJI-641. The enrollment secret is the operator's proof that the promotion was
// meant for that person, so an unconfigured secret has to mean "nobody is
// promoted" rather than "any code will do". That difference is the whole
// finding: the open-door reading is what handed admin to whoever registered the
// published address first.
func TestVerifyBootstrapSecret(t *testing.T) {
	const secret = "correct-horse-battery"

	cases := []struct {
		name       string
		presented  string
		configured string
		want       bool
	}{
		{"the configured secret", secret, secret, true},
		{"a same-length secret that differs by one rune", "correct-horse-batterX", secret, false},
		{"a prefix is not the secret", "correct-horse", secret, false},
		{"the secret with a trailing character", secret + "!", secret, false},
		{"case matters", "Correct-Horse-Battery", secret, false},
		{"a subtle difference in the middle", "correct-horse-bsttery", secret, false},
		{"nothing configured refuses any code", secret, "", false},
		{"nothing configured and nothing presented", "", "", false},
		{"an empty code is refused", "", secret, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, VerifyBootstrapSecret(tc.presented, tc.configured))
		})
	}
}

// The comparison must not leak the configured secret's length: a configuration
// of one length and a presentation of another are refused the same way.
func TestVerifyBootstrapSecret_DoesNotCompareByLengthAlone(t *testing.T) {
	assert.False(t, VerifyBootstrapSecret("", "a"))
	assert.False(t, VerifyBootstrapSecret("aaaaaaaaaaaaaaaaaa", "a"))
	assert.False(t, VerifyBootstrapSecret("a", "aaaaaaaaaaaaaaaaaa"))
	assert.True(t, VerifyBootstrapSecret("a", "a"))
}
