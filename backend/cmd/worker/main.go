package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/pvnkmnk/netrunner/backend/internal/api"
	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/pvnkmnk/netrunner/backend/internal/metrics"
	"github.com/pvnkmnk/netrunner/backend/internal/services"
	"github.com/robfig/cron/v3"
	"gorm.io/gorm"
)

const MaxConcurrentJobs = 5

type WorkerOrchestrator struct {
	workerID string
	db       *gorm.DB
	cfg      *config.Config

	// Services
	mbService           *services.MusicBrainzService
	atService           *services.ArtistTrackingService
	rmService           *services.ReleaseMonitorService
	watchlist           *services.WatchlistService
	scanService         *services.ScannerService
	discogs             *services.DiscogsService
	lockManager         database.LockManager
	spotify             *services.SpotifyService
	slskd               *services.SlskdService
	metadata            *services.MetadataExtractor
	litefs              *database.LiteFSGuard
	notificationService *services.NotificationService
	diskQuotaService    *services.DiskQuotaService

	// Handlers
	syncHandler *services.SyncHandler
	acqHandler  *services.AcquisitionHandler

	// Extracted sub-components (DJI-364)
	itemProcessor  *services.JobItemProcessor
	zombieRecovery *services.ZombieRecovery

	activeJobs map[uint64]*jobContext
	jobMutex   sync.Mutex
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup

	// Notify
	wakeupChan chan bool
}

type jobContext struct {
	job        database.Job
	cancel     context.CancelFunc
	ctx        context.Context
	lockKey    int64
	processing bool // true when a goroutine is actively processing this job
}

func NewWorkerOrchestrator(cfg *config.Config, db *gorm.DB) *WorkerOrchestrator {
	// 4. Initialize services
	cache := services.NewCacheService(db)
	sqlDB, _ := db.DB() // GORM v1 style; ignore error for now
	mb := services.NewMusicBrainzService(cfg)
	mb.SetCache(cache)
	at := services.NewArtistTrackingService(db, mb)
	rm := services.NewReleaseMonitorService(db, at)
	spotifyAuth := api.NewSpotifyAuthHandler(db)
	watchlist := services.NewWatchlistService(db, spotifyAuth, cfg)
	spotify := services.NewSpotifyService(cfg)
	spotify.SetCache(cache)
	slskd := services.NewSlskdService(cfg, db)
	metadata := services.NewMetadataExtractor()
	aid := services.NewAcoustIDService(cfg)
	aid.SetCache(cache)
	proxyClient := services.NewProxyAwareHTTPClient(cfg, 30*time.Second)
	gonic := services.NewGonicClient(cfg.GonicURL, cfg.GonicUser, cfg.GonicPass, proxyClient)
	discogs := services.NewDiscogsService(cfg)

	// DJI-357: Initialize ctx in constructor to prevent nil panic if methods
	// are called before Start(). Start() replaces this with a fresh context.
	ctx, cancel := context.WithCancel(context.Background())

	lm := database.NewLockManager(db)
	acqHandler := services.NewAcquisitionHandler(db, cfg, slskd, mb, aid, metadata, gonic, discogs, cache)

	return &WorkerOrchestrator{
		workerID:            fmt.Sprintf("worker-%s", uuid.New().String()[:8]),
		db:                  db,
		cfg:                 cfg,
		mbService:           mb,
		atService:           at,
		rmService:           rm,
		watchlist:           watchlist,
		scanService:         services.NewScannerService(db),
		discogs:             discogs,
		lockManager:         lm,
		spotify:             spotify,
		slskd:               slskd,
		metadata:            metadata,
		litefs:              database.NewLiteFSGuard(cfg.DatabaseURL),
		syncHandler:         services.NewSyncHandler(db, spotify, watchlist),
		acqHandler:          acqHandler,
		itemProcessor:       services.NewJobItemProcessor(db, acqHandler),
		zombieRecovery:      services.NewZombieRecovery(db, lm, services.DefaultZombieRecoveryConfig()),
		notificationService: services.NewNotificationService(cfg.NotificationWebhookURL, cfg.NotificationEnabled, proxyClient),
		diskQuotaService:    services.NewDiskQuotaService(sqlDB),
		activeJobs:          make(map[uint64]*jobContext),
		wakeupChan:          make(chan bool, 1),
		ctx:                 ctx,
		cancel:              cancel,
	}
}

func (w *WorkerOrchestrator) Start() {
	// ctx controls the lifecycle of all goroutines spawned by this orchestrator.
	// Call w.cancel() (via Stop()) to signal graceful shutdown.
	w.ctx, w.cancel = context.WithCancel(context.Background())
	slog.Info("Starting worker", "worker_id", w.workerID)

	if !database.IsPostgres(w.cfg.DatabaseURL) && MaxConcurrentJobs > 1 {
		slog.Warn("SQLite detected with MaxConcurrentJobs > 1 — concurrent workers are unsafe without PostgreSQL advisory locks. Consider switching to PostgreSQL for production workloads.",
			"max_concurrent_jobs", MaxConcurrentJobs)
	}

	// Start background tasks — all tracked in WaitGroup for graceful shutdown
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		w.heartbeatLoop()
	}()

	if w.litefs.IsPrimary() {
		w.wg.Add(3)
		go func() {
			defer w.wg.Done()
			w.schedulerLoop()
		}()
		go func() {
			defer w.wg.Done()
			w.watchlistPollingLoop()
		}()
		go func() {
			defer w.wg.Done()
			w.zombieRecovery.Run(w.ctx, w.workerID)
		}()
		w.rmService.StartBackgroundTask(w.ctx, &w.wg)
	} else {
		slog.Info("Running in replica mode. Skipping scheduler and watchlist poller.", "worker_id", w.workerID)
	}

	// listenForWakeup only works for Postgres, for SQLite we rely on polling
	if w.db.Dialector.Name() == "postgres" {
		w.wg.Add(1)
		go func() {
			defer w.wg.Done()
			w.listenForWakeup()
		}()
	}

	// Main job loop with round-robin item processing
	for {
		select {
		case <-w.ctx.Done():
			return
		default:
		}

		w.claimAndProcess()
		w.processActiveJobsRoundRobin()

		// Wait for next tick OR wakeup notification
		select {
		case <-time.After(5 * time.Second):
			// Regular poll
		case <-w.wakeupChan:
			slog.Info("Received wakeup notification", "worker_id", w.workerID)
		case <-w.ctx.Done():
			return
		}
	}
}

func (w *WorkerOrchestrator) watchlistPollingLoop() {
	slog.Info("Starting watchlist polling loop", "worker_id", w.workerID)
	// Poll every 4 hours by default, or use config if available
	ticker := time.NewTicker(4 * time.Hour)

	// Run once at startup
	w.triggerWatchlistSyncs()

	for {
		select {
		case <-w.ctx.Done():
			return
		case <-ticker.C:
			w.triggerWatchlistSyncs()
		}
	}
}

func (w *WorkerOrchestrator) triggerWatchlistSyncs() {
	lists, err := w.watchlist.GetWatchlists(0, "admin")
	if err != nil {
		slog.Error("Error fetching watchlists", "worker_id", w.workerID, "error", err)
		return
	}

	for _, l := range lists {
		if !l.Enabled {
			continue
		}

		slog.Info("Triggering watchlist sync", "worker_id", w.workerID, "watchlist", l.Name, "watchlist_id", l.ID)

		// Enqueue sync job for watchlist
		job := database.Job{
			Type:        "sync",
			State:       "queued",
			ScopeType:   "watchlist",
			ScopeID:     l.ID.String(),
			RequestedAt: time.Now(),
			OwnerUserID: l.OwnerUserID,
			CreatedBy:   "watchlist_poller",
		}

		if err := w.db.Create(&job).Error; err != nil {
			slog.Error("Error enqueuing watchlist job", "worker_id", w.workerID, "watchlist_id", l.ID, "error", err)
		}
	}
}

func (w *WorkerOrchestrator) listenForWakeup() {
	reportProblem := func(ev pq.ListenerEventType, err error) {
		if err != nil {
			slog.Error("Notify listener error", "worker_id", w.workerID, "error", err)
		}
	}

	listener := pq.NewListener(w.cfg.DatabaseURL, 10*time.Second, time.Minute, reportProblem)
	err := listener.Listen("opswakeup")
	if err != nil {
		slog.Error("Failed to listen for wakeup", "worker_id", w.workerID, "error", err)
		os.Exit(1)
	}

	slog.Info("Listening for opswakeup events", "worker_id", w.workerID)

	for {
		select {
		case <-w.ctx.Done():
			return
		case <-listener.Notify:
			// Non-blocking send to wakeupChan
			select {
			case w.wakeupChan <- true:
			default:
			}
		case <-time.After(1 * time.Minute):
			go listener.Ping()
		}
	}
}

func (w *WorkerOrchestrator) Stop() {
	slog.Info("Shutting down worker", "worker_id", w.workerID)

	// DJI-356: Stop rate limiter BEFORE cancel+wg.Wait to unblock any
	// goroutine waiting on a search token.
	w.slskd.Stop()

	w.cancel()

	w.jobMutex.Lock()
	for id, jc := range w.activeJobs {
		slog.Info("Cancelling job", "worker_id", w.workerID, "job_id", id)
		jc.cancel()
	}
	w.jobMutex.Unlock()

	w.wg.Wait()
	w.lockManager.Close()
	slog.Info("Shutdown complete", "worker_id", w.workerID)
}

func (w *WorkerOrchestrator) heartbeatLoop() {
	ticker := time.NewTicker(5 * time.Second)
	for {
		select {
		case <-w.ctx.Done():
			return
		case <-ticker.C:
			w.updateHeartbeats()
		}
	}
}

func (w *WorkerOrchestrator) checkQuotaAlerts() {
	if w.diskQuotaService == nil || w.notificationService == nil {
		return
	}
	alerts, err := w.diskQuotaService.CheckAllLibraryQuotas()
	if err != nil {
		slog.Error("Error checking quotas", "error", err)
		return
	}
	for _, alert := range alerts {
		if alert.LimitBytes > 0 {
			threshold := 80
			w.notificationService.NotifyQuotaWarning(&alert, threshold)
		}
	}
}



func (w *WorkerOrchestrator) updateHeartbeats() {
	w.jobMutex.Lock()
	defer w.jobMutex.Unlock()

	if len(w.activeJobs) == 0 {
		return
	}

	ids := make([]uint64, 0, len(w.activeJobs))
	for id := range w.activeJobs {
		ids = append(ids, id)
	}

	w.db.Model(&database.Job{}).Where("id IN ?", ids).Update("heartbeat_at", time.Now())
}

func (w *WorkerOrchestrator) schedulerLoop() {
	slog.Info("Starting scheduler loop", "worker_id", w.workerID)
	ticker := time.NewTicker(30 * time.Second)

	for {
		select {
		case <-w.ctx.Done():
			return
		case <-ticker.C:
			var schedules []database.Schedule
			// Initialize NextRunAt if NULL
			w.db.Model(&database.Schedule{}).Where("enabled = ? AND next_run_at IS NULL", true).Update("next_run_at", time.Now())

			// Find due schedules
			err := w.db.Where("enabled = ? AND next_run_at <= ?", true, time.Now()).Find(&schedules).Error
			if err != nil {
				slog.Error("Error fetching schedules", "worker_id", w.workerID, "error", err)
				continue
			}

			for _, s := range schedules {
				slog.Info("Executing schedule", "worker_id", w.workerID, "schedule_id", s.ID, "watchlist_id", s.WatchlistID)

				// Enqueue sync job
				job := database.Job{
					Type:        "sync",
					State:       "queued",
					ScopeType:   "watchlist",
					ScopeID:     s.WatchlistID.String(),
					RequestedAt: time.Now(),
					OwnerUserID: s.Watchlist.OwnerUserID,
					CreatedBy:   "scheduler",
				}

				if err := w.db.Create(&job).Error; err != nil {
					slog.Error("Error enqueuing scheduled job", "worker_id", w.workerID, "schedule_id", s.ID, "error", err)
					continue
				}

				// Compute next run at
				sched, err := cron.ParseStandard(s.CronExpr)
				if err != nil {
					slog.Error("Invalid cron expression", "worker_id", w.workerID, "schedule_id", s.ID, "cron", s.CronExpr, "error", err)
					w.db.Model(&s).Update("enabled", false)
					continue
				}

				nextRun := sched.Next(time.Now())
				now := time.Now()
				w.db.Model(&s).Updates(map[string]interface{}{
					"last_run_at": &now,
					"next_run_at": &nextRun,
				})
			}
		}
	}
}

func (w *WorkerOrchestrator) claimAndProcess() {
	w.jobMutex.Lock()
	if len(w.activeJobs) >= MaxConcurrentJobs {
		w.jobMutex.Unlock()
		return
	}
	w.jobMutex.Unlock()

	var job database.Job

	// Capture claim timestamp before the transaction so the same value is
	// persisted in the DB and kept on the local struct (see post-tx block).
	claimTime := time.Now()

	// Start an immediate transaction to "lock" the row for SQLite
	err := w.db.Transaction(func(tx *gorm.DB) error {
		// 1. Find next queued job
		err := tx.Where("state = ?", "queued").Order("requested_at ASC").First(&job).Error
		if err != nil {
			return err // Will rollback and we'll try again next tick
		}

		// 2. Mark as running — guard with state='queued' to prevent two workers
		//    claiming the same job (see also DJI-331 for item-level fix).
		result := tx.Model(&job).Where("state = ?", "queued").Updates(map[string]interface{}{
			"state":        "running",
			"worker_id":    w.workerID,
			"started_at":   &claimTime,
			"heartbeat_at": &claimTime,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound // already claimed by another worker
		}
		return nil
	})

	if err != nil {
		if err != gorm.ErrRecordNotFound {
			slog.Error("Error claiming job", "worker_id", w.workerID, "error", err)
		}
		return
	}

	// GORM map-based Updates doesn't write back into the struct, so populate
	// the fields we just set in the DB to keep the local copy consistent.
	job.State = "running"
	job.WorkerID = &w.workerID
	job.StartedAt = &claimTime
	job.HeartbeatAt = &claimTime

	// Update queued gauge after claiming
	var queuedCount int64
	w.db.Model(&database.Job{}).Where("state = ?", "queued").Count(&queuedCount)
	metrics.JobsQueued.Set(float64(queuedCount))

	// Acquire advisory lock for scope (use worker context to allow cancellation).
	// If lock key computation or acquisition fails, requeue the job immediately
	// rather than leaving it in 'running' state for ZombieRecovery to clean up.
	lockKey, err := w.lockManager.GetScopeLockKey(w.ctx, job.ScopeType, job.ScopeID)
	if err != nil {
		slog.Error("Error computing lock key, requeueing", "worker_id", w.workerID, "job_id", job.ID, "error", err)
		w.db.Model(&job).Updates(map[string]interface{}{
			"state": "queued", "worker_id": nil, "started_at": nil,
		})
		return
	}

	acquired, err := w.lockManager.AcquireTryLock(w.ctx, lockKey)
	if err != nil {
		slog.Error("Error acquiring advisory lock, requeueing", "worker_id", w.workerID, "job_id", job.ID, "error", err)
		w.db.Model(&job).Updates(map[string]interface{}{
			"state": "queued", "worker_id": nil, "started_at": nil,
		})
		return
	}

	if !acquired {
		slog.Info("Scope locked, requeueing", "worker_id", w.workerID, "job_id", job.ID, "scope_type", job.ScopeType, "scope_id", job.ScopeID)
		w.db.Model(&job).Updates(map[string]interface{}{
			"state":      "queued",
			"worker_id":  nil,
			"started_at": nil,
		})
		return
	}

	ctx, cancel := context.WithCancel(w.ctx)

	jc := &jobContext{
		job:     job,
		cancel:  cancel,
		ctx:     ctx,
		lockKey: lockKey,
	}

	w.jobMutex.Lock()
	// Re-check capacity while holding lock — prevents race between check and insertion (DJI-339)
	if len(w.activeJobs) >= MaxConcurrentJobs {
		w.jobMutex.Unlock()
		cancel()
		w.lockManager.ReleaseLock(context.Background(), lockKey)
		w.db.Model(&job).Updates(map[string]interface{}{
			"state": "queued", "worker_id": nil, "started_at": nil,
		})
		slog.Info("Capacity reached, requeued job", "worker_id", w.workerID, "job_id", job.ID)
		return
	}
	w.activeJobs[job.ID] = jc
	metrics.JobsRunning.Set(float64(len(w.activeJobs)))
	w.jobMutex.Unlock()

	slog.Info("Claimed job", "worker_id", w.workerID, "job_id", job.ID, "job_type", job.Type)
}

func (w *WorkerOrchestrator) processActiveJobsRoundRobin() {
	w.jobMutex.Lock()
	activeIDs := make([]uint64, 0, len(w.activeJobs))
	for id := range w.activeJobs {
		activeIDs = append(activeIDs, id)
	}
	w.jobMutex.Unlock()

	for _, id := range activeIDs {
		w.jobMutex.Lock()
		jc, ok := w.activeJobs[id]
		if ok && jc.processing {
			w.jobMutex.Unlock()
			continue
		}
		if ok {
			jc.processing = true
		}
		w.jobMutex.Unlock()

		if !ok {
			continue
		}

		// Process based on type
		switch jc.job.Type {
		case "acquisition":
			// Process one item
			w.wg.Add(1)
			go func(jc *jobContext) {
				defer func() {
					w.jobMutex.Lock()
					jc.processing = false
					w.jobMutex.Unlock()
				}()
				defer w.wg.Done()
				err := w.runJobSafely(jc, func() error {
					w.processAcquisitionItem(jc)
					return nil
				})
				if err != nil {
					w.finishJob(jc.job.ID, err)
				}
			}(jc)
		case "sync":
			w.wg.Add(1)
			go func(jc *jobContext) {
				defer func() {
					w.jobMutex.Lock()
					jc.processing = false
					w.jobMutex.Unlock()
				}()
				defer w.wg.Done()
				err := w.runJobSafely(jc, func() error {
					return w.syncHandler.Execute(jc.ctx, jc.job.ID, jc.job)
				})
				w.finishJob(jc.job.ID, err)
			}(jc)
		default:
			w.wg.Add(1)
			go func(jc *jobContext) {
				defer func() {
					w.jobMutex.Lock()
					jc.processing = false
					w.jobMutex.Unlock()
				}()
				defer w.wg.Done()
				err := w.runJobSafely(jc, func() error {
					w.runMonolithicJob(jc)
					return nil
				})
				if err != nil {
					w.finishJob(jc.job.ID, err)
				}
			}(jc)
		}
	}
}

func (w *WorkerOrchestrator) runJobSafely(jc *jobContext, fn func() error) error {
	return services.RunSafely(w.workerID, jc.job.ID, jc.job.Type, fn)
}

func (w *WorkerOrchestrator) processAcquisitionItem(jc *jobContext) {
	result := w.itemProcessor.ProcessItem(jc.ctx, w.workerID, jc.job.ID)
	if result.NoItems {
		w.finishJob(jc.job.ID, nil)
	}
}

func (w *WorkerOrchestrator) runMonolithicJob(jc *jobContext) {
	slog.Info("Executing monolithic job", "worker_id", w.workerID, "job_id", jc.job.ID, "job_type", jc.job.Type)

	var err error
	switch jc.job.Type {
	case "artist_scan":
		artistID, _ := uuid.Parse(jc.job.ScopeID)
		err = w.atService.SyncDiscography(artistID)
	case "release_monitor":
		err = w.rmService.CheckAllArtists()
	case "index_refresh":
		slog.Info("Triggering Gonic index refresh", "worker_id", w.workerID, "job_id", jc.job.ID)
		// Placeholder for actual refresh call via GonicClient
		w.mbService.HealthCheck() // Just a dummy call to use a service
	case "scan":
		libraryID, err := uuid.Parse(jc.job.ScopeID)
		if err != nil {
			err = fmt.Errorf("invalid library UUID: %w", err)
			w.finishJob(jc.job.ID, err)
			return
		}
		// Look up the library path from the database
		var library database.Library
		if err := w.db.First(&library, "id = ?", libraryID).Error; err != nil {
			w.finishJob(jc.job.ID, err)
			return
		}
		slog.Info("Scanning library", "name", library.Name, "path", library.Path)
		err = w.scanService.ScanLibrary(jc.ctx, libraryID, library.Path)
		if err == nil {
			w.checkQuotaAlerts() // Check disk quotas after a successful scan
		}
	case "enrich":
		// Enrich metadata for tracks in a library using Discogs
		libraryID, err := uuid.Parse(jc.job.ScopeID)
		if err != nil {
			err = fmt.Errorf("invalid library UUID: %w", err)
			w.finishJob(jc.job.ID, err)
			return
		}
		slog.Info("Starting metadata enrichment for library", "library_id", libraryID)

		// Get library
		var library database.Library
		if err := w.db.First(&library, "id = ?", libraryID).Error; err != nil {
			w.finishJob(jc.job.ID, err)
			return
		}

		// Get all tracks for this library that need enrichment
		var tracks []database.Track
		w.db.Where("library_id = ? AND (genre = '' OR genre IS NULL)", libraryID).Find(&tracks)

		enriched := 0
		for _, track := range tracks {
			select {
			case <-jc.ctx.Done():
				err = jc.ctx.Err()
				goto doneEnrich
			default:
			}

			// Try to enrich from Discogs
			enrichedData, err := w.discogs.EnrichTrack(track.Artist, track.Title)
			if err != nil {
				slog.Warn("Could not enrich track", "artist", track.Artist, "title", track.Title, "error", err)
				continue
			}

			// Update track
			if coverURL, ok := enrichedData["cover_url"].(string); ok {
				track.CoverURL = coverURL
			}
			if genre, ok := enrichedData["genre"].(string); ok {
				track.Genre = genre
			}
			if year, ok := enrichedData["year"].(int); ok {
				track.Year = &year
			}

			w.db.Save(&track)
			enriched++
		}

	doneEnrich:
		slog.Info("Enriched tracks for library", "count", enriched, "library", library.Name)
	case "prune":
		libraryID, err := uuid.Parse(jc.job.ScopeID)
		if err != nil {
			err = fmt.Errorf("invalid library UUID: %w", err)
			w.finishJob(jc.job.ID, err)
			return
		}
		var library database.Library
		if err := w.db.First(&library, "id = ?", libraryID).Error; err != nil {
			w.finishJob(jc.job.ID, err)
			return
		}
		slog.Info("Pruning library", "name", library.Name, "library_id", libraryID)
		err = w.scanService.PruneTracks(jc.ctx, libraryID, jc.job.ID)
	default:
		err = fmt.Errorf("unsupported job type: %s", jc.job.Type)
	}

	w.finishJob(jc.job.ID, err)
}

func (w *WorkerOrchestrator) finishJob(jobID uint64, err error) {
	w.jobMutex.Lock()
	jc, ok := w.activeJobs[jobID]
	if !ok {
		w.jobMutex.Unlock()
		return
	}
	delete(w.activeJobs, jobID)
	runningCount := len(w.activeJobs)
	w.jobMutex.Unlock()

	// Release lock (use background context since worker may be shutting down)
	w.lockManager.ReleaseLock(context.Background(), jc.lockKey)

	finalState := "succeeded"
	summary := "Completed"
	if err != nil {
		finalState = "failed"
		summary = err.Error()
	}

	now := time.Now()
	updates := map[string]interface{}{
		"state":        finalState,
		"finished_at":  &now,
		"summary":      summary,
		"error_detail": "",
	}
	if err != nil {
		updates["error_detail"] = err.Error()
	}
	w.db.Model(&database.Job{}).Where("id = ?", jobID).Updates(updates)

	w.notificationService.NotifyJobCompletion(jobID, jc.job.Type, finalState, summary, w.workerID)

	// Record metrics
	metrics.JobsProcessedTotal.WithLabelValues(jc.job.Type, finalState).Inc()
	if jc.job.StartedAt != nil {
		metrics.JobDurationSeconds.WithLabelValues(jc.job.Type).Observe(time.Since(*jc.job.StartedAt).Seconds())
	}
	metrics.JobsRunning.Set(float64(runningCount))

	slog.Info("Finished job", "worker_id", w.workerID, "job_id", jobID, "state", finalState)
}

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("Failed to load config", "error", err)
		os.Exit(1)
	}

	db, err := database.Connect(cfg)
	if err != nil {
		slog.Error("Failed to connect to database", "error", err)
		os.Exit(1)
	}

	worker := NewWorkerOrchestrator(cfg, db)

	// Expose /metrics for Prometheus scraping on :9090
	metricsMux := http.NewServeMux()
	metricsMux.Handle("/metrics", promhttp.Handler())
	metricsServer := &http.Server{Addr: ":9090", Handler: metricsMux}
	go func() {
		slog.Info("Worker metrics server listening on :9090")
		if err := metricsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("Metrics server error", "error", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	go worker.Start()

	slog.Info("Worker process running. Press Ctrl+C to stop.")
	<-stop
	metricsServer.Close()
	worker.Stop()
}
