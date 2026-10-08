package services

import (
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"gorm.io/gorm"
)

// ErrArtistNotOwned is returned when the row exists but belongs to somebody
// else. Deliberately indistinguishable from "no such row": telling an attacker
// that an ID exists but is not theirs is the difference between an authorisation
// check and an enumeration oracle.
var ErrArtistNotOwned = errors.New("no such monitored artist")

// RepointMonitoredArtist moves a monitored row at a different MusicBrainz
// entity, in place.
//
// Two decisions are load-bearing and both are easy to get backwards:
//
//   - The caller MUST have resolved the new entity from MusicBrainz already.
//     This takes the resolved fields rather than an ID to look up, because a
//     service method that reached the network mid-transaction could hold a
//     write lock across a third-party call. The handler does the lookup; see
//     ArtistsHandler.Repoint.
//
//   - The counters reset. AcquiredReleases, TotalReleases and LastScanDate
//     describe the entity being replaced, so carrying them across would report
//     the old artist's discography as the new artist's. The monitor_* flags are
//     NOT reset: the operator chose to monitor this row, not to re-decide the
//     policy. And TrackedRelease rows are left alone -- acquired releases belong
//     to the library, not to the monitor.
//
// Re-pointing at the entity already being monitored is a no-op success. It
// discards nothing and queues no scan.
func (s *ArtistTrackingService) RepointMonitoredArtist(
	id uuid.UUID,
	mbid, name, sortName, disambiguation, country, artistType string,
	userID uint64,
) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		var artist database.MonitoredArtist
		// No isAdmin branch, and that is the point. UpdateArtistStatus and
		// DeleteMonitoredArtist both accept (id, userID, isAdmin) and skip the
		// owner filter for an admin, because pausing or removing someone's row
		// is an operator action. Re-pointing is not: it rewrites which artist
		// the owner's dashboard claims to be tracking, and no operator role
		// makes that their call.
		if err := tx.Where("id = ? AND owner_user_id = ?", id, userID).First(&artist).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrArtistNotOwned
			}
			return err
		}

		if artist.MusicBrainzID == mbid {
			// Same entity. Resetting the counters here would discard a real
			// discography for nothing, and queuing a scan would re-fetch what
			// we already have.
			return nil
		}

		updates := map[string]any{
			"music_brainz_id":    mbid,
			"name":               name,
			"sort_name":          sortName,
			"disambiguation":     disambiguation,
			"country":            country,
			"artist_type":        artistType,
			"acquired_releases":  0,
			"total_releases":     0,
			"last_scan_date":     nil,
			"last_release_check": nil,
		}
		if err := tx.Model(&database.MonitoredArtist{}).Where("id = ?", artist.ID).Updates(updates).Error; err != nil {
			return err
		}

		// queueArtistScan, not QueueArtistScan: the internal one takes this
		// transaction and answers a job that is already queued or running for
		// this artist. The worker takes an advisory lock keyed on
		// scope_type:scope_id, so a second live job would serialise for nothing
		// -- and an operator who pressed two buttons deserves one scan.
		updated := artist
		updated.MusicBrainzID = mbid
		updated.Name = name
		updated.SortName = sortName
		updated.Country = country
		updated.ArtistType = artistType
		updated.AcquiredReleases = 0
		updated.TotalReleases = 0
		updated.LastScanDate = nil

		if _, _, err := s.queueArtistScan(tx, &updated, "user_api"); err != nil {
			return fmt.Errorf("re-point queued no scan: %w", err)
		}
		return nil
	})
}
