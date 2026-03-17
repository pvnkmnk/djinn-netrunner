package api

import (
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
	since := time.Now().Add(-24 * time.Hour)

	var stats JobStats
	h.db.Model(&database.Job{}).Where("requested_at > ?", since).
		Select("COUNT(*) as total, " +
			"COUNT(*) FILTER (WHERE state = 'queued') as queued, " +
			"COUNT(*) FILTER (WHERE state = 'running') as running, " +
			"COUNT(*) FILTER (WHERE state = 'succeeded') as succeeded, " +
			"COUNT(*) FILTER (WHERE state = 'failed') as failed").
		Scan(&stats)

	// Calculate success rate (excluding queued/running)
	completed := stats.Succeeded + stats.Failed
	if completed > 0 {
		stats.SuccessRate = float64(stats.Succeeded) / float64(completed) * 100
	}

	return c.JSON(stats)
}

// GetJobTypeBreakdown returns job stats by type
func (h *StatsHandler) GetJobTypeBreakdown(c *fiber.Ctx) error {
	since := time.Now().Add(-30 * 24 * time.Hour)

	var breakdowns []JobTypeBreakdown
	h.db.Model(&database.Job{}).Where("requested_at > ?", since).
		Select("job_type as type, " +
			"COUNT(*) as total, " +
			"COUNT(*) FILTER (WHERE state = 'succeeded') as succeeded, " +
			"COUNT(*) FILTER (WHERE state = 'failed') as failed").
		Group("job_type").
		Scan(&breakdowns)

	return c.JSON(breakdowns)
}

// GetJobTrends returns daily job trends
func (h *StatsHandler) GetJobTrends(c *fiber.Ctx) error {
	days := 7
	if d := c.Query("days"); d != "" {
		if parsed := 0; parsed > 0 {
			days = parsed
		}
	}

	since := time.Now().Add(-time.Duration(days) * 24 * time.Hour)

	var trends []DailyJobTrend
	h.db.Model(&database.Job{}).Where("requested_at > ?", since).
		Select("DATE(requested_at) as date, " +
			"COUNT(*) FILTER (WHERE state = 'succeeded') as succeeded, " +
			"COUNT(*) FILTER (WHERE state = 'failed') as failed").
		Group("DATE(requested_at)").
		Order("date").
		Scan(&trends)

	return c.JSON(trends)
}

// GetLibraryStats returns library statistics
func (h *StatsHandler) GetLibraryStats(c *fiber.Ctx) error {
	var stats LibraryStats

	// Total tracks and size
	h.db.Model(&database.Track{}).
		Select("COUNT(*) as total_tracks, COALESCE(SUM(file_size), 0) as total_size").
		Scan(&stats)

	stats.TotalSizeMB = float64(stats.TotalSize) / (1024 * 1024)

	// Format breakdown
	h.db.Model(&database.Track{}).
		Select("format, COUNT(*) as count, COALESCE(SUM(file_size), 0) as total_size").
		Group("format").
		Order("count DESC").
		Scan(&stats.FormatBreakdown)

	// Library breakdown
	h.db.Model(&database.Track{}).
		Select("library_id, COUNT(*) as track_count, COALESCE(SUM(file_size), 0) as total_size").
		Group("library_id").
		Scan(&stats.LibraryBreakdown)

	// Join library names
	for i := range stats.LibraryBreakdown {
		var lib database.Library
		if err := h.db.First(&lib, "id = ?", stats.LibraryBreakdown[i].LibraryID).Error; err == nil {
			stats.LibraryBreakdown[i].LibraryName = lib.Name
		}
	}

	return c.JSON(stats)
}

// GetActivityStats returns activity metrics
func (h *StatsHandler) GetActivityStats(c *fiber.Ctx) error {
	var stats ActivityStats

	h.db.Model(&database.MonitoredArtist{}).Count(&stats.MonitoredArtists)
	h.db.Model(&database.Watchlist{}).Count(&stats.Watchlists)
	h.db.Model(&database.QualityProfile{}).Count(&stats.QualityProfiles)
	h.db.Model(&database.Library{}).Count(&stats.Libraries)

	// Recent jobs
	since24h := time.Now().Add(-24 * time.Hour)
	h.db.Model(&database.Job{}).Where("requested_at > ?", since24h).Count(&stats.RecentJobs24h)

	since7d := time.Now().Add(-7 * 24 * time.Hour)
	h.db.Model(&database.Job{}).Where("requested_at > ?", since7d).Count(&stats.RecentJobs7d)

	return c.JSON(stats)
}

// GetSummary returns combined overview stats
func (h *StatsHandler) GetSummary(c *fiber.Ctx) error {
	var summary SummaryStats

	// Job stats (last 24h)
	since := time.Now().Add(-24 * time.Hour)
	h.db.Model(&database.Job{}).Where("requested_at > ?", since).
		Select("COUNT(*) as total, " +
			"COUNT(*) FILTER (WHERE state = 'queued') as queued, " +
			"COUNT(*) FILTER (WHERE state = 'running') as running, " +
			"COUNT(*) FILTER (WHERE state = 'succeeded') as succeeded, " +
			"COUNT(*) FILTER (WHERE state = 'failed') as failed").
		Scan(&summary.Jobs)

	completed := summary.Jobs.Succeeded + summary.Jobs.Failed
	if completed > 0 {
		summary.Jobs.SuccessRate = float64(summary.Jobs.Succeeded) / float64(completed) * 100
	}

	// Library stats
	h.db.Model(&database.Track{}).
		Select("COUNT(*) as total_tracks, COALESCE(SUM(file_size), 0) as total_size").
		Scan(&summary.Library)
	summary.Library.TotalSizeMB = float64(summary.Library.TotalSize) / (1024 * 1024)

	// Activity stats
	h.db.Model(&database.MonitoredArtist{}).Count(&summary.Activity.MonitoredArtists)
	h.db.Model(&database.Watchlist{}).Count(&summary.Activity.Watchlists)
	h.db.Model(&database.QualityProfile{}).Count(&summary.Activity.QualityProfiles)
	h.db.Model(&database.Library{}).Count(&summary.Activity.Libraries)

	since24h := time.Now().Add(-24 * time.Hour)
	h.db.Model(&database.Job{}).Where("requested_at > ?", since24h).Count(&summary.Activity.RecentJobs24h)

	return c.JSON(summary)
}
