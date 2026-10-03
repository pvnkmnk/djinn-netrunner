package config

import (
	"fmt"
	"unicode/utf8"
)

// The password policy, in one place.
//
// It lived inline in AuthHandler.Register and nowhere else, which is how the
// two admin routes came to enforce NEITHER bound: POST /api/admin/users and
// POST /api/admin/users/:id/reset-password both called bcrypt directly, so an
// over-72-byte password returned 500 and a one-character password was accepted
// -- on the same server where registration refused both. An administrator is
// not a different class of person: a policy that only applies to strangers is
// not a policy.
//
// Both bounds live here, beside the constants they are derived from.

// DefaultMinPasswordLength is the floor used when nothing is configured.
const DefaultMinPasswordLength = 12

// PasswordBound names which end of the policy a password failed, so a caller
// can tell the two apart. They are counted in DIFFERENT UNITS, so "too short"
// and "too long" are not two sides of one number.
type PasswordBound string

const (
	// PasswordBoundFloor: fewer characters than the configured minimum.
	PasswordBoundFloor PasswordBound = "floor"
	// PasswordBoundCeiling: more BYTES than bcrypt accepts.
	PasswordBoundCeiling PasswordBound = "ceiling"
)

// PasswordViolation describes one failed bound, in the terms the person who
// typed the password needs. Returned rather than formatted by the caller so
// all three routes answer identically.
type PasswordViolation struct {
	Bound   PasswordBound
	Message string

	// Floor fields, set when Bound is PasswordBoundFloor.
	MinLength      int
	PasswordLength int

	// Ceiling fields, set when Bound is PasswordBoundCeiling.
	MaxBytes      int
	PasswordBytes int
}

// JSON renders the 400 body. The field names are part of the API contract --
// `error` is read by ops/web/static/js/app.js, and the numeric fields let a
// client re-check the request without parsing prose. They are the same names
// registration has always returned, so nothing downstream has to learn a
// second spelling.
func (v *PasswordViolation) JSON() map[string]any {
	fields := map[string]any{"error": v.Message}
	switch v.Bound {
	case PasswordBoundFloor:
		fields["minLength"] = v.MinLength
		fields["passwordLength"] = v.PasswordLength
	case PasswordBoundCeiling:
		fields["maxBytes"] = v.MaxBytes
		fields["passwordBytes"] = v.PasswordBytes
	}
	return fields
}

// ValidatePassword applies the whole policy and returns nil when the password
// satisfies it. minLength of zero or less means DefaultMinPasswordLength, so a
// handler with no configured policy still enforces one.
//
// The floor counts RUNES and the ceiling counts BYTES. They are not two sides
// of one scale: 40 e-acutes clear a 12-character floor and are 80 bytes, so a
// ceiling counted in characters waves them through and bcrypt still refuses
// them. That is why the ceiling message names the unit.
//
// The floor is checked first because it is the one a person can fix by typing
// more, and the ceiling message is the confusing one; a caller that wants only
// one bound at a time should not be tempted to reorder this.
func ValidatePassword(password string, minLength int) *PasswordViolation {
	if minLength < 1 {
		minLength = DefaultMinPasswordLength
	}

	// Count runes, not bytes: a 12-character password in any script must not be
	// rejected because its encoding is longer than 12 bytes.
	if length := utf8.RuneCountInString(password); length < minLength {
		return &PasswordViolation{
			Bound:          PasswordBoundFloor,
			Message:        fmt.Sprintf("password must be at least %d characters", minLength),
			MinLength:      minLength,
			PasswordLength: length,
		}
	}

	// bcrypt accepts at most 72 BYTES and treats a longer input as an error,
	// not a truncation. Without this guard the password reaches
	// bcrypt.GenerateFromPassword and comes back as a 500 -- a server fault for
	// a request a person made perfectly reasonably, and unreachable from any
	// UI hint, because a browser states no length limit on a password field.
	if size := len([]byte(password)); size > BcryptMaxPasswordBytes {
		return &PasswordViolation{
			Bound: PasswordBoundCeiling,
			Message: fmt.Sprintf(
				"password must be at most %d bytes; this one is %d bytes. This limit counts bytes, not characters, so a passphrase using accented or non-Latin characters can reach it in fewer characters",
				BcryptMaxPasswordBytes, size),
			MaxBytes:      BcryptMaxPasswordBytes,
			PasswordBytes: size,
		}
	}

	return nil
}
