package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/csrf"
	"github.com/gofiber/fiber/v2/middleware/limiter"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/google/uuid"
	"github.com/pvnkmnk/netrunner/backend/internal/agent"
	"github.com/pvnkmnk/netrunner/backend/internal/api"
	"github.com/pvnkmnk/netrunner/backend/internal/api/templates"
	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/pvnkmnk/netrunner/backend/internal/services"
	"gorm.io/gorm"
)

func main() {
	// 1. Load Config
	cfg, err := config.Load()
	if err != nil {
		slog.Error("Failed to load config", "error", err)
		os.Exit(1)
	}

	// 2. Connect Database
	db, err := database.Connect(cfg)
	if err != nil {
		slog.Error("Failed to connect to database", "error", err)
		os.Exit(1)
	}

	// 3. Run migrations (creates users, quality_profiles, etc.)
	if err := database.Migrate(db); err != nil {
		slog.Error("Failed to run migrations", "error", err)
		os.Exit(1)
	}

	// 4. Seed default quality profiles
	profileService := services.NewProfileService(db)
	if _, err := profileService.EnsureDefaultProfile(); err != nil {
		slog.Warn("Failed to ensure default profile", "error", err)
	}

	// 5. Initialize Services
	mbService := services.NewMusicBrainzService(cfg)
	atService := services.NewArtistTrackingService(db, mbService)
	scanService := services.NewScannerService(db)

	// 6. Initialize Fiber with pongo2 (Jinja2-compatible) template engine
	engine := templates.NewPongo2(cfg.TemplatesPath, ".html")
	if err := engine.LoadFromDir(); err != nil {
		slog.Warn("Failed to preload templates", "error", err)
	}

	app := fiber.New(fiber.Config{
		Views:                   engine,
		ProxyHeader:             fiber.HeaderXForwardedFor,
		EnableTrustedProxyCheck: true,
		TrustedProxies: []string{
			"127.0.0.0/8",    // IPv4 loopback
			"10.0.0.0/8",     // RFC1918 private
			"172.16.0.0/12",  // RFC1918 private
			"192.168.0.0/16", // RFC1918 private
			"::1/128",        // IPv6 loopback
			"fc00::/7",       // IPv6 unique local
		},
	})

	app.Use(recover.New())
	app.Use(logger.New())
	app.Static("/static", cfg.StaticFilesPath)

	// SECURITY: CSRF protection for state-changing operations
	// Uses cookie-based storage with HTMX-compatible header matching
	app.Use(csrf.New(csrf.Config{
		KeyLookup:      "header:X-CSRF-Token",
		CookieName:     "csrf_",
		CookieSameSite: "Lax",
		Expiration:     24 * time.Hour,
		ContextKey:     "csrf",
	}))

	// SECURITY: Add security headers to all responses
	// CSP is set here (not just in Caddy) to protect direct :8080 access
	app.Use(func(c *fiber.Ctx) error {
		c.Set("X-Content-Type-Options", "nosniff")
		c.Set("X-Frame-Options", "DENY")
		c.Set("X-XSS-Protection", "1; mode=block")
		c.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		c.Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self' ws: wss:; img-src 'self' data: https:; font-src 'self' data:;")
		return c.Next()
	})

	// Handlers
	healthHandler := api.NewHealthHandler(db, cfg)
	authHandler := api.NewAuthHandler(db)
	dashHandler := api.NewDashboardHandler(db)
	statsHandler := api.NewStatsHandler(db)
	libraryHandler := api.NewLibraryHandler(db)
	profileHandler := api.NewProfileHandler(db)
	spotifyAuthHandler := api.NewSpotifyAuthHandler(db)
	watchlistService := services.NewWatchlistService(db, spotifyAuthHandler, cfg)
	watchlistHandler := api.NewWatchlistHandler(db, watchlistService)
	wsManager := api.NewWebSocketManager()
	artistsHandler := api.NewArtistsHandler(db, atService, mbService)
	schedulesHandler := api.NewSchedulesHandler(db)

	// Health check (public, no authentication)
	app.Get("/api/health", healthHandler.GetHealth)

	// Start log listener with graceful shutdown and exponential backoff
	listenerCtx, listenerCancel := context.WithCancel(context.Background())
	go func() {
		backoff := 1 * time.Second
		const maxBackoff = 30 * time.Second
		for {
			wsManager.ListenForJobLogs(listenerCtx, cfg.DatabaseURL, db)

			select {
			case <-listenerCtx.Done():
				slog.Info("Log listener retry loop stopped")
				return
			default:
			}

			slog.Warn("Log listener exited, restarting", "backoff", backoff)
			select {
			case <-time.After(backoff):
			case <-listenerCtx.Done():
				slog.Info("Log listener retry loop stopped")
				return
			}
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}()

	// Routes
	setupRoutes(app, db, cfg, authHandler, dashHandler, statsHandler, libraryHandler, profileHandler, watchlistHandler, watchlistService, spotifyAuthHandler, wsManager, atService, scanService, artistsHandler, schedulesHandler)

	// Start server
	go func() {
		if err := app.Listen(":8080"); err != nil {
			slog.Error("Server failed", "error", err)
		}
	}()

	// Wait for termination
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	slog.Info("Shutting down server...")
	listenerCancel()
	app.Shutdown()
}

func setupRoutes(app *fiber.App, db *gorm.DB, cfg *config.Config, auth *api.AuthHandler, dash *api.DashboardHandler, stats *api.StatsHandler, library *api.LibraryHandler, profile *api.ProfileHandler, watchlist *api.WatchlistHandler, watchlistService *services.WatchlistService, spotifyAuth *api.SpotifyAuthHandler, ws *api.WebSocketManager, at *services.ArtistTrackingService, scan *services.ScannerService, artistsHandler *api.ArtistsHandler, schedulesHandler *api.SchedulesHandler) {
	// Public API routes
	apiPublic := app.Group("/api")

	// Auth routes
	authRoutes := apiPublic.Group("/auth")

	// Rate limiter for auth endpoints (configurable via environment variables)
	authLimiter := limiter.New(limiter.Config{
		Max: cfg.AuthRateLimitMax,
		Expiration: func() time.Duration {
			if d, err := time.ParseDuration(cfg.AuthRateLimitExpiration); err == nil {
				return d
			}
			return 1 * time.Minute // fallback
		}(),
		KeyGenerator: func(c *fiber.Ctx) string {
			// Get raw TCP connection address — c.IP() may already apply
			// trusted-proxy logic, making the X-Real-IP trust check circular.
			rawAddr := c.Context().RemoteAddr().String()
			remoteAddr := rawAddr
			if host, _, err := net.SplitHostPort(rawAddr); err == nil {
				remoteAddr = host
			}
			if ip := net.ParseIP(remoteAddr); ip != nil && (ip.IsPrivate() || ip.IsLoopback()) {
				if forwarded := c.Get("X-Real-IP"); forwarded != "" {
					// Strip port if present
					if host, _, err := net.SplitHostPort(forwarded); err == nil {
						forwarded = host
					}
					return strings.TrimSpace(forwarded)
				}
			}
			// Fall back to normalized remoteAddr (already stripped of port)
			return remoteAddr
		},
		LimitReached: func(c *fiber.Ctx) error {
			return c.Status(429).JSON(fiber.Map{"error": "too many requests, please try again later"})
		},
	})

	// Apply rate limiter to sensitive auth endpoints (excluding logout to prevent user friction)
	authRoutes.Post("/register", authLimiter, auth.Register)
	authRoutes.Post("/login", authLimiter, auth.Login)
	authRoutes.Post("/logout", auth.Logout)

	// Spotify Auth (OAuth Callback is public, but redirected to with user session)
	authRoutes.Get("/spotify/login", auth.AuthMiddleware, spotifyAuth.Login)
	authRoutes.Get("/spotify/callback", auth.AuthMiddleware, spotifyAuth.Callback)

	// UI routes (public - handles both auth and unauth)
	app.Get("/", dash.RenderIndex)

	// Page routes
	app.Get("/watchlists", auth.AuthMiddleware, watchlist.WatchlistsPage)
	app.Get("/libraries", auth.AuthMiddleware, library.LibrariesPage)
	app.Get("/profiles", auth.AuthMiddleware, profile.ProfilesPage)
	app.Get("/schedules", auth.AuthMiddleware, schedulesHandler.SchedulesPage)
	app.Get("/artists", auth.AuthMiddleware, artistsHandler.ArtistsPage)
	app.Get("/jobs", auth.AuthMiddleware, stats.JobsPage)

	// Partial routes (all protected)
	app.Get("/partials/stats", auth.AuthMiddleware, stats.RenderStatsPartial)
	app.Get("/partials/watchlists", auth.AuthMiddleware, watchlist.RenderWatchlistsPartial)
	app.Get("/partials/libraries", auth.AuthMiddleware, library.RenderLibrariesPartial)
	app.Get("/partials/schedules", auth.AuthMiddleware, schedulesHandler.RenderSchedulesPartial)
	app.Get("/partials/artists", auth.AuthMiddleware, artistsHandler.RenderPartial)
	app.Get("/partials/artist-form", auth.AuthMiddleware, artistsHandler.GetForm)
	app.Get("/partials/jobs", auth.AuthMiddleware, stats.RenderJobsPartial)
	app.Get("/partials/libraries/:id/browse", auth.AuthMiddleware, library.BrowseTracks)
	app.Get("/partials/tracks/:id", auth.AuthMiddleware, library.TrackDetail)

	// Protected API routes
	apiProtected := app.Group("/api", auth.AuthMiddleware)

	// Watchlists
	watchlistRoutes := apiProtected.Group("/watchlists")
	watchlistRoutes.Get("/", watchlist.ListWatchlists)
	watchlistRoutes.Post("/", watchlist.CreateWatchlist)
	watchlistRoutes.Patch("/:id", watchlist.UpdateWatchlist)
	watchlistRoutes.Delete("/:id", watchlist.DeleteWatchlist)
	watchlistRoutes.Get("/profiles", watchlist.ListProfiles)
	watchlistRoutes.Patch("/:id/toggle", watchlist.ToggleWatchlist)
	watchlistRoutes.Get("/form", watchlist.GetForm)
	watchlistPreviewHandler := api.NewWatchlistPreviewHandler(db, watchlistService)
	watchlistRoutes.Get("/:id/preview", watchlistPreviewHandler.GetPreview)

	// Quality Profiles
	profileRoutes := apiProtected.Group("/profiles")
	profileRoutes.Get("/", profile.List)
	profileRoutes.Post("/", profile.Create)
	profileRoutes.Get("/form", profile.GetForm)
	profileRoutes.Get("/:id", profile.Get)
	profileRoutes.Patch("/:id", profile.Update)
	profileRoutes.Delete("/:id", profile.Delete)

	// Artists
	artistsRoutes := apiProtected.Group("/artists")
	artistsRoutes.Get("/", artistsHandler.List)
	artistsRoutes.Get("/form", artistsHandler.GetForm)
	artistsRoutes.Post("/", artistsHandler.Add)
	artistsRoutes.Delete("/:id", artistsHandler.Delete)
	artistsRoutes.Patch("/:id", artistsHandler.Update)

	// Schedules
	schedulesRoutes := apiProtected.Group("/schedules")
	schedulesRoutes.Get("/", schedulesHandler.List)
	schedulesRoutes.Get("/form", schedulesHandler.GetForm)
	schedulesRoutes.Post("/", schedulesHandler.Create)
	schedulesRoutes.Delete("/:id", schedulesHandler.Delete)
	schedulesRoutes.Patch("/:id", schedulesHandler.Update)
	schedulesRoutes.Patch("/:id/toggle", schedulesHandler.Toggle)

	// Libraries
	libraryRoutes := apiProtected.Group("/libraries")
	libraryRoutes.Get("/", library.ListLibraries)
	libraryRoutes.Get("/form", library.GetForm)
	libraryRoutes.Post("/", library.CreateLibrary)
	libraryRoutes.Get("/:id", library.GetLibrary)
	libraryRoutes.Patch("/:id", library.UpdateLibrary)
	libraryRoutes.Delete("/:id", library.DeleteLibrary)
	libraryRoutes.Post("/:id/scan", library.TriggerScan)
	libraryRoutes.Post("/:id/enrich", library.TriggerEnrich)
	libraryRoutes.Post("/:id/prune", library.TriggerPrune)
	libraryRoutes.Get("/:id/tracks", library.ListTracks)

	// Stats
	statsRoutes := apiProtected.Group("/stats")
	statsRoutes.Get("/jobs", stats.GetJobStats)
	statsRoutes.Get("/jobs/breakdown", stats.GetJobTypeBreakdown)
	statsRoutes.Get("/jobs/trends", stats.GetJobTrends)
	statsRoutes.Get("/library", stats.GetLibraryStats)
	statsRoutes.Get("/activity", stats.GetActivityStats)
	statsRoutes.Get("/summary", stats.GetSummary)

	// Jobs
	jobRoutes := apiProtected.Group("/jobs")
	jobRoutes.Get("/", func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(database.User)
		if !ok {
			return c.Status(401).JSON(fiber.Map{"error": "unauthorized"})
		}

		var jobs []database.Job
		q := db.Order("requested_at DESC").Limit(50)
		if user.Role != "admin" {
			q = q.Where("owner_user_id = ?", user.ID)
		}
		if err := q.Find(&jobs).Error; err != nil {
			slog.Error("Failed to fetch jobs", "error", err)
			return c.Status(500).JSON(fiber.Map{"error": "failed to fetch jobs"})
		}
		return c.JSON(jobs)
	})
	jobRoutes.Post("/sync", func(c *fiber.Ctx) error {
		watchlistID := c.Query("watchlist_id")
		if watchlistID != "" {
			return c.JSON(fiber.Map{"status": "watchlist_sync_triggered", "id": watchlistID})
		}
		return c.Status(400).JSON(fiber.Map{"error": "no scope specified"})
	})
	jobRoutes.Post("/:id/retry", func(c *fiber.Ctx) error {
		jobID, err := parseUint64(c.Params("id"))
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid job ID"})
		}
		user, ok := c.Locals("user").(database.User)
		if !ok {
			return c.Status(401).JSON(fiber.Map{"error": "unauthorized"})
		}
		var job database.Job
		if err := db.First(&job, jobID).Error; err != nil {
			return c.Status(404).JSON(fiber.Map{"error": "job not found"})
		}
		if user.Role != "admin" && (job.OwnerUserID == nil || *job.OwnerUserID != user.ID) {
			return c.Status(403).JSON(fiber.Map{"error": "forbidden"})
		}
		if err := agent.RetryJob(db, jobID); err != nil {
			slog.Error("Failed to retry job", "job_id", jobID, "error", err)
			return c.Status(400).JSON(fiber.Map{"error": "failed to retry job"})
		}
		return c.JSON(fiber.Map{"status": "retry_queued", "job_id": jobID})
	})
	jobRoutes.Post("/:id/cancel", func(c *fiber.Ctx) error {
		jobID, err := parseUint64(c.Params("id"))
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid job ID"})
		}
		user, ok := c.Locals("user").(database.User)
		if !ok {
			return c.Status(401).JSON(fiber.Map{"error": "unauthorized"})
		}
		var job database.Job
		if err := db.First(&job, jobID).Error; err != nil {
			return c.Status(404).JSON(fiber.Map{"error": "job not found"})
		}
		if user.Role != "admin" && (job.OwnerUserID == nil || *job.OwnerUserID != user.ID) {
			return c.Status(403).JSON(fiber.Map{"error": "forbidden"})
		}
		if err := agent.CancelJob(db, jobID); err != nil {
			slog.Error("Failed to cancel job", "job_id", jobID, "error", err)
			return c.Status(400).JSON(fiber.Map{"error": "failed to cancel job"})
		}
		return c.JSON(fiber.Map{"status": "cancelled", "job_id": jobID})
	})

	// WebSockets
	// ✅ SECURITY: Apply authentication middleware to WebSocket endpoints
	app.Get("/ws/events", auth.AuthMiddleware, websocket.New(ws.HandleEvents))
	app.Get("/ws/jobs/:job_id", auth.AuthMiddleware, websocket.New(func(c *websocket.Conn) {
		ws.HandleConsole(c, db)
	}))

	// Artist Tracking — triggers a background discography sync job
	apiProtected.Post("/artists/:id/sync", func(c *fiber.Ctx) error {
		artistID, err := uuid.Parse(c.Params("id"))
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid artist id"})
		}

		// Verify the artist exists
		var artist database.MonitoredArtist
		if err := db.First(&artist, "id = ?", artistID).Error; err != nil {
			return c.Status(404).JSON(fiber.Map{"error": "artist not found"})
		}

		user, ok := c.Locals("user").(database.User)
		if !ok {
			return c.Status(401).JSON(fiber.Map{"error": "unauthorized"})
		}

		// Enqueue artist_scan job for the worker to pick up
		job := database.Job{
			Type:        "artist_scan",
			State:       "queued",
			ScopeType:   "artist",
			ScopeID:     artistID.String(),
			RequestedAt: time.Now(),
			OwnerUserID: artist.OwnerUserID,
			CreatedBy:   "user_api",
		}

		if err := db.Create(&job).Error; err != nil {
			slog.Error("Failed to create artist sync job", "artist_id", artistID, "error", err)
			return c.Status(500).JSON(fiber.Map{"error": "failed to queue sync job"})
		}

		slog.Info("Queued artist discography sync", "job_id", job.ID, "artist_id", artistID, "user_id", user.ID)
		return c.JSON(fiber.Map{"status": "sync_queued", "job_id": job.ID, "artist": artist.Name})
	})

	apiProtected.Post("/library/scan", func(c *fiber.Ctx) error {
		var payload struct {
			LibraryID string `json:"library_id"`
		}
		if err := c.BodyParser(&payload); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid payload"})
		}
		return c.JSON(fiber.Map{"status": "scan_triggered"})
	})
}

func parseUint64(s string) (uint64, error) {
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid uint64: %s", s)
	}
	return n, nil
}
