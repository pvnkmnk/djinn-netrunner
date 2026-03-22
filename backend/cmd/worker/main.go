package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/pvnkmnk/netrunner/backend/internal/api"
	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
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

	// Handlers
	syncHandler *services.SyncHandler
	acqHandler  *services.AcquisitionHandler

	activeJobs map[uint64]*jobContext
	jobMutex   sync.Mutex
	running    bool
	wg         sync.WaitGroup

	// Notify
	wakeupChan chan bool
}

type jobContext struct {
	job     database.Job
	cancel  context.CancelFunc
	ctx     context.Context
	lockKey int64
}

func NewWorkerOrchestrator(cfg *config.Config, db *gorm.DB) *WorkerOrchestrator {
	// 4. Initialize services
	cache := services.NewCacheService(db)
	mb := services.NewMusicBrainzService(cfg)
	mb.SetCache(cache)
	at := services.NewArtistTrackingService(db, mb)
	rm := services.NewReleaseMonitorService(db, at)
	spotifyAuth := api.NewSpotifyAuthHandler(db)
	watchlist := services.NewWatchlistService(db, spotifyAuth, cfg)
	spotify := services.NewSpotifyService(cfg)
	spotify.SetCache(cache)
	slskd := services.NewSlskdService(cfg)
	metadata := services.NewMetadataExtractor()
	aid := services.NewAcoustIDService(cfg)
	aid.SetCache(cache)
	gonic := services.NewGonicClient(cfg.GonicURL, cfg.GonicUser, cfg.GonicPass)
	discogs := services.NewDiscogsService(cfg)
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
		lockManager:         database.NewLockManager(db),
		spotify:             spotify,
		slskd:               slskd,
		metadata:            metadata,
		litefs:              database.NewLiteFSGuard(cfg.DatabaseURL),
		syncHandler:         services.NewSyncHandler(db, spotify, watchlist),
		acqHandler:          services.NewAcquisitionHandler(db, cfg, slskd, mb, aid, metadata, gonic, discogs, cache),
		notificationService: services.NewNotificationService(cfg.NotificationWebhookURL, cfg.NotificationEnabled),
		activeJobs:          make(map[uint64]*jobContext),
		wakeupChan:          make(chan bool, 1),
	}
}

func (w *WorkerOrchestrator) Start() {
	w.running = true
	log.Printf("[WORKER] Starting worker | worker_id=%s", w.workerID)

	// Start background tasks
	go w.heartbeatLoop()

	if w.litefs.IsPrimary() {
		go w.schedulerLoop()
		go w.watchlistPollingLoop()
		go w.zombieCleanupLoop()
		go w.rmService.StartBackgroundTask()
	} else {
		log.Printf("[WORKER] Running in replica mode. Skipping scheduler and watchlist poller. | worker_id=%s", w.workerID)
	}

	// listenForWakeup only works for Postgres, for SQLite we rely on polling
	if w.db.Dialector.Name() == "postgres" {
		go w.listenForWakeup()
	}

	// Main job loop with round-robin item processing
	for w.running {
		w.claimAndProcess()
		w.processActiveJobsRoundRobin()

		// Wait for next tick OR wakeup notification
		select {
		case <-time.After(5 * time.Second):
			// Regular poll
		case <-w.wakeupChan:
			log.Printf("[WORKER] Received wakeup notification | worker_id=%s", w.workerID)
		}
	}
}

func (w *WorkerOrchestrator) watchlistPollingLoop() {
	log.Printf("[WATCHLIST] Starting watchlist polling loop | worker_id=%s", w.workerID)
	// Poll every 4 hours by default, or use config if available
	ticker := time.NewTicker(4 * time.Hour)

	// Run once at startup
	w.triggerWatchlistSyncs()

	for range ticker.C {
		if !w.running {
			return
		}
		w.triggerWatchlistSyncs()
	}
}

func (w *WorkerOrchestrator) triggerWatchlistSyncs() {
	lists, err := w.watchlist.GetWatchlists()
	if err != nil {
		log.Printf("[WATCHLIST] Error fetching watchlists | worker_id=%s | error=%v", w.workerID, err)
		return
	}

	for _, l := range lists {
		if !l.Enabled {
			continue
		}

		log.Printf("[WATCHLIST] Triggering sync | worker_id=%s | watchlist=%s | watchlist_id=%s", w.workerID, l.Name, l.ID)

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
			log.Printf("[WATCHLIST] Error enqueuing job | worker_id=%s | watchlist_id=%s | error=%v", w.workerID, l.ID, err)
		}
	}
}

func (w *WorkerOrchestrator) listenForWakeup() {
	reportProblem := func(ev pq.ListenerEventType, err error) {
		if err != nil {
			log.Printf("[NOTIFY] Listener error | worker_id=%s | error=%v", w.workerID, err)
		}
	}

	listener := pq.NewListener(w.cfg.DatabaseURL, 10*time.Second, time.Minute, reportProblem)
	err := listener.Listen("opswakeup")
	if err != nil {
		log.Fatalf("[NOTIFY] Failed to listen | worker_id=%s | error=%v", w.workerID, err)
	}

	log.Printf("[NOTIFY] Listening for 'opswakeup' events | worker_id=%s", w.workerID)

	for {
		if !w.running {
			return
		}

		select {
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
	log.Printf("[WORKER] Shutting down worker | worker_id=%s", w.workerID)
	w.running = false

	w.jobMutex.Lock()
	for id, jc := range w.activeJobs {
		log.Printf("[WORKER] Cancelling job | worker_id=%s | job_id=%d", w.workerID, id)
		jc.cancel()
	}
	w.jobMutex.Unlock()

	w.wg.Wait()
	w.lockManager.Close()
	log.Printf("[WORKER] Shutdown complete | worker_id=%s", w.workerID)
}

func (w *WorkerOrchestrator) heartbeatLoop() {
	ticker := time.NewTicker(5 * time.Second)
	for range ticker.C {
		if !w.running {
			return
		}
		w.updateHeartbeats()
	}
}

func (w *WorkerOrchestrator) zombieCleanupLoop() {
	log.Printf("[WORKER] Starting zombie cleanup loop | worker_id=%s", w.workerID)
	ticker := time.NewTicker(1 * time.Minute)
	for range ticker.C {
		if !w.running {
			return
		}

		// Find jobs marked as running but with stale heartbeats (> 2 mins)
		staleThreshold := time.Now().Add(-2 * time.Minute)
		var zombieJobs []database.Job
		err := w.db.Where("state = ? AND heartbeat_at < ?", "running", staleThreshold).Find(&zombieJobs).Error
		if err != nil {
			log.Printf("[WORKER] Error searching for zombie jobs | worker_id=%s | error=%v", w.workerID, err)
			continue
		}

		for _, job := range zombieJobs {
			log.Printf("[WORKER] Resetting zombie job | worker_id=%s | job_id=%d | last_heartbeat=%v", w.workerID, job.ID, job.HeartbeatAt)

			w.db.Model(&job).Updates(map[string]interface{}{
				"state":        "queued",
				"worker_id":    nil,
				"started_at":   nil,
				"heartbeat_at": nil,
			})

			// Attempt to release the advisory lock if it was held
			lockKey, err := w.lockManager.GetScopeLockKey(context.Background(), job.ScopeType, job.ScopeID)
			if err == nil {
				w.lockManager.ReleaseLock(context.Background(), lockKey)
			}
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
	log.Printf("[SCHEDULER] Starting scheduler loop | worker_id=%s", w.workerID)
	ticker := time.NewTicker(30 * time.Second)

	for range ticker.C {
		if !w.running {
			return
		}

		var schedules []database.Schedule
		// Initialize NextRunAt if NULL
		w.db.Model(&database.Schedule{}).Where("enabled = ? AND next_run_at IS NULL", true).Update("next_run_at", time.Now())

		// Find due schedules
		err := w.db.Where("enabled = ? AND next_run_at <= ?", true, time.Now()).Find(&schedules).Error
		if err != nil {
			log.Printf("[SCHEDULER] Error fetching schedules | worker_id=%s | error=%v", w.workerID, err)
			continue
		}

		for _, s := range schedules {
			log.Printf("[SCHEDULER] Executing schedule | worker_id=%s | schedule_id=%d | watchlist_id=%s", w.workerID, s.ID, s.WatchlistID)

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
				log.Printf("[SCHEDULER] Error enqueuing job | worker_id=%s | schedule_id=%d | error=%v", w.workerID, s.ID, err)
				continue
			}

			// Compute next run at
			sched, err := cron.ParseStandard(s.CronExpr)
			if err != nil {
				log.Printf("[SCHEDULER] Invalid cron expression | worker_id=%s | schedule_id=%d | cron=%s | error=%v", w.workerID, s.ID, s.CronExpr, err)
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

func (w *WorkerOrchestrator) claimAndProcess() {
	w.jobMutex.Lock()
	if len(w.activeJobs) >= MaxConcurrentJobs {
		w.jobMutex.Unlock()
		return
	}
	w.jobMutex.Unlock()

	var job database.Job

	// Start an immediate transaction to "lock" the row for SQLite
	err := w.db.Transaction(func(tx *gorm.DB) error {
		// 1. Find next queued job
		err := tx.Where("state = ?", "queued").Order("requested_at ASC").First(&job).Error
		if err != nil {
			return err // Will rollback and we'll try again next tick
		}

		// 2. Mark as running
		now := time.Now()
		return tx.Model(&job).Updates(map[string]interface{}{
			"state":        "running",
			"worker_id":    w.workerID,
			"started_at":   &now,
			"heartbeat_at": &now,
		}).Error
	})

	if err != nil {
		if err != gorm.ErrRecordNotFound {
			log.Printf("[WORKER] Error claiming job | worker_id=%s | error=%v", w.workerID, err)
		}
		return
	}

	// Acquire advisory lock for scope
	lockKey, err := w.lockManager.GetScopeLockKey(context.Background(), job.ScopeType, job.ScopeID)
	if err != nil {
		log.Printf("[WORKER] Error computing lock key | worker_id=%s | job_id=%d | error=%v", w.workerID, job.ID, err)
		return
	}

	acquired, err := w.lockManager.AcquireTryLock(context.Background(), lockKey)
	if err != nil {
		log.Printf("[WORKER] Error acquiring advisory lock | worker_id=%s | job_id=%d | error=%v", w.workerID, job.ID, err)
		return
	}

	if !acquired {
		log.Printf("[WORKER] Scope locked, requeueing | worker_id=%s | job_id=%d | scope_type=%s | scope_id=%s", w.workerID, job.ID, job.ScopeType, job.ScopeID)
		w.db.Model(&job).Updates(map[string]interface{}{
			"state":      "queued",
			"worker_id":  nil,
			"started_at": nil,
		})
		return
	}

	ctx, cancel := context.WithCancel(context.Background())

	jc := &jobContext{
		job:     job,
		cancel:  cancel,
		ctx:     ctx,
		lockKey: lockKey,
	}

	w.jobMutex.Lock()
	w.activeJobs[job.ID] = jc
	w.jobMutex.Unlock()

	log.Printf("[WORKER] Claimed job | worker_id=%s | job_id=%d | job_type=%s", w.workerID, job.ID, job.Type)
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
				defer w.wg.Done()
				w.processAcquisitionItem(jc)
			}(jc)
		case "sync":
			w.wg.Add(1)
			go func(jc *jobContext) {
				defer w.wg.Done()
				err := w.syncHandler.Execute(jc.ctx, jc.job.ID, jc.job)
				w.finishJob(jc.job.ID, err)
			}(jc)
		default:
			w.wg.Add(1)
			go func(jc *jobContext) {
				defer w.wg.Done()
				w.runMonolithicJob(jc)
			}(jc)
		}
	}
}

func (w *WorkerOrchestrator) processAcquisitionItem(jc *jobContext) {
	// Claim next item
	itemID, err := w.claimNextJobItem(jc.job.ID)
	if err != nil {
		log.Printf("[WORKER] Error claiming item | worker_id=%s | job_id=%d | error=%v", w.workerID, jc.job.ID, err)
		return
	}

	if itemID == 0 {
		// No more items, finish job
		w.finishJob(jc.job.ID, nil)
		return
	}

	err = w.acqHandler.ExecuteItem(jc.ctx, jc.job.ID, itemID)
	if err != nil {
		log.Printf("[WORKER] Error processing item | worker_id=%s | job_id=%d | item_id=%d | error=%v", w.workerID, jc.job.ID, itemID, err)
	}
}

func (w *WorkerOrchestrator) claimNextJobItem(jobID uint64) (uint64, error) {
	var itemID uint64
	err := w.db.Transaction(func(tx *gorm.DB) error {
		var item database.JobItem
		// Find next queued item or failed item whose next_attempt_at has passed
		err := tx.Where("job_id = ? AND (status = 'queued' OR (status = 'failed' AND next_attempt_at <= ?))", jobID, time.Now()).
			Order("sequence ASC").
			First(&item).Error

		if err != nil {
			if err == gorm.ErrRecordNotFound {
				return nil // No items found
			}
			return err
		}

		// Mark as running
		now := time.Now()
		err = tx.Model(&item).Updates(map[string]interface{}{
			"status":     "running",
			"started_at": &now,
		}).Error
		if err != nil {
			return err
		}

		itemID = item.ID
		return nil
	})

	return itemID, err
}

func (w *WorkerOrchestrator) runMonolithicJob(jc *jobContext) {
	log.Printf("[WORKER] Executing monolithic job | worker_id=%s | job_id=%d | job_type=%s", w.workerID, jc.job.ID, jc.job.Type)

	var err error
	switch jc.job.Type {
	case "artist_scan":
		artistID, _ := uuid.Parse(jc.job.ScopeID)
		err = w.atService.SyncDiscography(artistID)
	case "release_monitor":
		err = w.rmService.CheckAllArtists()
	case "index_refresh":
		log.Printf("[WORKER] Triggering Gonic index refresh | worker_id=%s | job_id=%d", w.workerID, jc.job.ID)
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
		log.Printf("[WORKER] Scanning library %s at path %s", library.Name, library.Path)
		err = w.scanService.ScanLibrary(jc.ctx, libraryID, library.Path)
	case "enrich":
		// Enrich metadata for tracks in a library using Discogs
		libraryID, err := uuid.Parse(jc.job.ScopeID)
		if err != nil {
			err = fmt.Errorf("invalid library UUID: %w", err)
			w.finishJob(jc.job.ID, err)
			return
		}
		log.Printf("[WORKER] Starting metadata enrichment for library %s", libraryID)

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
				log.Printf("[WORKER] Could not enrich %s - %s: %v", track.Artist, track.Title, err)
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
		log.Printf("[WORKER] Enriched %d tracks for library %s", enriched, library.Name)
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
	w.jobMutex.Unlock()

	// Release lock
	w.lockManager.ReleaseLock(context.Background(), jc.lockKey)

	finalState := "succeeded"
	summary := "Completed"
	if err != nil {
		finalState = "failed"
		summary = err.Error()
	}

	now := time.Now()
	w.db.Model(&database.Job{}).Where("id = ?", jobID).Updates(map[string]interface{}{
		"state":       finalState,
		"finished_at": &now,
		"summary":     summary,
	})

	w.notificationService.NotifyJobCompletion(jobID, jc.job.Type, finalState, summary, w.workerID)

	log.Printf("[WORKER] Finished job | worker_id=%s | job_id=%d | state=%s", w.workerID, jobID, finalState)
}

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	db, err := database.Connect(cfg)
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}

	worker := NewWorkerOrchestrator(cfg, db)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	go worker.Start()

	log.Println("[WORKER] Worker process running. Press Ctrl+C to stop.")
	<-stop
	worker.Stop()
}
