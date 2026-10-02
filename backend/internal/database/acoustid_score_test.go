package database

import (
	"encoding/json"
	"testing"

	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/stretchr/testify/require"
)

// score returns a pointer to an int, for the nullable AcoustIDScore column.
func score(n int) *int { return &n }

// DJI-545, AC4: "a track whose audio cannot be fingerprinted is recorded as
// unscored and is distinguishable from a track that scored zero".
//
// The column used to be a plain int, so both states were the value 0 and the
// distinction did not exist anywhere - not in the database, not in the JSON the
// CLI and MCP surface emit. Because no fpcalc had ever been present in the
// image, every one of those rows was in the "never scored" state while
// reporting a score.
func TestAcquisition_UnscoredIsDistinguishableFromScoredZero(t *testing.T) {
	db, err := Connect(&config.Config{DatabaseURL: ":memory:"})
	require.NoError(t, err)
	require.NoError(t, Migrate(db))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { sqlDB.Close() })

	never := Acquisition{Artist: "a", TrackTitle: "never", FinalPath: "/m/never.mp3"}
	zero := Acquisition{Artist: "a", TrackTitle: "zero", FinalPath: "/m/zero.mp3", AcoustIDScore: score(0)}
	hit := Acquisition{Artist: "a", TrackTitle: "hit", FinalPath: "/m/hit.mp3", AcoustIDScore: score(87)}
	for _, acq := range []*Acquisition{&never, &zero, &hit} {
		require.NoError(t, db.Create(acq).Error)
	}

	// Read back through the ORM, which is what every caller goes through.
	var got Acquisition
	require.NoError(t, db.Where("track_title = ?", "never").First(&got).Error)
	require.Nil(t, got.AcoustIDScore, "a track that was never scored must not come back holding a zero")

	got = Acquisition{}
	require.NoError(t, db.Where("track_title = ?", "zero").First(&got).Error)
	require.NotNil(t, got.AcoustIDScore, "a genuine scored zero must survive the round trip")
	require.Equal(t, 0, *got.AcoustIDScore)

	got = Acquisition{}
	require.NoError(t, db.Where("track_title = ?", "hit").First(&got).Error)
	require.NotNil(t, got.AcoustIDScore)
	require.Equal(t, 87, *got.AcoustIDScore)

	// And through the JSON an API caller receives. `0` and `null` are the whole
	// of the contract on the wire, so assert on the wire shape.
	for _, tc := range []struct {
		title string
		want  any
	}{
		{"never", nil},
		{"zero", float64(0)},
		{"hit", float64(87)},
	} {
		var row Acquisition
		require.NoError(t, db.Where("track_title = ?", tc.title).First(&row).Error)
		raw, err := json.Marshal(row)
		require.NoError(t, err)
		var fields map[string]any
		require.NoError(t, json.Unmarshal(raw, &fields))
		require.Contains(t, fields, "AcoustIDScore", "the API must keep exposing the score field")
		require.Equal(t, tc.want, fields["AcoustIDScore"],
			"unscored and scored-zero must be different values on the wire")
	}
}

// The rows already in a deployed database are the other half of AC4. Every one
// of them reads a score of zero and none was ever scored, because the binary
// that computes them was not in the image. The backfill has to say so, and it
// has to stay said: a zero written afterwards by a working lookup is a real
// measurement and must not be nulled by the next boot.
func TestMigrate_BackfillsNeverScoredAcoustIDsAndLeavesLaterOnesAlone(t *testing.T) {
	db, err := Connect(&config.Config{DatabaseURL: ":memory:"})
	require.NoError(t, err)
	require.NoError(t, Migrate(db))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { sqlDB.Close() })

	legacy := Acquisition{Artist: "a", TrackTitle: "legacy", FinalPath: "/m/legacy.mp3", AcoustIDScore: score(0)}
	real := Acquisition{Artist: "a", TrackTitle: "real", FinalPath: "/m/real.mp3", AcoustIDScore: score(87)}
	require.NoError(t, db.Create(&legacy).Error)
	require.NoError(t, db.Create(&real).Error)

	// The marker is written by the first Migrate above, so the backfill is
	// already behind us. Rewind it to stand in for a database that predates it.
	require.NoError(t, db.Exec(`DELETE FROM settings WHERE key = ?`, "acoustid_unscored_backfill_v1").Error)
	require.NoError(t, Migrate(db))

	var got Acquisition
	require.NoError(t, db.Where("track_title = ?", "legacy").First(&got).Error)
	require.Nil(t, got.AcoustIDScore, "a score written before fpcalc existed is not a measurement")

	got = Acquisition{}
	require.NoError(t, db.Where("track_title = ?", "real").First(&got).Error)
	require.NotNil(t, got.AcoustIDScore, "a real confidence must never be backfilled away")
	require.Equal(t, 87, *got.AcoustIDScore)

	// The backfill runs once. A zero recorded by a working lookup afterwards is
	// indistinguishable in the column from a legacy one, and only the marker
	// separates them - so if the backfill were not idempotent it would silently
	// erase every low-confidence match on the next restart.
	after := Acquisition{Artist: "a", TrackTitle: "after", FinalPath: "/m/after.mp3", AcoustIDScore: score(0)}
	require.NoError(t, db.Create(&after).Error)
	require.NoError(t, Migrate(db))

	got = Acquisition{}
	require.NoError(t, db.Where("track_title = ?", "after").First(&got).Error)
	require.NotNil(t, got.AcoustIDScore,
		"the backfill must not run twice: a post-fix scored zero is a real measurement")
	require.Equal(t, 0, *got.AcoustIDScore)
}
