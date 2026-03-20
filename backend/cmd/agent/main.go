package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/pvnkmnk/netrunner/backend/internal/agent"
	"github.com/pvnkmnk/netrunner/backend/internal/api"
	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/pvnkmnk/netrunner/backend/internal/services"
)

func main() {
	// Load config
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Connect to database
	db, err := database.Connect(cfg)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	// Initialize services
	spotifyAuth := api.NewSpotifyAuthHandler(db)
	watchlistService := services.NewWatchlistService(db, spotifyAuth, cfg)
	gonicClient := services.NewGonicClient(cfg.GonicURL, cfg.GonicUser, cfg.GonicPass)

	// Create a new MCP server
	s := server.NewMCPServer(
		"NetRunner Agent Interface",
		"1.0.0",
	)

	// Register probe_system tool
	s.AddTool(mcp.NewTool("probe_system",
		mcp.WithDescription("Check the connectivity and health of NetRunner components"),
	), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		status, err := agent.ProbeSystem(db, cfg)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to probe system: %v", err)), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf(
			"Database: %v\nGonic: %v\nSlskd: %v\n\n%s",
			status.DatabaseConnected,
			status.GonicConnected,
			status.SlskdConnected,
			status.Message,
		)), nil
	})

	// Register read_config tool
	s.AddTool(mcp.NewTool("read_config",
		mcp.WithDescription("Read the current non-sensitive system configuration"),
	), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		settings, err := agent.ReadConfig(db, cfg)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to read config: %v", err)), nil
		}

		out := "Current Settings:\n"
		for k, v := range settings {
			out += fmt.Sprintf("- %s: %s\n", k, v)
		}
		return mcp.NewToolResultText(out), nil
	})

	// Register update_config tool
	s.AddTool(mcp.NewTool("update_config",
		mcp.WithDescription("Update a dynamic system setting"),
		mcp.WithString("key", mcp.Description("The setting key to update"), mcp.Required()),
		mcp.WithString("value", mcp.Description("The new value for the setting"), mcp.Required()),
	), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		key := mcp.ParseString(request, "key", "")
		if key == "" {
			return mcp.NewToolResultError("Missing required 'key' argument"), nil
		}
		value := mcp.ParseString(request, "value", "")
		if value == "" {
			return mcp.NewToolResultError("Missing required 'value' argument"), nil
		}

		if err := agent.UpdateConfig(db, key, value); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to update config: %v", err)), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf("Setting '%s' updated successfully.", key)), nil
	})

	// Register list_watchlists tool
	s.AddTool(mcp.NewTool("list_watchlists",
		mcp.WithDescription("List all registered music discovery watchlists"),
	), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		lists, err := agent.ListWatchlists(watchlistService)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to list watchlists: %v", err)), nil
		}

		out := "Registered Watchlists:\n"
		for _, l := range lists {
			status := "Enabled"
			if !l.Enabled {
				status = "Disabled"
			}
			out += fmt.Sprintf("- [%s] %s (%s): %s\n", status, l.Name, l.SourceType, l.SourceURI)
		}
		return mcp.NewToolResultText(out), nil
	})

	// Register add_watchlist tool
	s.AddTool(mcp.NewTool("add_watchlist",
		mcp.WithDescription("Add a new music discovery watchlist"),
		mcp.WithString("name", mcp.Description("Display name for the watchlist"), mcp.Required()),
		mcp.WithString("source_type", mcp.Description("Type of source (e.g., lastfm_loved, rss_feed, local_file)"), mcp.Required()),
		mcp.WithString("source_uri", mcp.Description("The URI or path for the source"), mcp.Required()),
		mcp.WithString("quality_profile_id", mcp.Description("UUID of the quality profile to use"), mcp.Required()),
	), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		name := mcp.ParseString(request, "name", "")
		sourceType := mcp.ParseString(request, "source_type", "")
		sourceURI := mcp.ParseString(request, "source_uri", "")
		profileIDStr := mcp.ParseString(request, "quality_profile_id", "")

		profileID, err := uuid.Parse(profileIDStr)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid quality_profile_id UUID: %v", err)), nil
		}

		wl, err := agent.AddWatchlist(watchlistService, name, sourceType, sourceURI, profileID, nil)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to add watchlist: %v", err)), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf("Watchlist '%s' created successfully with ID %s.", wl.Name, wl.ID)), nil
	})

	// Register sync_watchlist tool
	s.AddTool(mcp.NewTool("sync_watchlist",
		mcp.WithDescription("Trigger a sync job for a specific watchlist"),
		mcp.WithString("watchlist_id", mcp.Description("The UUID of the watchlist to sync"), mcp.Required()),
	), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		wlIDStr := mcp.ParseString(request, "watchlist_id", "")
		if wlIDStr == "" {
			return mcp.NewToolResultError("Missing required 'watchlist_id' argument"), nil
		}

		wlID, err := uuid.Parse(wlIDStr)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid watchlist_id UUID: %v", err)), nil
		}

		job, err := agent.SyncWatchlist(db, wlID, nil)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to sync watchlist: %v", err)), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf("Sync job #%d queued for watchlist %s.", job.ID, wlIDStr)), nil
	})

	// Register list_jobs tool
	s.AddTool(mcp.NewTool("list_jobs",
		mcp.WithDescription("List recent and active background jobs"),
		mcp.WithNumber("limit", mcp.Description("Number of jobs to return (default 10)")),
	), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		limit := int(mcp.ParseFloat64(request, "limit", 10))
		jobs, err := agent.ListJobs(db, limit)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to list jobs: %v", err)), nil
		}

		out := "Recent Jobs:\n"
		for _, j := range jobs {
			out += fmt.Sprintf("- [%s] ID: %d, Type: %s, Started: %s\n", j.State, j.ID, j.Type, j.RequestedAt.Format("15:04:05"))
		}
		return mcp.NewToolResultText(out), nil
	})

	// Register get_job_logs tool
	s.AddTool(mcp.NewTool("get_job_logs",
		mcp.WithDescription("Get structured logs for a specific job"),
		mcp.WithNumber("job_id", mcp.Description("The ID of the job"), mcp.Required()),
	), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		jobID := uint64(mcp.ParseFloat64(request, "job_id", 0))
		if jobID == 0 {
			return mcp.NewToolResultError("Missing or invalid 'job_id'"), nil
		}

		logs, err := agent.GetJobLogs(db, jobID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to get job logs: %v", err)), nil
		}

		out := ""
		for _, l := range logs {
			out += fmt.Sprintf("[%s] %s: %s\n", l.CreatedAt.Format("15:04:05"), l.Level, l.Message)
		}
		return mcp.NewToolResultText(fmt.Sprintf("Logs for Job %d:\n", jobID) + out), nil
	})

	// Register enqueue_acquisition tool
	s.AddTool(mcp.NewTool("enqueue_acquisition",
		mcp.WithDescription("Manually trigger a new acquisition job for a specific artist/track"),
		mcp.WithString("artist", mcp.Description("The artist name"), mcp.Required()),
		mcp.WithString("album", mcp.Description("The album name")),
		mcp.WithString("title", mcp.Description("The track title"), mcp.Required()),
	), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		artist := mcp.ParseString(request, "artist", "")
		album := mcp.ParseString(request, "album", "")
		title := mcp.ParseString(request, "title", "")

		if artist == "" || title == "" {
			return mcp.NewToolResultError("Missing required 'artist' or 'title' argument"), nil
		}

		job, err := agent.EnqueueAcquisition(db, artist, album, title, nil)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to enqueue acquisition: %v", err)), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf("Acquisition job #%d enqueued for: %s - %s", job.ID, artist, title)), nil
	})

	// Register bootstrap tool
	s.AddTool(mcp.NewTool("bootstrap",
		mcp.WithDescription("Perform initial environment validation and system setup"),
	), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		results, err := agent.Bootstrap(db, cfg)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Bootstrap process failed: %v", err)), nil
		}

		out := "Bootstrap Results:\n"
		for k, v := range results {
			out += fmt.Sprintf("- %s: %s\n", k, v)
		}
		return mcp.NewToolResultText(out), nil
	})

	// Register search_library tool
	s.AddTool(mcp.NewTool("search_library",
		mcp.WithDescription("Search the local acquisition index and Gonic server for tracks"),
		mcp.WithString("query", mcp.Description("The search query (artist, title, or album)"), mcp.Required()),
	), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		query := mcp.ParseString(request, "query", "")
		if query == "" {
			return mcp.NewToolResultError("Missing required 'query' argument"), nil
		}

		results, err := agent.SearchLibrary(db, gonicClient, query)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Search failed: %v", err)), nil
		}

		if len(results) == 0 {
			return mcp.NewToolResultText("No matches found in library."), nil
		}

		out := fmt.Sprintf("Found %d matches:\n", len(results))
		for _, r := range results {
			out += fmt.Sprintf("- [%s] %s - %s (%s)\n", r["source"], r["artist"], r["title"], r["album"])
		}
		return mcp.NewToolResultText(out), nil
	})

	// Register register_webhook tool
	s.AddTool(mcp.NewTool("register_webhook",
		mcp.WithDescription("Register a webhook URL for autonomous agent notifications"),
		mcp.WithString("url", mcp.Description("The callback URL"), mcp.Required()),
	), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		webhookURL := mcp.ParseString(request, "url", "")
		if webhookURL == "" {
			return mcp.NewToolResultError("Missing required 'url' argument"), nil
		}

		if err := agent.RegisterWebhook(db, webhookURL); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to register webhook: %v", err)), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf("Webhook '%s' registered successfully.", webhookURL)), nil
	})

	// Register get_stats tool
	s.AddTool(mcp.NewTool("get_stats",
		mcp.WithDescription("Get system summary statistics: jobs, library, and activity"),
	), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		stats, err := agent.GetStatsSummary(db)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to get stats: %v", err)), nil
		}

		out := fmt.Sprintf("Jobs (24h): %d total, %d queued, %d running, %d succeeded, %d failed (%.1f%% success)\n",
			stats.Jobs.Total, stats.Jobs.Queued, stats.Jobs.Running,
			stats.Jobs.Succeeded, stats.Jobs.Failed, stats.Jobs.SuccessRate)
		out += fmt.Sprintf("Library: %d tracks (%.1f MB)\n", stats.Library.TotalTracks, stats.Library.TotalSizeMB)
		out += fmt.Sprintf("Activity: %d monitored artists, %d watchlists, %d libraries",
			stats.Activity.MonitoredArtists, stats.Activity.Watchlists, stats.Activity.Libraries)

		return mcp.NewToolResultText(out), nil
	})

	// Register list_quality_profiles tool
	s.AddTool(mcp.NewTool("list_quality_profiles",
		mcp.WithDescription("List all quality profiles with their settings"),
	), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		profiles, err := agent.ListProfiles(db)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to list profiles: %v", err)), nil
		}

		if len(profiles) == 0 {
			return mcp.NewToolResultText("No quality profiles configured."), nil
		}

		out := "Quality Profiles:\n"
		for _, p := range profiles {
			defaultMark := ""
			if p.IsDefault {
				defaultMark = " [DEFAULT]"
			}
			bitrateInfo := ""
			if p.PreferBitrate != nil {
				bitrateInfo = fmt.Sprintf(", prefer %dkbps", *p.PreferBitrate)
			}
			out += fmt.Sprintf("- %s%s: %s, %s, min %dkbps%s\n",
				p.Name, defaultMark, p.Description, p.AllowedFormats, p.MinBitrate, bitrateInfo)
		}
		return mcp.NewToolResultText(out), nil
	})

	// Register list_libraries tool
	s.AddTool(mcp.NewTool("list_libraries",
		mcp.WithDescription("List all configured music libraries"),
	), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		libraries, err := agent.ListLibraries(db)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to list libraries: %v", err)), nil
		}

		if len(libraries) == 0 {
			return mcp.NewToolResultText("No libraries configured."), nil
		}

		out := "Libraries:\n"
		for _, lib := range libraries {
			out += fmt.Sprintf("- %s: %s (ID: %s)\n", lib.Name, lib.Path, lib.ID.String())
		}
		return mcp.NewToolResultText(out), nil
	})

	// Run the server on stdio
	if err := server.ServeStdio(s); err != nil {
		fmt.Fprintf(os.Stderr, "Error serving MCP: %v\n", err)
		os.Exit(1)
	}
}
