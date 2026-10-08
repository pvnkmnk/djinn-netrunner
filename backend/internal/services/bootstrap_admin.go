package services

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/mail"
	"strings"
	"time"

	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"gorm.io/gorm"
)

// Bootstrap admin (DJI-542).
//
// Registration hardcodes every new account to the user role and nothing in the
// UI, the deploy scripts or the docs promotes anyone, so on a fresh install the
// entire admin surface is unreachable without editing the database by hand.
// On the playtest instance that had produced sixteen accounts and zero admins,
// ever.
//
// This is the fix: an operator configured in the environment is promoted to
// admin. Deliberately not first-user promotion - an anonymous browser reaching
// a fresh port would otherwise win admin - and not a documented manual SQL step,
// which is the thing this slice exists to remove.

// SystemActorID is the audit actor for actions no user performed. A bootstrap
// promotion is done by the operator through configuration, before or instead of
// any session, so there is no user to attribute it to.
const SystemActorID uint64 = 0

// BootstrapAdminAction is the audit action recorded when the bootstrap runs. It
// doubles as the marker that the bootstrap has already happened for an address,
// which is what makes promotion one-way: see BootstrapAdmin.
const BootstrapAdminAction = "user.bootstrap_admin_promoted"

// AdminRole is the role the bootstrap grants.
const AdminRole = "admin"

// BootstrapResult reports what a bootstrap attempt actually did, so the caller
// can log it honestly rather than claiming a promotion that did not happen.
type BootstrapResult struct {
	// Configured is false when no address was configured at all, which is the
	// default and is not worth logging.
	Configured bool
	// Email is the normalized address the attempt was for.
	Email string
	// Promoted is true only when this call changed a user's role.
	Promoted bool
	// AlreadyAdmin is true when the account was already an admin and no prior
	// bootstrap record existed, so the marker has now been written.
	AlreadyAdmin bool
	// AlreadyBootstrapped is true when a previous run already handled this
	// address, so the role was deliberately left alone.
	AlreadyBootstrapped bool
	// NoAccount is true when the configured address matches no user yet.
	NoAccount bool
	// NotEnrolled is true when the account exists but has never presented the
	// enrollment secret, so the boot left its role alone.
	NotEnrolled bool
}

// String renders a result as one log-friendly line.
func (r BootstrapResult) String() string {
	switch {
	case !r.Configured:
		return "not configured"
	case r.Promoted:
		return fmt.Sprintf("promoted %s to admin", r.Email)
	case r.AlreadyAdmin:
		return fmt.Sprintf("%s was already admin; recorded the bootstrap so later role changes are not reverted", r.Email)
	case r.AlreadyBootstrapped:
		return fmt.Sprintf("%s was already bootstrapped; leaving its role alone", r.Email)
	case r.NoAccount:
		return fmt.Sprintf("no account matches %s", r.Email)
	case r.NotEnrolled:
		return fmt.Sprintf("%s has never presented the enrollment secret; role left alone", r.Email)
	default:
		return fmt.Sprintf("%s: nothing to do", r.Email)
	}
}

// NormalizeBootstrapEmail applies the same normalization registration does, so
// whatever an operator typed into the environment is compared on the same terms
// the stored address uses. It parses the RFC 5322 form first, because
// "Ops Admin <admin@example.com>" is a legal thing to put in a config value and
// registration would have stored it as "admin@example.com".
//
// Input that does not parse is still normalized rather than rejected: the
// boot-time lookup uses the same string, and the operator is better served by a
// log line naming exactly what was searched for.
func NormalizeBootstrapEmail(email string) string {
	trimmed := strings.TrimSpace(email)
	if addr, err := mail.ParseAddress(trimmed); err == nil {
		trimmed = addr.Address
	}
	return strings.ToLower(strings.TrimSpace(trimmed))
}

// VerifyBootstrapSecret reports whether a presented enrollment code is the one
// the operator configured.
//
// Compared as fixed-size digests, so it leaks neither the secret through timing
// nor its length through an early length mismatch. An empty configuration never
// matches: the bootstrap being off must not read as "any code will do".
func VerifyBootstrapSecret(presented, configured string) bool {
	if configured == "" || presented == "" {
		return false
	}
	presentedSum := sha256.Sum256([]byte(presented))
	configuredSum := sha256.Sum256([]byte(configured))
	return subtle.ConstantTimeCompare(presentedSum[:], configuredSum[:]) == 1
}

// MarkBootstrapEnrolled records that an account presented the enrollment
// secret.
//
// This is the proof the boot-time bootstrap requires, and registration is the
// only code path that can write it, because registration is the only place a
// code is ever presented. A nil column means the account has proved nothing,
// and a boot must not promote it however the environment is configured.
func MarkBootstrapEnrolled(db *gorm.DB, userID uint64) error {
	now := time.Now()
	return db.Model(&database.User{}).
		Where("id = ?", userID).
		Update("bootstrap_enrolled_at", &now).Error
}

// BootstrapAdmin promotes the account matching email to admin, once.
//
// The promotion is deliberately one-way, which is what "a later manual role
// change is not reverted on the next boot" requires. Once an address has been
// bootstrapped, a marker recording that is written to the audit trail, and
// every later call for that address returns without touching the role. An
// operator who later demotes the account stays demoted across restarts.
//
// It is also safe to leave the variable set: repeated boots are no-ops.
//
// A boot can only promote an account that has proven it holds the enrollment
// secret, because a boot has no code to check. The proof is written by
// registration, which is the only place a stranger could present one, so the
// two triggers cannot disagree about who the operator meant.
//
// The audit marker is not just bookkeeping. It is the only record that this
// particular promotion already happened, so it is written even when the
// account turned out to be an admin already - otherwise the first boot that
// found an existing admin would leave no marker, and a later demote would be
// silently undone by the next restart.
func BootstrapAdmin(db *gorm.DB, email string) (BootstrapResult, error) {
	normalized := NormalizeBootstrapEmail(email)
	result := BootstrapResult{Configured: normalized != "", Email: normalized}
	if !result.Configured {
		return result, nil
	}

	err := db.Transaction(func(tx *gorm.DB) error {
		// Already handled this address? Then this is not our promotion to make,
		// and making it again would silently undo a deliberate demotion.
		var prior int64
		if err := tx.Model(&database.AuditLog{}).
			Where("action = ? AND target_type = ? AND target_id = ?",
				BootstrapAdminAction, "user", normalized).
			Count(&prior).Error; err != nil {
			return err
		}
		if prior > 0 {
			result.AlreadyBootstrapped = true
			return nil
		}

		var user database.User
		if err := tx.Where("LOWER(TRIM(email)) = ?", normalized).First(&user).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				result.NoAccount = true
				return nil
			}
			return err
		}

		// The boot has no enrollment code to compare, so the account itself has
		// to carry the proof that it presented one. Without this the address is a
		// bearer credential again: whoever registers it first stays a user at
		// registration and becomes an admin at the next restart.
		//
		// Checked BEFORE the marker is written, so an address nobody has proved
		// is not spent - the account can still enroll later and be promoted then.
		if user.BootstrapEnrolledAt == nil {
			result.NotEnrolled = true
			return nil
		}

		entry := database.AuditLog{
			Action:     BootstrapAdminAction,
			ActorID:    SystemActorID,
			TargetType: "user",
			TargetID:   normalized,
			Metadata:   bootstrapMetadata(user.Role),
			CreatedAt:  time.Now(),
		}
		if err := tx.Create(&entry).Error; err != nil {
			return err
		}

		if user.Role == AdminRole {
			result.AlreadyAdmin = true
			return nil
		}

		if err := tx.Model(&database.User{}).
			Where("id = ?", user.ID).
			Update("role", AdminRole).Error; err != nil {
			return err
		}
		result.Promoted = true
		return nil
	})
	return result, err
}

// LogBootstrapResult reports a bootstrap attempt at boot, naming the address it
// looked for. The "no account matches" case is a warning on purpose: it is the
// one outcome an operator has to act on, because their admin will not exist
// until the address they configured has registered.
func LogBootstrapResult(result BootstrapResult) {
	if !result.Configured {
		return
	}

	attrs := []any{"email", result.Email, "result", result.String()}
	switch {
	case result.Promoted:
		slog.Warn("Bootstrap admin promoted", attrs...)
	case result.NoAccount:
		slog.Warn("Bootstrap admin not applied: no account matches the configured address. "+
			"It is promoted when that address registers with the enrollment code.",
			attrs...)
	case result.AlreadyBootstrapped:
		slog.Info("Bootstrap admin already applied; role left unchanged", attrs...)
	case result.NotEnrolled:
		slog.Warn("Bootstrap admin not applied: that account has never presented the enrollment secret. "+
			"It is promoted the moment it registers with that code.", attrs...)
	default:
		slog.Info("Bootstrap admin", attrs...)
	}
}

func bootstrapMetadata(previousRole string) string {
	meta, err := json.Marshal(map[string]string{
		"previous_role": previousRole,
		"new_role":      AdminRole,
		"reason":        "BOOTSTRAP_ADMIN_EMAIL",
	})
	if err != nil {
		return ""
	}
	return string(meta)
}
