package api

import (
	"log/slog"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"gorm.io/gorm"
)

type StatsHandler struct {
	db *gorm.DB
}

func NewStatsHandler(db *gorm.DB) *StatsHandler {
	return &StatsHandler{db: db}
}

// JobStats represents job statistics
type JobStats struct {
	Total       int64   `json:"total"`
	Queued      int64   `json:"queued"`
	Running     int64   `json:"running"`
	Succeeded   int64   `json:"succeeded"`
	Failed      int64   `json:"failed"`
	SuccessRate float64 `json:"success_rate"`
}

// JobTypeBreakdown represents stats per job type
type JobTypeBreakdown struct {
	Type      string `json:"type"`
	Total     int64  `json:"total"`
	Succeeded int64  `json:"succeeded"`
	Failed    int64  `json:"failed"`
}

// DailyJobTrend represents daily job counts
type DailyJobTrend struct {
	Date      string `json:"date"`
	Succeeded int64  `json:"succeeded"`
	Failed    int64  `json:"failed"`
}

// LibraryStats represents library statistics
type LibraryStats struct {
	TotalTracks      int64          `json:"total_tracks"`
	TotalSize        int64          `json:"total_size"`
	TotalSizeMB      float64        `json:"total_size_mb"`
	FormatBreakdown  []FormatCount  `json:"format_breakdown"`
	LibraryBreakdown []LibraryCount `json:"library_breakdown"`
}

// FormatCount represents track count per format
type FormatCount struct {
	Format    string `json:"format"`
	Count     int64  `json:"count"`
	TotalSize int64  `json:"total_size"`
}

// LibraryCount represents track count per library
type LibraryCount struct {
	LibraryID   string `json:"library_id"`
	LibraryName string `json:"library_name"`
	TrackCount  int64  `json:"track_count"`
	TotalSize   int64  `json:"total_size"`
}

// ActivityStats represents activity metrics
type ActivityStats struct {
	MonitoredArtists int64 `json:"monitored_artists"`
	Watchlists       int64 `json:"watchlists"`
	QualityProfiles  int64 `json:"quality_profiles"`
	Libraries        int64 `json:"libraries"`
	RecentJobs24h    int64 `json:"recent_jobs_24h"`
	RecentJobs7d     int64 `json:"recent_jobs_7d"`
}

// SummaryStats represents combined overview
type SummaryStats struct {
	Jobs     JobStats      `json:"jobs"`
	Library  LibraryStats  `json:"library"`
	Activity ActivityStats `json:"activity"`
}

// GetJobStats returns job statistics
func (h *StatsHandler) GetJobStats(c *fiber.Ctx) error {
	// BOLA Protection: Verify user authentication
	user, ok := c.Locals("user").(database.User)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
	}

	since := time.Now().Add(-24 * time.Hour)

	var stats JobStats
	jobQuery := h.db.Model(&database.Job{}).Where("requested_at > ?", since)
	if user.Role != "admin" {
		jobQuery = jobQuery.Where("owner_user_id = ?", user.ID)
	}
	if err := jobQuery.Select("COUNT(*) as total, " +
		"COUNT(*) FILTER (WHERE state = 'queued') as queued, " +
		"COUNT(*) FILTER (WHERE state = 'running') as running, " +
		"COUNT(*) FILTER (WHERE state = 'succeeded') as succeeded, " +
		"COUNT(*) FILTER (WHERE state = 'failed') as failed").
		Scan(&stats).Error; err != nil {
		return internalServerError(c, err)
	}

	// Calculate success rate (excluding queued/running)
	completed := stats.Succeeded + stats.Failed
	if completed > 0 {
		stats.SuccessRate = float64(stats.Succeeded) / float64(completed) * 100
	}

	return c.JSON(stats)
}

// GetJobTypeBreakdown returns job stats by type
func (h *StatsHandler) GetJobTypeBreakdown(c *fiber.Ctx) error {
	// BOLA Protection: Verify user authentication
	user, ok := c.Locals("user").(database.User)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
	}

	since := time.Now().Add(-30 * 24 * time.Hour)

	var breakdowns []JobTypeBreakdown
	jobQuery := h.db.Model(&database.Job{}).Where("requested_at > ?", since)
	if user.Role != "admin" {
		jobQuery = jobQuery.Where("owner_user_id = ?", user.ID)
	}
	jobQuery.Select("job_type as type, " +
		"COUNT(*) as total, " +
		"COUNT(*) FILTER (WHERE state = 'succeeded') as succeeded, " +
		"COUNT(*) FILTER (WHERE state = 'failed') as failed").
		Group("job_type").
		Scan(&breakdowns)

	return c.JSON(breakdowns)
}

// GetJobTrends returns daily job trends
func (h *StatsHandler) GetJobTrends(c *fiber.Ctx) error {
	// BOLA Protection: Verify user authentication
	user, ok := c.Locals("user").(database.User)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
	}

	days := 7
	if d := c.Query("days"); d != "" {
		if parsed, err := strconv.Atoi(d); err == nil && parsed > 0 {
			days = parsed
		}
	}

	since := time.Now().Add(-time.Duration(days) * 24 * time.Hour)

	var trends []DailyJobTrend
	jobQuery := h.db.Model(&database.Job{}).Where("requested_at > ?", since)
	if user.Role != "admin" {
		jobQuery = jobQuery.Where("owner_user_id = ?", user.ID)
	}
	jobQuery.Select("DATE(requested_at) as date, " +
		"COUNT(*) FILTER (WHERE state = 'succeeded') as succeeded, " +
		"COUNT(*) FILTER (WHERE state = 'failed') as failed").
		Group("DATE(requested_at)").
		Order("date").
		Scan(&trends)

	return c.JSON(trends)
}

// GetLibraryStats returns library statistics
func (h *StatsHandler) GetLibraryStats(c *fiber.Ctx) error {
	// BOLA Protection: Verify user authentication
	user, ok := c.Locals("user").(database.User)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
	}

	var stats LibraryStats

	// Bolt Optimization: Reduce database roundtrips from 4 to 2.
	// 1. Fetch format breakdown and calculate global totals in-memory.
	formatQuery := h.db.Model(&database.Track{})
	if user.Role != "admin" {
		formatQuery = formatQuery.Joins("JOIN libraries ON libraries.id = tracks.library_id").
			Where("libraries.owner_user_id = ?", user.ID)
	}
	if err := formatQuery.Select("format, COUNT(*) as count, COALESCE(SUM(file_size), 0) as total_size").
		Group("format").
		Order("count DESC").
		Scan(&stats.FormatBreakdown).Error; err != nil {
		return internalServerError(c, err)
	}

	for _, f := range stats.FormatBreakdown {
		stats.TotalTracks += f.Count
		stats.TotalSize += f.TotalSize
	}
	stats.TotalSizeMB = float64(stats.TotalSize) / (1024 * 1024)

	// 2. Fetch library breakdown with names using a single JOIN.
	// This also ensures libraries with 0 tracks are included in the results.
	libBreakdownQuery := `
		SELECT
			l.id as library_id,
			l.name as library_name,
			COUNT(t.id) as track_count,
			COALESCE(SUM(t.file_size), 0) as total_size
		FROM libraries l
		LEFT JOIN tracks t ON l.id = t.library_id
	`
	if user.Role != "admin" {
		libBreakdownQuery += " WHERE l.owner_user_id = ?"
	}
	libBreakdownQuery += " GROUP BY l.id, l.name ORDER BY l.name ASC"

	var err error
	if user.Role == "admin" {
		err = h.db.Raw(libBreakdownQuery).Scan(&stats.LibraryBreakdown).Error
	} else {
		err = h.db.Raw(libBreakdownQuery, user.ID).Scan(&stats.LibraryBreakdown).Error
	}

	if err != nil {
		return internalServerError(c, err)
	}

	return c.JSON(stats)
}

// GetActivityStats returns activity metrics
func (h *StatsHandler) GetActivityStats(c *fiber.Ctx) error {
	// BOLA Protection: Verify user authentication
	user, ok := c.Locals("user").(database.User)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
	}

	var stats ActivityStats

	// Bolt Optimization: Consolidate multiple count queries into a single SQL statement using subqueries
	// to reduce database roundtrips from 6 to 1.
	since24h := time.Now().Add(-24 * time.Hour)
	since7d := time.Now().Add(-7 * 24 * time.Hour)

	// BOLA: Add owner_user_id filtering for non-admin users
	var query string
	if user.Role == "admin" {
		query = `
			SELECT
				(SELECT COUNT(*) FROM monitored_artists) as monitored_artists,
				(SELECT COUNT(*) FROM watchlists) as watchlists,
				(SELECT COUNT(*) FROM quality_profiles) as quality_profiles,
				(SELECT COUNT(*) FROM libraries) as libraries,
				(SELECT COUNT(*) FROM jobs WHERE requested_at > ?) as recent_jobs24h,
				(SELECT COUNT(*) FROM jobs WHERE requested_at > ?) as recent_jobs7d
		`
	} else {
		query = `
			SELECT
				(SELECT COUNT(*) FROM monitored_artists WHERE owner_user_id = ?) as monitored_artists,
				(SELECT COUNT(*) FROM watchlists WHERE owner_user_id = ?) as watchlists,
				(SELECT COUNT(*) FROM quality_profiles WHERE owner_user_id = ?) as quality_profiles,
				(SELECT COUNT(*) FROM libraries WHERE owner_user_id = ?) as libraries,
				(SELECT COUNT(*) FROM jobs WHERE owner_user_id = ? AND requested_at > ?) as recent_jobs24h,
				(SELECT COUNT(*) FROM jobs WHERE owner_user_id = ? AND requested_at > ?) as recent_jobs7d
		`
	}

	var err error
	if user.Role == "admin" {
		err = h.db.Raw(query, since24h, since7d).Scan(&stats).Error
	} else {
		err = h.db.Raw(query, user.ID, user.ID, user.ID, user.ID, user.ID, since24h, user.ID, since7d).Scan(&stats).Error
	}

	if err != nil {
		return internalServerError(c, err)
	}

	return c.JSON(stats)
}

// GetSummary returns combined overview stats
func (h *StatsHandler) GetSummary(c *fiber.Ctx) error {
	// BOLA Protection: Verify user authentication
	user, ok := c.Locals("user").(database.User)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
	}

	var summary SummaryStats

	// Bolt Optimization: Consolidate multiple statistics queries into 2 database roundtrips.
	// 1. Job stats (last 24h) - with BOLA filtering
	since := time.Now().Add(-24 * time.Hour)
	jobQuery := h.db.Model(&database.Job{}).Where("requested_at > ?", since)
	if user.Role != "admin" {
		jobQuery = jobQuery.Where("owner_user_id = ?", user.ID)
	}
	if err := jobQuery.Select("COUNT(*) as total, " +
		"COUNT(*) FILTER (WHERE state = 'queued') as queued, " +
		"COUNT(*) FILTER (WHERE state = 'running') as running, " +
		"COUNT(*) FILTER (WHERE state = 'succeeded') as succeeded, " +
		"COUNT(*) FILTER (WHERE state = 'failed') as failed").
		Scan(&summary.Jobs).Error; err != nil {
		return internalServerError(c, err)
	}

	completed := summary.Jobs.Succeeded + summary.Jobs.Failed
	if completed > 0 {
		summary.Jobs.SuccessRate = float64(summary.Jobs.Succeeded) / float64(completed) * 100
	}

	// 2. Consolidated Activity and Library totals - with BOLA filtering
	since24h := time.Now().Add(-24 * time.Hour)
	if user.Role == "admin" {
		// Use a flat struct for scanning to be safe and clear.
		var flat struct {
			MonitoredArtists int64 `gorm:"column:monitored_artists"`
			Watchlists       int64 `gorm:"column:watchlists"`
			QualityProfiles  int64 `gorm:"column:quality_profiles"`
			Libraries        int64 `gorm:"column:libraries"`
			RecentJobs24h    int64 `gorm:"column:recent_jobs24h"`
			TotalTracks      int64 `gorm:"column:total_tracks"`
			TotalSize        int64 `gorm:"column:total_size"`
		}
		if err := h.db.Raw(`
			SELECT
				(SELECT COUNT(*) FROM monitored_artists) as monitored_artists,
				(SELECT COUNT(*) FROM watchlists) as watchlists,
				(SELECT COUNT(*) FROM quality_profiles) as quality_profiles,
				(SELECT COUNT(*) FROM libraries) as libraries,
				(SELECT COUNT(*) FROM jobs WHERE requested_at > ?) as recent_jobs24h,
				(SELECT COUNT(*) FROM tracks) as total_tracks,
				(SELECT COALESCE(SUM(file_size), 0) FROM tracks) as total_size
		`, since24h).Scan(&flat).Error; err != nil {
			return internalServerError(c, err)
		}

		summary.Activity.MonitoredArtists = flat.MonitoredArtists
		summary.Activity.Watchlists = flat.Watchlists
		summary.Activity.QualityProfiles = flat.QualityProfiles
		summary.Activity.Libraries = flat.Libraries
		summary.Activity.RecentJobs24h = flat.RecentJobs24h
		summary.Library.TotalTracks = flat.TotalTracks
		summary.Library.TotalSize = flat.TotalSize
	} else {
		var flat struct {
			MonitoredArtists int64 `gorm:"column:monitored_artists"`
			Watchlists       int64 `gorm:"column:watchlists"`
			QualityProfiles  int64 `gorm:"column:quality_profiles"`
			Libraries        int64 `gorm:"column:libraries"`
			RecentJobs24h    int64 `gorm:"column:recent_jobs24h"`
			TotalTracks      int64 `gorm:"column:total_tracks"`
			TotalSize        int64 `gorm:"column:total_size"`
		}
		if err := h.db.Raw(`
			SELECT
				(SELECT COUNT(*) FROM monitored_artists WHERE owner_user_id = ?) as monitored_artists,
				(SELECT COUNT(*) FROM watchlists WHERE owner_user_id = ?) as watchlists,
				(SELECT COUNT(*) FROM quality_profiles WHERE owner_user_id = ?) as quality_profiles,
				(SELECT COUNT(*) FROM libraries WHERE owner_user_id = ?) as libraries,
				(SELECT COUNT(*) FROM jobs WHERE owner_user_id = ? AND requested_at > ?) as recent_jobs24h,
				(SELECT COUNT(*) FROM tracks t JOIN libraries l ON l.id = t.library_id WHERE l.owner_user_id = ?) as total_tracks,
				(SELECT COALESCE(SUM(file_size), 0) FROM tracks t JOIN libraries l ON l.id = t.library_id WHERE l.owner_user_id = ?) as total_size
		`, user.ID, user.ID, user.ID, user.ID, user.ID, since24h, user.ID, user.ID).Scan(&flat).Error; err != nil {
			return internalServerError(c, err)
		}

		summary.Activity.MonitoredArtists = flat.MonitoredArtists
		summary.Activity.Watchlists = flat.Watchlists
		summary.Activity.QualityProfiles = flat.QualityProfiles
		summary.Activity.Libraries = flat.Libraries
		summary.Activity.RecentJobs24h = flat.RecentJobs24h
		summary.Library.TotalTracks = flat.TotalTracks
		summary.Library.TotalSize = flat.TotalSize
	}

	summary.Library.TotalSizeMB = float64(summary.Library.TotalSize) / (1024 * 1024)

	return c.JSON(summary)
}

// RenderStatsPartial returns stats HTML for HTMX
func (h *StatsHandler) RenderStatsPartial(c *fiber.Ctx) error {
	user, hasAuth := currentUserFromLocals(c)

	isHtmx := isHTMXRequest(c)

	if !hasAuth {
		if isHtmx {
			return c.SendString("<div class=\"error\">Not authenticated.</div>")
		}
		return c.Redirect("/", 302)
	}

	var stats StatsData

	since := time.Now().Add(-24 * time.Hour)

	// Use conditional aggregation for efficient single-query stats
	jobQuery := h.db.Model(&database.Job{}).Where("requested_at > ?", since)
	if user.Role != "admin" {
		jobQuery = jobQuery.Where("owner_user_id = ?", user.ID)
	}

	if err := jobQuery.
		Select("COUNT(*) FILTER (WHERE state = 'queued') as queued_count, " +
			"COUNT(*) FILTER (WHERE state = 'running') as running_count, " +
			"COUNT(*) FILTER (WHERE state = 'succeeded') as succeeded_count, " +
			"COUNT(*) FILTER (WHERE state = 'failed') as failed_count").
		Scan(&stats).Error; err != nil {
		slog.Error("Error fetching stats", "error", err)
		return c.SendString("<div class=\"error\">Error loading stats.</div>")
	}

	return c.Render("partials/stats", fiber.Map{
		"stats":   stats,
		"IsAdmin": user.Role == "admin",
	})
}
