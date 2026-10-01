package database

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The list is the single source of truth for the Jobs page type filter. A type
// missing from it cannot be filtered to at all, which is how release_monitor -
// the scheduler's own job type - became invisible on the Jobs page.
func TestJobTypesAreUniqueAndLabelled(t *testing.T) {
	require.NotEmpty(t, JobTypes)

	seen := map[string]bool{}
	for _, jt := range JobTypes {
		assert.NotEmpty(t, jt.Value, "every entry needs a stored value")
		assert.NotEmpty(t, jt.Label, "%s needs a label a person can read", jt.Value)
		assert.False(t, seen[jt.Value], "%s is listed twice", jt.Value)
		seen[jt.Value] = true
	}
}

// "All Types" is the empty filter, not a job type. If it appeared here it
// would render as an option that matches nothing.
func TestJobTypesDoesNotContainTheEmptyFilter(t *testing.T) {
	for _, jt := range JobTypes {
		assert.NotEmpty(t, jt.Value, "an empty value would render an option matching nothing")
	}
	assert.False(t, IsKnownJobType(""), "the empty filter is not a job type")
}

// These are the types the app creates, measured on the playtest instance.
// Pinned here so deleting a row from the list is a visible test failure rather
// than a filter that quietly stops offering something.
func TestJobTypesCoverEveryTypeTheAppCreates(t *testing.T) {
	expected := []string{
		"acquisition", "artist_scan", "scan", "sync",
		"enrich", "prune", "release_monitor", "index_refresh",
	}
	for _, want := range expected {
		assert.True(t, IsKnownJobType(want),
			"%s is created by the app but missing from the filter list", want)
	}
	assert.Len(t, JobTypes, len(expected),
		"the filter list grew or shrank; update this test deliberately")
}

func TestJobTypeValuesMirrorsJobTypes(t *testing.T) {
	values := JobTypeValues()
	require.Len(t, values, len(JobTypes))
	for i, jt := range JobTypes {
		assert.Equal(t, jt.Value, values[i])
	}
}

// An unknown value is not an error - rows from a newer build can outlive a
// downgrade - but IsKnownJobType must say so honestly rather than guessing.
func TestIsKnownJobTypeRejectsUnknownValues(t *testing.T) {
	assert.False(t, IsKnownJobType("nonsense"))
	assert.False(t, IsKnownJobType("acquisition "), "no silent trimming")
	assert.False(t, IsKnownJobType("ACQUISITION"), "stored values are lowercase")
}
