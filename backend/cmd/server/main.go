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
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/gofiber/template/html/v2"
	"github.com/pvnkmnk/netrunner/backend/internal/api"
	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/pvnkmnk/netrunner/backend/internal/services"
)

func main() {
	// 1. Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	// 2. Connect to database
	db, err := database.Connect(cfg)
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}

	// 3. Run migrations
	if err := database.Migrate(db); err != nil {
		log.Fatalf("failed to migrate database: %v", err)
	}

	// 4. Initialize services
	cache := services.NewCacheService(db)
	
	mbService := services.NewMusicBrainzService(cfg)
	mbService.SetCache(cache)
	
	atService := services.NewArtistTrackingService(db, mbService)
	rmService := services.NewReleaseMonitorService(db, atService)
	scanService := services.NewScannerService(db)
	
	spotify := services.NewSpotifyService(cfg)
	spotify.SetCache(cache)

	// 5. Start background tasks
	rmService.StartBackgroundTask()

	// 6. Initialize Template engine
	engine := html.New("../../ops/web/templates", ".html")
	engine.AddFunc("strftime", func(t interface{}, format string) string {
		if t == nil {
			return "Never"
		}
		
		var timeVal time.Time
		switch v := t.(type) {
		case time.Time:
			timeVal = v
		case *time.Time:
			if v == nil {
				return "Never"
			}
			timeVal = *v
		default:
			return "Invalid"
		}

		// Convert Python strftime format to Go layout
		goLayout := convertStrftimeToGo(format)
		return timeVal.Format(goLayout)
	})
	
	engine.AddFunc("upper", func(s string) string {
		return strings.ToUpper(s)
	})

	// Initialize Fiber app
	app := fiber.New(fiber.Config{
		AppName: "NetRunner API",
		Views:   engine,
	})

	// Middleware
	app.Use(logger.New())
	app.Use(cors.New())
	app.Use(recover.New())

	// Static files
	staticPath := getEnv("STATIC_FILES_PATH", "../../ops/web/static")
	app.Static("/static", staticPath)

	// Handlers
	authHandler := api.NewAuthHandler(db)
	dashHandler := api.NewDashboardHandler(db)
	sourceHandler := api.NewSourceHandler(db)
	wsManager := api.NewWebSocketManager()

	// Start log listener
	go wsManager.ListenForJobLogs(cfg.DatabaseURL, db)

	// Routes
	setupRoutes(app, authHandler, dashHandler, sourceHandler, wsManager, atService, scanService)

	// 7. Start server
	go func() {
		if err := app.Listen(":" + cfg.Port); err != nil {
			log.Fatalf("failed to start server: %v", err)
		}
	}()

	log.Printf("Server started on port %s", cfg.Port)

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")
	mbService.Close()
	if err := app.Shutdown(); err != nil {
		log.Fatalf("failed to shutdown server: %v", err)
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func convertStrftimeToGo(format string) string {
	// Simple mapping for common formats used in templates
	replacer := strings.NewReplacer(
		"%Y", "2006",
		"%m", "01",
		"%d", "02",
		"%H", "15",
		"%M", "04",
		"%S", "05",
	)
	return replacer.Replace(format)
}

func setupRoutes(app *fiber.App, auth *api.AuthHandler, dash *api.DashboardHandler, source *api.SourceHandler, ws *api.WebSocketManager, at *services.ArtistTrackingService, scan *services.ScannerService) {
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

	// UI routes (protected)
	app.Get("/", auth.AuthMiddleware, dash.RenderIndex)

	// WebSocket Console (protected via session cookie)
	app.Get("/ws/jobs/:job_id", auth.AuthMiddleware, websocket.New(func(c *websocket.Conn) {
		ws.HandleConsole(c, auth.GetDB())
	}))

	// Protected API routes
	apiProtected := app.Group("/api")
	apiProtected.Use(auth.AuthMiddleware)

	// Sources API
	sources := apiProtected.Group("/sources")
	sources.Get("/", source.ListSources)
	sources.Post("/", source.CreateSource)
	sources.Patch("/:id", source.UpdateSource)
	sources.Delete("/:id", source.DeleteSource)
	sources.Get("/:source_id/schedules", source.ListSchedules)
	sources.Post("/schedules", source.CreateSchedule)

	// Artist Tracking
	artists := apiProtected.Group("/artists")
	artists.Get("/", func(c *fiber.Ctx) error {
		list, err := at.GetMonitoredArtists()
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(list)
	})

	// Scanner
	apiProtected.Post("/scan", func(c *fiber.Ctx) error {
		var payload struct {
			Path      string `json:"path"`
			LibraryID string `json:"library_id"`
		}
		if err := c.BodyParser(&payload); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid payload"})
		}
		return c.JSON(fiber.Map{"status": "scan_triggered"})
	})
}
