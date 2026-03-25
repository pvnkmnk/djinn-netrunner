package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/limiter"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
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
		log.Fatalf("failed to load config: %v", err)
	}

	// 2. Connect Database
	db, err := database.Connect(cfg)
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}

	// 3. Run migrations (creates users, quality_profiles, etc.)
	if err := database.Migrate(db); err != nil {
		log.Fatalf("failed to run migrations: %v", err)
	}

	// 4. Seed default quality profiles
	profileService := services.NewProfileService(db)
	if _, err := profileService.EnsureDefaultProfile(); err != nil {
		log.Printf("warning: failed to ensure default profile: %v", err)
	}

	// 5. Initialize Services
	mbService := services.NewMusicBrainzService(cfg)
	atService := services.NewArtistTrackingService(db, mbService)
	scanService := services.NewScannerService(db)

	// 6. Initialize Fiber with pongo2 (Jinja2-compatible) template engine
	engine := templates.NewPongo2(cfg.TemplatesPath, ".html")
	if err := engine.LoadFromDir(); err != nil {
		log.Printf("warning: failed to preload templates: %v", err)
	}

	app := fiber.New(fiber.Config{
		Views:       engine,
		ProxyHeader: fiber.HeaderXForwardedFor,
	})

	app.Use(recover.New())
	app.Use(logger.New())
	app.Static("/static", cfg.StaticFilesPath)

	// Handlers
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

	// Start log listener
	go wsManager.ListenForJobLogs(cfg.DatabaseURL, db)

	// Routes
	setupRoutes(app, db, cfg, authHandler, dashHandler, statsHandler, libraryHandler, profileHandler, watchlistHandler, watchlistService, spotifyAuthHandler, wsManager, atService, scanService, artistsHandler, schedulesHandler)

	// Start server
	go func() {
		if err := app.Listen(":8080"); err != nil {
			log.Printf("Server failed: %v", err)
		}
	}()

	// Wait for termination
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	log.Println("Shutting down server...")
	app.Shutdown()
}

func setupRoutes(app *fiber.App, db *gorm.DB, cfg *config.Config, auth *api.AuthHandler, dash *api.DashboardHandler, stats *api.StatsHandler, library *api.LibraryHandler, profile *api.ProfileHandler, watchlist *api.WatchlistHandler, watchlistService *services.WatchlistService, spotifyAuth *api.SpotifyAuthHandler, ws *api.WebSocketManager, at *services.ArtistTrackingService, scan *services.ScannerService, artistsHandler *api.ArtistsHandler, schedulesHandler *api.SchedulesHandler) {
	// Public API routes
	apiPublic := app.Group("/api")

	// Health check
	apiPublic.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})

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
			return c.IP()
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

	// Partial routes (all protected - auth handled inside each handler)
	app.Get("/partials/stats", auth.AuthMiddleware, stats.RenderStatsPartial)
	app.Get("/partials/watchlists", auth.AuthMiddleware, watchlist.RenderWatchlistsPartial)
	app.Get("/partials/libraries", auth.AuthMiddleware, library.RenderLibrariesPartial)
	app.Get("/partials/schedules", auth.AuthMiddleware, schedulesHandler.RenderSchedulesPartial)
	app.Get("/partials/artists", auth.AuthMiddleware, artistsHandler.RenderPartial)
	app.Get("/partials/artist-form", auth.AuthMiddleware, artistsHandler.GetForm)
	app.Get("/partials/jobs", auth.AuthMiddleware, stats.RenderJobsPartial)

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
	jobRoutes.Get("/", func(c *fiber.Ctx) error { return nil })
	jobRoutes.Post("/sync", func(c *fiber.Ctx) error {
		watchlistID := c.Query("watchlist_id")
		if watchlistID != "" {
			return c.JSON(fiber.Map{"status": "watchlist_sync_triggered", "id": watchlistID})
		}
		return c.Status(400).JSON(fiber.Map{"error": "no scope specified"})
	})

	// WebSockets
	// ✅ SECURITY: Apply authentication middleware to WebSocket endpoints
	app.Get("/ws/events", auth.AuthMiddleware, websocket.New(ws.HandleEvents))
	app.Get("/ws/jobs/:job_id", auth.AuthMiddleware, websocket.New(func(c *websocket.Conn) {
		ws.HandleConsole(c, db)
	}))

	// Artist Tracking
	apiProtected.Post("/artists/track", func(c *fiber.Ctx) error {
		var payload struct {
			MBID string `json:"mbid"`
		}
		if err := c.BodyParser(&payload); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid payload"})
		}
		return c.JSON(fiber.Map{"status": "tracking_started"})
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
