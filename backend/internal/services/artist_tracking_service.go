package services

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"gorm.io/gorm"
)

// ArtistTrackingService manages monitored artists and their releases
type ArtistTrackingService struct {
	db *gorm.DB
	mb *MusicBrainzService
}

// NewArtistTrackingService creates a new artist tracking service
func NewArtistTrackingService(db *gorm.DB, mb *MusicBrainzService) *ArtistTrackingService {
	return &ArtistTrackingService{
		db: db,
		mb: mb,
	}
}

// AddMonitoredArtist adds a new artist to the system and starts monitoring.
//
// Monitoring an artist is a promise that its discography gets looked at. That
// promise used to stop at the INSERT: the row appeared, nothing ever scanned
// it, and the operator had to notice and press Sync by hand (DJI-588). So the
// row and its first scan are written in ONE transaction, through
// QueueArtistScan -- the single owner of "an artist scan reaches the queue".
// A failed enqueue therefore leaves no unscanned artist behind for the
// operator to discover later, and a retry is not met with a duplicate.
func (s *ArtistTrackingService) AddMonitoredArtist(mbid string, qualityProfileID uuid.UUID, name, sortName, disambiguation, country, artistType string, ownerUserID *uint64) (*database.MonitoredArtist, error) {
	var created *database.MonitoredArtist

	err := s.db.Transaction(func(tx *gorm.DB) error {
		// Check if artist already exists for this user (or globally if owner is nil)
		var existing database.MonitoredArtist
		query := tx.Where("music_brainz_id = ?", mbid)
		if ownerUserID != nil {
			query = query.Where("owner_user_id = ?", *ownerUserID)
		} else {
			query = query.Where("owner_user_id IS NULL")
		}

		switch err := query.First(&existing).Error; {
		case err == nil:
			return errors.New("artist already monitored")
		case !errors.Is(err, gorm.ErrRecordNotFound):
			// A lookup that FAILED is not a lookup that found nothing. Treating
			// a database error as "not monitored" is what let a second row for
			// the same artist be inserted under an outage, and it would do the
			// same here now that the insert carries a job with it.
			return err
		}

		// Use provided name or fallback to MBID
		artistName := name
		if artistName == "" {
			artistName = mbid
		}

		// Create artist record
		artist := database.MonitoredArtist{
			MusicBrainzID:    mbid,
			Name:             artistName,
			SortName:         sortName,
			Disambiguation:   disambiguation,
			Country:          country,
			ArtistType:       artistType,
			QualityProfileID: qualityProfileID,
			Monitored:        true,
			MonitorNew:       true,
			MonitorAlbums:    true,
			MonitorEPs:       true,
			OwnerUserID:      ownerUserID,
		}

		if err := tx.Create(&artist).Error; err != nil {
			return err
		}

		if _, _, err := s.queueArtistScan(tx, &artist, "user_api"); err != nil {
			return err
		}

		created = &artist
		return nil
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

// QueueArtistScan puts a monitored artist's discography scan on the job queue,
// and is the ONLY place that does so: AddMonitoredArtist calls it as part of
// the promise it makes, and POST /api/artists/:id/sync calls it for the
// operator who wants one now. Two callers, one owner -- the shape DJI-600
// applied to the password policy, and the reason the two cannot drift.
//
// It answers the existing job rather than queueing a second when one is already
// queued or running for that artist. The worker takes an advisory lock keyed on
// scope_type:scope_id, so two live jobs for one artist would serialise for no
// reason, and the operator pressing Sync twice deserves one scan, not two.
//
// alreadyActive is true when an existing job was returned rather than created.
func (s *ArtistTrackingService) QueueArtistScan(artist *database.MonitoredArtist, createdBy string) (*database.Job, bool, error) {
	return s.queueArtistScan(s.db, artist, createdBy)
}

func (s *ArtistTrackingService) queueArtistScan(db *gorm.DB, artist *database.MonitoredArtist, createdBy string) (*database.Job, bool, error) {
	var existing database.Job
	err := db.Where(
		"job_type = ? AND scope_type = ? AND scope_id = ? AND state IN ?",
		"artist_scan",
		"artist",
		artist.ID.String(),
		[]string{"queued", "running"},
	).First(&existing).Error
	switch {
	case err == nil:
		return &existing, true, nil
	case !errors.Is(err, gorm.ErrRecordNotFound):
		return nil, false, err
	}

	job := database.Job{
		Type:        "artist_scan",
		State:       "queued",
		ScopeType:   "artist",
		ScopeID:     artist.ID.String(),
		RequestedAt: time.Now(),
		OwnerUserID: artist.OwnerUserID,
		CreatedBy:   createdBy,
	}
	if err := db.Create(&job).Error; err != nil {
		return nil, false, err
	}
	return &job, false, nil
}

// GetMonitoredArtists retrieves all artists with monitoring enabled
func (s *ArtistTrackingService) GetMonitoredArtists(userID uint64, isAdmin bool) ([]database.MonitoredArtist, error) {
	var artists []database.MonitoredArtist
	query := s.db.Preload("QualityProfile").Where("monitored = ?", true)
	if !isAdmin {
		query = query.Where("owner_user_id = ?", userID)
	}
	err := query.Find(&artists).Error
	return artists, err
}

// UpdateArtistStatus updates the status of an artist
func (s *ArtistTrackingService) UpdateArtistStatus(id uuid.UUID, monitored bool, userID uint64, isAdmin bool) error {
	query := s.db.Model(&database.MonitoredArtist{}).Where("id = ?", id)
	if !isAdmin {
		query = query.Where("owner_user_id = ?", userID)
	}
	return query.Update("monitored", monitored).Error
}

// DeleteMonitoredArtist removes an artist from monitoring
func (s *ArtistTrackingService) DeleteMonitoredArtist(id uuid.UUID, userID uint64, isAdmin bool) error {
	// An artist's tracked releases are its children, and the FK says so. Only
	// the artist row used to be deleted, so removing any artist the worker had
	// actually scanned answered 500 on fk_monitored_artists_releases. That was
	// a rare path until DJI-588 made Add queue a scan: now every monitored
	// artist is scanned, so "scan, then remove" is the ordinary sequence and
	// the ordinary sequence 500s.
	//
	// Both deletes commit together and share the same ownership scope, so one
	// artist's removal never reaches another's releases. Deleting an artist that
	// does not exist still removes nothing and still reports no error, which is
	// what this always did.
	return s.db.Transaction(func(tx *gorm.DB) error {
		scope := tx.Model(&database.MonitoredArtist{}).Where("id = ?", id)
		if !isAdmin {
			scope = scope.Where("owner_user_id = ?", userID)
		}
		if err := tx.Where("artist_id IN (?)", scope.Select("id")).Delete(&database.TrackedRelease{}).Error; err != nil {
			return err
		}
		return scope.Delete(&database.MonitoredArtist{}).Error
	})
}

// SyncDiscography fetches the latest releases for an artist and creates acquisition jobs for new releases
func (s *ArtistTrackingService) SyncDiscography(artistID uuid.UUID) error {
	var artist database.MonitoredArtist
	if err := s.db.Preload("QualityProfile").First(&artist, "id = ?", artistID).Error; err != nil {
		return err
	}

	// Fetch from MusicBrainz
	data, err := s.mb.GetArtistDiscography(artist.MusicBrainzID)
	if err != nil {
		return err
	}

	// Parse release groups and upsert into TrackedRelease table
	releaseGroups, ok := data["release-groups"].([]interface{})
	if !ok {
		return nil
	}

	// Bolt Optimization: Bulk fetch existing releases to avoid N+1 queries in the loop
	var existingReleases []database.TrackedRelease
	if err := s.db.Where("artist_id = ?", artist.ID).Find(&existingReleases).Error; err != nil {
		return err
	}

	existingMap := make(map[string]database.TrackedRelease)
	for _, er := range existingReleases {
		existingMap[er.ReleaseGroupID] = er
	}

	var releasesToCreate []database.TrackedRelease
	var releasesToUpdate []database.TrackedRelease
	var newReleasesForJob []database.TrackedRelease

	now := time.Now()
	for _, rg := range releaseGroups {
		group := rg.(map[string]interface{})
		rgID, _ := group["id"].(string)
		title, _ := group["title"].(string)
		primaryType := ""
		if t, ok := group["primary-type"].(string); ok {
			primaryType = t
		}

		// Basic filtering based on artist preferences
		shouldMonitor := false
		switch primaryType {
		case "Album":
			shouldMonitor = artist.MonitorAlbums
		case "EP":
			shouldMonitor = artist.MonitorEPs
		case "Single":
			shouldMonitor = artist.MonitorSingles
		}

		if release, exists := existingMap[rgID]; !exists {
			// Create new
			// Bolt Fix: Manually generate ID to ensure consistency between creation and job item reference
			newRel := database.TrackedRelease{
				ID:             uuid.New(),
				ArtistID:       artist.ID,
				ReleaseGroupID: rgID,
				Title:          title,
				ReleaseType:    primaryType,
				Status:         "wanted",
				Monitored:      shouldMonitor,
				CreatedAt:      now,
				UpdatedAt:      now,
			}
			releasesToCreate = append(releasesToCreate, newRel)
			if shouldMonitor {
				newReleasesForJob = append(newReleasesForJob, newRel)
			}
		} else {
			// Update existing if title changed
			if release.Title != title {
				release.Title = title
				release.UpdatedAt = now
				releasesToUpdate = append(releasesToUpdate, release)
			}
		}
	}

	// Bolt Optimization: Use CreateInBatches for bulk inserts
	if len(releasesToCreate) > 0 {
		if err := s.db.CreateInBatches(releasesToCreate, 100).Error; err != nil {
			slog.Error("Error batch creating releases", "error", err)
		}
	}

	// Individual updates for modified releases (usually rare, so N queries here is acceptable
	// but we could also optimize this if needed)
	for _, rel := range releasesToUpdate {
		s.db.Model(&rel).Updates(map[string]interface{}{
			"title":      rel.Title,
			"updated_at": rel.UpdatedAt,
		})
	}

	// Create acquisition job for new releases AND for tracked releases that
	// never finished (wanted/queued/failed): a sync means "make the monitored
	// discography complete", so the existing backlog must be re-enqueued too,
	// otherwise repeated syncs are no-ops for anything but brand-new groups.
	var unfinishedReleases []database.TrackedRelease
	if err := s.db.Where("artist_id = ? AND monitored = ? AND status IN ?",
		artist.ID, true, []string{"wanted", "queued", "failed"}).Find(&unfinishedReleases).Error; err != nil {
		slog.Error("Error fetching unfinished releases", "error", err)
	}
	newReleasesForJob = append(newReleasesForJob, unfinishedReleases...)

	if len(newReleasesForJob) > 0 {
		job := database.Job{
			Type:        "acquisition",
			State:       "queued",
			ScopeType:   "artist",
			ScopeID:     artistID.String(),
			OwnerUserID: artist.OwnerUserID,
			CreatedBy:   "artist_tracking",
		}

		if err := s.db.Create(&job).Error; err != nil {
			slog.Error("Error creating acquisition job", "error", err)
		} else {
			var jobItems []database.JobItem
			var releaseIDsToMarkQueued []uuid.UUID

			// Create job items for each new release
			for i, rel := range newReleasesForJob {
				item := database.JobItem{
					JobID:           job.ID,
					Sequence:        i,
					NormalizedQuery: fmt.Sprintf("%s %s", artist.Name, rel.Title),
					Artist:          artist.Name,
					Album:           rel.Title,
					TrackTitle:      rel.Title,
					Status:          "queued",
					OwnerUserID:     artist.OwnerUserID,
				}
				jobItems = append(jobItems, item)
				releaseIDsToMarkQueued = append(releaseIDsToMarkQueued, rel.ID)
			}

			// Bolt Optimization: Batch create job items
			if err := s.db.CreateInBatches(jobItems, 100).Error; err != nil {
				slog.Error("Error batch creating job items", "error", err)
			}

			// Bolt Optimization: Bulk update release status
			if len(releaseIDsToMarkQueued) > 0 {
				s.db.Model(&database.TrackedRelease{}).
					Where("id IN ?", releaseIDsToMarkQueued).
					Update("status", "queued")
			}

			slog.Info("Created acquisition job", "job_id", job.ID, "items", len(newReleasesForJob), "artist", artist.Name)
		}
	}

	// Update last scan date
	scanNow := time.Now()
	if err := s.db.Model(&artist).Update("last_scan_date", &scanNow).Error; err != nil {
		return err
	}

	// A scan is the only place that knows the artist's release totals.
	return RefreshArtistReleaseCounters(s.db, artist.ID)
}

// RefreshArtistReleaseCounters recomputes an artist's denormalised release
// counters from tracked_releases.
//
// Recomputed, never incremented. The two numbers are a count of rows this
// package owns, so calling this twice gives the same answer and two writers
// cannot drift apart by one of them forgetting an increment.
//
// The count is over rows we keep, not over rows MusicBrainz just returned. A
// scan that comes back empty - a rate limit, a dropped connection, an artist
// with nothing published - therefore leaves both counters exactly as they were.
// That is deliberate: a rescan must never be able to make the numbers worse,
// and tracked_releases rows are never deleted, so an acquired release cannot
// fall out of the denominator either.
//
// The two callers are the two places that change the underlying truth:
// SyncDiscography at the end of a scan, and the worker's acquisition finaliser
// when releases flip to acquired. Writing acquired_releases from the scan
// alone would leave it stale for exactly as long as no scan happens to run -
// which is the common case, because acquisitions finish between scans.
func RefreshArtistReleaseCounters(db *gorm.DB, artistID uuid.UUID) error {
	// One statement, not a count followed by an update. A scan finishing while an
	// acquisition for the same artist finishes would otherwise each read a count
	// and then write its own stale copy over the other's. As a single UPDATE the
	// two serialise on the row lock, and each evaluates its subqueries against one
	// snapshot of tracked_releases.
	//
	// A single SET also means zero is always written. A GORM struct update would
	// skip zero values and leave an artist with no releases reading its last
	// non-zero count forever.
	//
	// Plain SQL rather than a GORM map update, so both columns come from the same
	// subquery pass, and portable to SQLite where these services are also tested
	// (no FILTER, no IS DISTINCT FROM).
	return db.Exec(`
		UPDATE monitored_artists SET
			total_releases = (
				SELECT count(*) FROM tracked_releases tr WHERE tr.artist_id = ?
			),
			acquired_releases = (
				SELECT count(*) FROM tracked_releases tr
				WHERE tr.artist_id = ? AND tr.status = 'acquired'
			)
		WHERE id = ?`, artistID, artistID, artistID).Error
}
