package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
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
	mbService := services.NewMusicBrainzService(cfg)
	atService := services.NewArtistTrackingService(db, mbService)
	rmService := services.NewReleaseMonitorService(db, atService)
	scanService := services.NewScannerService(db)

	// 5. Start background tasks
	rmService.StartBackgroundTask()

	// 6. Initialize Fiber app
	app := fiber.New(fiber.Config{
		AppName: "NetRunner API",
	})

	// Middleware
	app.Use(logger.New())
	app.Use(cors.New())
	app.Use(recover.New())

	// Static files
	app.Static("/static", "../../ops/web/static")

	// Handlers
	authHandler := api.NewAuthHandler(db)

	// Routes
	setupRoutes(app, authHandler, atService, scanService)

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

func setupRoutes(app *fiber.App, auth *api.AuthHandler, at *services.ArtistTrackingService, scan *services.ScannerService) {
	apiGroup := app.Group("/api")

	// Auth routes
	authRoutes := apiGroup.Group("/auth")
	authRoutes.Post("/register", auth.Register)
	authRoutes.Post("/login", auth.Login)
	authRoutes.Post("/logout", auth.Logout)

	// Protected routes
	protected := apiGroup.Group("/")
	protected.Use(auth.AuthMiddleware)

	// Health check (public)
	apiGroup.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})

	// Artist Tracking (protected)
	artists := protected.Group("/artists")
	artists.Get("/", func(c *fiber.Ctx) error {
		list, err := at.GetMonitoredArtists()
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(list)
	})

	// Scanner (protected)
	protected.Post("/scan", func(c *fiber.Ctx) error {
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
