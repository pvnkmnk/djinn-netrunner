package config

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The policy this file pins is the one all three password-accepting routes
// share: registration, admin create-user, admin reset-password. It used to live
// inline in AuthHandler.Register and nowhere else, so the two admin routes
// enforced neither bound.
//
// Every case below asserts the boundary, not the happy path: a guard placed at
// 70 bytes, or one counting characters, would pass a single "too long is
// refused" test and let every one of these through.

const eAcute = "é"

func TestValidatePasswordPinsBothEndsOfTheBoundary(t *testing.T) {
	const min = 12
	ceiling := BcryptMaxPasswordBytes

	// 40 characters, 80 bytes: clears the floor comfortably and is under 72
	// CHARACTERS, so a rune-counted ceiling would wave it through.
	multiByte := strings.Repeat(eAcute, 40)
	require.Equal(t, 40, utf8.RuneCountInString(multiByte), "forty characters")
	require.Equal(t, 80, len(multiByte), "eighty bytes -- under 72 characters, over the ceiling")

	// Asserted here because it is not obvious: an eight-word passphrase is
	// 57 bytes, comfortably UNDER the ceiling, and a test that used one passed
	// 201 where it meant to prove a refusal. A boundary fixture that does not
	// measure itself is decoration.
	passphrase := "correct horse battery staple vanilla harbor candle drawer lonely summit kitten"
	require.Greater(t, len(passphrase), ceiling,
		"the passphrase fixture must actually exceed the ceiling")

	tests := []struct {
		name       string
		password   string
		minLength  int
		wantBound  PasswordBound
		wantReject bool
	}{
		{"one character", "x", min, PasswordBoundFloor, true},
		{"one below the floor", strings.Repeat("a", min-1), min, PasswordBoundFloor, true},
		{"exactly at the floor", strings.Repeat("a", min), min, "", false},
		{"exactly at the ceiling in bytes", strings.Repeat("a", ceiling), min, "", false},
		{"one byte over the ceiling", strings.Repeat("a", ceiling+1), min, PasswordBoundCeiling, true},
		{"well past the ceiling", strings.Repeat("a", 100), min, PasswordBoundCeiling, true},
		{"at the ceiling in multi-byte text", strings.Repeat(eAcute, ceiling/2), min, "", false},
		{"under 72 characters but over 72 bytes", multiByte, min, PasswordBoundCeiling, true},
		{"a long passphrase in ASCII", passphrase, min, PasswordBoundCeiling, true},
		{"empty", "", min, PasswordBoundFloor, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ValidatePassword(tc.password, tc.minLength)
			if !tc.wantReject {
				assert.Nil(t, got, "%d bytes / %d runes must be accepted",
					len(tc.password), utf8.RuneCountInString(tc.password))
				return
			}
			require.NotNil(t, got, "must be refused")
			assert.Equal(t, tc.wantBound, got.Bound)
		})
	}
}

// The floor and the ceiling are counted in DIFFERENT units. A password can
// clear one and fail the other, so the violation has to name which bound was
// hit and in which unit -- otherwise a person who satisfied the rule the form
// states is refused with nothing to act on.
func TestValidatePasswordSaysWhichBoundAndInWhichUnit(t *testing.T) {
	const min = 12

	t.Run("the floor names characters and counts runes", func(t *testing.T) {
		// 11 e-acutes are 22 BYTES -- over half the byte ceiling -- and must
		// still fail the floor, because the floor counts characters.
		v := ValidatePassword(strings.Repeat(eAcute, min-1), min)
		require.NotNil(t, v)
		assert.Equal(t, PasswordBoundFloor, v.Bound)

		fields := v.JSON()
		assert.Contains(t, fields["error"], "at least 12 characters")
		assert.Equal(t, min, fields["minLength"])
		assert.Equal(t, min-1, fields["passwordLength"])

		// The byte count must NOT appear here: it was never the failing bound.
		_, leaksBytes := fields["passwordBytes"]
		assert.False(t, leaksBytes, "the floor is not about bytes")
	})

	t.Run("the ceiling names bytes and counts bytes", func(t *testing.T) {
		v := ValidatePassword(strings.Repeat(eAcute, 40), min)
		require.NotNil(t, v)
		assert.Equal(t, PasswordBoundCeiling, v.Bound)

		message := v.JSON()["error"].(string)
		assert.Contains(t, message, "72 bytes")
		assert.Contains(t, message, "not characters",
			"the refusal must say the limit is not counted in characters")

		fields := v.JSON()
		assert.Equal(t, BcryptMaxPasswordBytes, fields["maxBytes"])
		assert.Equal(t, 80, fields["passwordBytes"])

		_, leaksLength := fields["passwordLength"]
		assert.False(t, leaksLength, "the ceiling is not about character count")
	})
}

// A ceiling message that only said "too long" would satisfy any assertion that
// merely checks for a refusal, and leaves a user with nothing to act on.
func TestValidatePasswordCeilingMessageIsActionable(t *testing.T) {
	v := ValidatePassword(strings.Repeat("a", BcryptMaxPasswordBytes+1), 12)
	require.NotNil(t, v)

	message := v.JSON()["error"].(string)
	assert.Contains(t, message, "at most 72 bytes")
	assert.Contains(t, message, "this one is 73 bytes", "it must report the actual size")
	assert.NotContains(t, message, "too long",
		"'too long' names no unit and no number")
}

// The floor is configurable but the ceiling is bcrypt's, so a floor above the
// ceiling must fail the floor for every password rather than producing a range
// nothing satisfies.
func TestValidatePasswordFloorAboveCeilingRefusesEverything(t *testing.T) {
	for _, length := range []int{0, 12, 72, 100} {
		password := strings.Repeat("a", length)
		v := ValidatePassword(password, 200)
		require.NotNil(t, v,
			"a floor of 200 leaves no satisfiable password (length %d)", length)
		assert.Equal(t, PasswordBoundFloor, v.Bound)
	}
}

// A handler with no configured policy must still enforce one, or the two admin
// routes would silently accept anything when MIN_PASSWORD_LENGTH is unset.
func TestValidatePasswordZeroMinMeansTheDefault(t *testing.T) {
	assert.NotNil(t, ValidatePassword(strings.Repeat("a", DefaultMinPasswordLength-1), 0))
	assert.NotNil(t, ValidatePassword(strings.Repeat("a", DefaultMinPasswordLength-1), -5))
	assert.Nil(t, ValidatePassword(strings.Repeat("a", DefaultMinPasswordLength), 0))
}

// The JSON body is the API contract: app.js reads `error`, and clients read
// the numeric fields. A key rename here is a breaking change, so pin the names.
func TestValidatePasswordJSONKeysAreTheContract(t *testing.T) {
	floor := ValidatePassword("x", 12).JSON()
	for _, key := range []string{"error", "minLength", "passwordLength"} {
		assert.Contains(t, floor, key)
	}
	assert.Len(t, floor, 3, "a floor violation carries exactly three fields")

	ceiling := ValidatePassword(strings.Repeat("a", 100), 12).JSON()
	for _, key := range []string{"error", "maxBytes", "passwordBytes"} {
		assert.Contains(t, ceiling, key)
	}
	assert.Len(t, ceiling, 3, "a ceiling violation carries exactly three fields")
}
