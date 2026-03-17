package main

import (
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/gofiber/template/html/v2"
	"github.com/pvnkmnk/netrunner/backend/internal/api"
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

	// 3. Initialize Services
	mbService := services.NewMusicBrainzService(cfg)
	atService := services.NewArtistTrackingService(db, mbService)
	scanService := services.NewScannerService(db)

	// 4. Initialize Fiber
	engine := html.New("./ops/web/templates", ".html")
	engine.AddFunc("strftime", func(t time.Time, format string) string {
		return t.Format("01/02 15:04")
	})
	engine.AddFunc("upper", strings.ToUpper)

	app := fiber.New(fiber.Config{
		Views: engine,
	})

	app.Use(recover.New())
	app.Use(logger.New())
	app.Static("/static", "./ops/web/static")

	// Handlers
	authHandler := api.NewAuthHandler(db)
	dashHandler := api.NewDashboardHandler(db)
	spotifyAuthHandler := api.NewSpotifyAuthHandler(db)
	watchlistService := services.NewWatchlistService(db, spotifyAuthHandler, cfg)
	watchlistHandler := api.NewWatchlistHandler(db, watchlistService)
	wsManager := api.NewWebSocketManager()
	artistsHandler := api.NewArtistsHandler(db, atService, mbService)

	// Start log listener
	go wsManager.ListenForJobLogs(cfg.DatabaseURL, db)

	// Routes
	setupRoutes(app, db, authHandler, dashHandler, watchlistHandler, spotifyAuthHandler, wsManager, atService, scanService, artistsHandler)

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

func setupRoutes(app *fiber.App, db *gorm.DB, auth *api.AuthHandler, dash *api.DashboardHandler, watchlist *api.WatchlistHandler, spotifyAuth *api.SpotifyAuthHandler, ws *api.WebSocketManager, at *services.ArtistTrackingService, scan *services.ScannerService, artistsHandler *api.ArtistsHandler) {
	// Public API routes
	apiPublic := app.Group("/api")

	// Health check
	apiPublic.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})

	// Auth routes
	authRoutes := apiPublic.Group("/auth")
	authRoutes.Post("/register", auth.Register)
	authRoutes.Post("/login", auth.Login)
	authRoutes.Post("/logout", auth.Logout)

	// Spotify Auth (OAuth Callback is public, but redirected to with user session)
	authRoutes.Get("/spotify/login", auth.AuthMiddleware, spotifyAuth.Login)
	authRoutes.Get("/spotify/callback", auth.AuthMiddleware, spotifyAuth.Callback)

	// UI routes (protected)
	app.Get("/", auth.AuthMiddleware, dash.RenderIndex)

	// Protected API routes
	apiProtected := app.Group("/api", auth.AuthMiddleware)

	// Watchlists
	watchlistRoutes := apiProtected.Group("/watchlists")
	watchlistRoutes.Get("/", watchlist.ListWatchlists)
	watchlistRoutes.Post("/", watchlist.CreateWatchlist)
	watchlistRoutes.Patch("/:id", watchlist.UpdateWatchlist)
	watchlistRoutes.Delete("/:id", watchlist.DeleteWatchlist)
	watchlistRoutes.Get("/profiles", watchlist.ListProfiles)

	// Artists
	artistsRoutes := apiProtected.Group("/artists")
	artistsRoutes.Get("/", artistsHandler.List)
	artistsRoutes.Post("/", artistsHandler.Add)
	artistsRoutes.Delete("/:id", artistsHandler.Delete)
	artistsRoutes.Patch("/:id", artistsHandler.Update)

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
	app.Get("/ws/events", websocket.New(ws.HandleEvents))
	app.Get("/ws/jobs/:job_id", websocket.New(func(c *websocket.Conn) {
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
