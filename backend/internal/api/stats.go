package api

import (
	"log"
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
	user, ok := c.Locals("user").(database.User)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
	}

	since := time.Now().Add(-24 * time.Hour)

	var stats JobStats
	query := h.db.Model(&database.Job{}).Where("requested_at > ?", since)
	if user.Role != "admin" {
		query = query.Where("owner_user_id = ?", user.ID)
	}

	query.Select("COUNT(*) as total, " +
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
	user, ok := c.Locals("user").(database.User)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
	}

	since := time.Now().Add(-30 * 24 * time.Hour)

	var breakdowns []JobTypeBreakdown
	query := h.db.Model(&database.Job{}).Where("requested_at > ?", since)
	if user.Role != "admin" {
		query = query.Where("owner_user_id = ?", user.ID)
	}

	query.Select("job_type as type, " +
		"COUNT(*) as total, " +
		"COUNT(*) FILTER (WHERE state = 'succeeded') as succeeded, " +
		"COUNT(*) FILTER (WHERE state = 'failed') as failed").
		Group("job_type").
		Scan(&breakdowns)

	return c.JSON(breakdowns)
}

// GetJobTrends returns daily job trends
func (h *StatsHandler) GetJobTrends(c *fiber.Ctx) error {
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
	query := h.db.Model(&database.Job{}).Where("requested_at > ?", since)
	if user.Role != "admin" {
		query = query.Where("owner_user_id = ?", user.ID)
	}

	query.Select("DATE(requested_at) as date, " +
		"COUNT(*) FILTER (WHERE state = 'succeeded') as succeeded, " +
		"COUNT(*) FILTER (WHERE state = 'failed') as failed").
		Group("DATE(requested_at)").
		Order("date").
		Scan(&trends)

	return c.JSON(trends)
}

// GetLibraryStats returns library statistics
func (h *StatsHandler) GetLibraryStats(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(database.User)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
	}

	var stats LibraryStats

	// Total tracks and size
	query := h.db.Model(&database.Track{})
	if user.Role != "admin" {
		query = query.Joins("JOIN libraries ON libraries.id = tracks.library_id").Where("libraries.owner_user_id = ?", user.ID)
	}

	query.Select("COUNT(tracks.id) as total_tracks, COALESCE(SUM(tracks.file_size), 0) as total_size").
		Scan(&stats)

	stats.TotalSizeMB = float64(stats.TotalSize) / (1024 * 1024)

	// Format breakdown
	fQuery := h.db.Model(&database.Track{})
	if user.Role != "admin" {
		fQuery = fQuery.Joins("JOIN libraries ON libraries.id = tracks.library_id").Where("libraries.owner_user_id = ?", user.ID)
	}
	fQuery.Select("format, COUNT(tracks.id) as count, COALESCE(SUM(tracks.file_size), 0) as total_size").
		Group("format").
		Order("count DESC").
		Scan(&stats.FormatBreakdown)

	// Library breakdown
	lQuery := h.db.Model(&database.Track{})
	if user.Role != "admin" {
		lQuery = lQuery.Joins("JOIN libraries ON libraries.id = tracks.library_id").Where("libraries.owner_user_id = ?", user.ID)
	}
	lQuery.Select("library_id, COUNT(tracks.id) as track_count, COALESCE(SUM(tracks.file_size), 0) as total_size").
		Group("library_id").
		Scan(&stats.LibraryBreakdown)

	// Get library names in a single query to avoid N+1
	var libraries []database.Library
	libNameQuery := h.db.Model(&database.Library{})
	if user.Role != "admin" {
		libNameQuery = libNameQuery.Where("owner_user_id = ?", user.ID)
	}
	libNameQuery.Find(&libraries)
	libraryNames := make(map[string]string)
	for _, lib := range libraries {
		libraryNames[lib.ID.String()] = lib.Name
	}

	// Map library names
	for i := range stats.LibraryBreakdown {
		if name, ok := libraryNames[stats.LibraryBreakdown[i].LibraryID]; ok {
			stats.LibraryBreakdown[i].LibraryName = name
		}
	}

	return c.JSON(stats)
}

// GetActivityStats returns activity metrics
func (h *StatsHandler) GetActivityStats(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(database.User)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
	}

	var stats ActivityStats

	// Bolt Optimization: Consolidate multiple count queries into a single SQL statement using subqueries
	// to reduce database roundtrips from 6 to 1.
	since24h := time.Now().Add(-24 * time.Hour)
	since7d := time.Now().Add(-7 * 24 * time.Hour)

	// BOLA: Filter subqueries by owner_user_id for non-admin users
	var filterClause string
	var qpFilter string

	if user.Role != "admin" {
		uidStr := strconv.FormatUint(user.ID, 10)
		filterClause = " WHERE owner_user_id = " + uidStr
		qpFilter = " WHERE owner_user_id = " + uidStr + " OR is_default = true"
	}

	rawSQL := `
		SELECT
			(SELECT COUNT(*) FROM monitored_artists` + filterClause + `) as monitored_artists,
			(SELECT COUNT(*) FROM watchlists` + filterClause + `) as watchlists,
			(SELECT COUNT(*) FROM quality_profiles` + qpFilter + `) as quality_profiles,
			(SELECT COUNT(*) FROM libraries` + filterClause + `) as libraries,
			(SELECT COUNT(*) FROM jobs WHERE (owner_user_id = ? OR ? = true) AND requested_at > ?) as recent_jobs24h,
			(SELECT COUNT(*) FROM jobs WHERE (owner_user_id = ? OR ? = true) AND requested_at > ?) as recent_jobs7d
	`

	var finalParams []interface{}
	if user.Role != "admin" {
		finalParams = append(finalParams, user.ID, false, since24h, user.ID, false, since7d)
	} else {
		finalParams = append(finalParams, 0, true, since24h, 0, true, since7d)
	}

	err := h.db.Raw(rawSQL, finalParams...).Scan(&stats).Error

	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(stats)
}

// GetSummary returns combined overview stats
func (h *StatsHandler) GetSummary(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(database.User)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
	}

	var summary SummaryStats

	// Job stats (last 24h)
	since := time.Now().Add(-24 * time.Hour)
	jSub := h.db.Model(&database.Job{}).Where("requested_at > ?", since)
	if user.Role != "admin" {
		jSub = jSub.Where("owner_user_id = ?", user.ID)
	}

	jSub.Select("COUNT(*) as total, " +
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
	tQuery := h.db.Model(&database.Track{})
	if user.Role != "admin" {
		tQuery = tQuery.Joins("JOIN libraries ON libraries.id = tracks.library_id").Where("libraries.owner_user_id = ?", user.ID)
	}
	tQuery.Select("COUNT(tracks.id) as total_tracks, COALESCE(SUM(tracks.file_size), 0) as total_size").
		Scan(&summary.Library)
	summary.Library.TotalSizeMB = float64(summary.Library.TotalSize) / (1024 * 1024)

	// Activity stats
	since24h := time.Now().Add(-24 * time.Hour)

	var filterClause string
	var qpFilter string
	if user.Role != "admin" {
		uidStr := strconv.FormatUint(user.ID, 10)
		filterClause = " WHERE owner_user_id = " + uidStr
		qpFilter = " WHERE owner_user_id = " + uidStr + " OR is_default = true"
	}

	rawSQL := `
		SELECT
			(SELECT COUNT(*) FROM monitored_artists` + filterClause + `) as monitored_artists,
			(SELECT COUNT(*) FROM watchlists` + filterClause + `) as watchlists,
			(SELECT COUNT(*) FROM quality_profiles` + qpFilter + `) as quality_profiles,
			(SELECT COUNT(*) FROM libraries` + filterClause + `) as libraries,
			(SELECT COUNT(*) FROM jobs WHERE (owner_user_id = ? OR ? = true) AND requested_at > ?) as recent_jobs24h
	`

	var finalParams []interface{}
	if user.Role != "admin" {
		finalParams = append(finalParams, user.ID, false, since24h)
	} else {
		finalParams = append(finalParams, 0, true, since24h)
	}

	h.db.Raw(rawSQL, finalParams...).Scan(&summary.Activity)

	return c.JSON(summary)
}

// RenderStatsPartial returns stats HTML for HTMX
func (h *StatsHandler) RenderStatsPartial(c *fiber.Ctx) error {
	// Auth check - Use context
	user, ok := c.Locals("user").(database.User)
	if !ok {
		isHtmx := c.Get("Htmx-Request") == "true"
		if isHtmx {
			return c.SendString("<div class=\"error\">Not authenticated.</div>")
		}
		return c.Redirect("/", 302)
	}

	var stats StatsData
	since := time.Now().Add(-24 * time.Hour)

	// Use conditional aggregation for efficient single-query stats
	query := h.db.Model(&database.Job{}).Where("requested_at > ?", since)
	if user.Role != "admin" {
		query = query.Where("owner_user_id = ?", user.ID)
	}

	if err := query.Select("COUNT(*) FILTER (WHERE state = 'queued') as queued_count, " +
			"COUNT(*) FILTER (WHERE state = 'running') as running_count, " +
			"COUNT(*) FILTER (WHERE state = 'succeeded') as succeeded_count, " +
			"COUNT(*) FILTER (WHERE state = 'failed') as failed_count").
		Scan(&stats).Error; err != nil {
		log.Printf("Error fetching stats: %v", err)
		return c.SendString("<div class=\"error\">Error loading stats.</div>")
	}

	return c.Render("partials/stats", fiber.Map{
		"stats": stats,
	})
}
