package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestProfileListColumns_Constant verifies the profileListColumns constant
// is defined and includes expected fields while excluding wildcards.
func TestProfileListColumns_Constant(t *testing.T) {
	require.NotEmpty(t, profileListColumns, "profileListColumns constant must be defined")
	assert.Contains(t, profileListColumns, "id")
	assert.Contains(t, profileListColumns, "name")
	assert.Contains(t, profileListColumns, "is_default")
	assert.Contains(t, profileListColumns, "description")
	assert.Contains(t, profileListColumns, "prefer_lossless")
	assert.Contains(t, profileListColumns, "allowed_formats")
	assert.Contains(t, profileListColumns, "min_bitrate")
	assert.Contains(t, profileListColumns, "cover_art_sources")
	assert.NotContains(t, profileListColumns, "*", "must not use wildcard selection")
}

// TestTrackBrowseColumns_Constant verifies the trackBrowseColumns constant
// is defined and intentionally excludes large/sensitive fields.
func TestTrackBrowseColumns_Constant(t *testing.T) {
	require.NotEmpty(t, trackBrowseColumns, "trackBrowseColumns constant must be defined")
	assert.Contains(t, trackBrowseColumns, "id")
	assert.Contains(t, trackBrowseColumns, "title")
	assert.Contains(t, trackBrowseColumns, "artist")
	assert.Contains(t, trackBrowseColumns, "album")

	// Verify large fields are intentionally excluded
	assert.NotContains(t, trackBrowseColumns, "enrichment_provenance",
		"should exclude large EnrichmentProvenance field")
	assert.NotContains(t, trackBrowseColumns, "fingerprint",
		"should exclude large Fingerprint field")
	assert.NotContains(t, trackBrowseColumns, "*", "must not use wildcard")
}

// TestLibraryListColumns_Constant verifies the library list column constant.
func TestLibraryListColumns_Constant(t *testing.T) {
	require.NotEmpty(t, libraryListColumns, "libraryListColumns constant must be defined")
	assert.Contains(t, libraryListColumns, "id")
	assert.Contains(t, libraryListColumns, "name")
	assert.Contains(t, libraryListColumns, "path")
	assert.NotContains(t, libraryListColumns, "*", "must not use wildcard")
}
