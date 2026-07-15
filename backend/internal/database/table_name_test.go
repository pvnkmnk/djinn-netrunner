package database

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestJob_TableName(t *testing.T) {
	j := Job{}
	assert.Equal(t, "jobs", j.TableName())
}

func TestJobItem_TableName(t *testing.T) {
	ji := JobItem{}
	assert.Equal(t, "jobitems", ji.TableName())
}

func TestAcquisition_TableName(t *testing.T) {
	a := Acquisition{}
	assert.Equal(t, "acquisitions", a.TableName())
}

func TestUser_TableName(t *testing.T) {
	u := User{}
	assert.Equal(t, "users", u.TableName())
}

func TestSession_TableName(t *testing.T) {
	s := Session{}
	assert.Equal(t, "sessions", s.TableName())
}

func TestQualityProfile_TableName(t *testing.T) {
	qp := QualityProfile{}
	assert.Equal(t, "quality_profiles", qp.TableName())
}

func TestMonitoredArtist_TableName(t *testing.T) {
	ma := MonitoredArtist{}
	assert.Equal(t, "monitored_artists", ma.TableName())
}

func TestTrackedRelease_TableName(t *testing.T) {
	tr := TrackedRelease{}
	assert.Equal(t, "tracked_releases", tr.TableName())
}

func TestLock_TableName(t *testing.T) {
	l := Lock{}
	assert.Equal(t, "locks", l.TableName())
}

func TestSetting_TableName(t *testing.T) {
	s := Setting{}
	assert.Equal(t, "settings", s.TableName())
}

func TestPeerReputation_TableName(t *testing.T) {
	pr := PeerReputation{}
	assert.Equal(t, "peer_reputations", pr.TableName())
}

func TestAuditLog_TableName(t *testing.T) {
	al := AuditLog{}
	assert.Equal(t, "audit_logs", al.TableName())
}

func TestPlaylist_TableName(t *testing.T) {
	p := Playlist{}
	assert.Equal(t, "playlists", p.TableName())
}

func TestPlaylistTrack_TableName(t *testing.T) {
	pt := PlaylistTrack{}
	assert.Equal(t, "playlist_tracks", pt.TableName())
}
