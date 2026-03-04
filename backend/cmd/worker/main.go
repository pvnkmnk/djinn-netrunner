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
	mbService   *services.MusicBrainzService
	atService   *services.ArtistTrackingService
	rmService   *services.ReleaseMonitorService
	scanService *services.ScannerService
	lockManager *database.LockManager
	spotify     *services.SpotifyService
	slskd       *services.SlskdService
	metadata    *services.MetadataExtractor
	
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
	job    database.Job
	cancel context.CancelFunc
	ctx    context.Context
	lockKey int64
}

func NewWorkerOrchestrator(cfg *config.Config, db *gorm.DB) *WorkerOrchestrator {
	mb := services.NewMusicBrainzService(cfg)
	at := services.NewArtistTrackingService(db, mb)
	rm := services.NewReleaseMonitorService(db, at)
	spotify := services.NewSpotifyService(cfg)
	slskd := services.NewSlskdService(cfg)
	metadata := services.NewMetadataExtractor()
	
	return &WorkerOrchestrator{
		workerID:    fmt.Sprintf("worker-%s", uuid.New().String()[:8]),
		db:          db,
		cfg:         cfg,
		mbService:   mb,
		atService:   at,
		rmService:   rm,
		scanService: services.NewScannerService(db),
		lockManager: database.NewLockManager(db),
		spotify:     spotify,
		slskd:       slskd,
		metadata:    metadata,
		syncHandler: services.NewSyncHandler(db, spotify),
		acqHandler:  services.NewAcquisitionHandler(db, slskd, metadata),
		activeJobs:  make(map[uint64]*jobContext),
		wakeupChan:  make(chan bool, 1),
	}
}

func (w *WorkerOrchestrator) Start() {
	w.running = true
	log.Printf("[WORKER] Starting worker %s", w.workerID)

	// Start background tasks
	go w.heartbeatLoop()
	go w.schedulerLoop()
	go w.listenForWakeup()

	// Main job loop with round-robin item processing
	for w.running {
		w.claimAndProcess()
		w.processActiveJobsRoundRobin()
		
		// Wait for next tick OR wakeup notification
		select {
		case <-time.After(5 * time.Second):
			// Regular poll
		case <-w.wakeupChan:
			log.Println("[WORKER] Received wakeup notification")
		}
	}
}

func (w *WorkerOrchestrator) listenForWakeup() {
	reportProblem := func(ev pq.ListenerEventType, err error) {
		if err != nil {
			log.Printf("[NOTIFY] Listener error: %v", err)
		}
	}

	listener := pq.NewListener(w.cfg.DatabaseURL, 10*time.Second, time.Minute, reportProblem)
	err := listener.Listen("opswakeup")
	if err != nil {
		log.Fatalf("[NOTIFY] Failed to listen: %v", err)
	}

	log.Println("[NOTIFY] Listening for 'opswakeup' events")

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
	log.Printf("[WORKER] Shutting down worker %s...", w.workerID)
	w.running = false
	
	w.jobMutex.Lock()
	for id, jc := range w.activeJobs {
		log.Printf("[WORKER] Cancelling job %d", id)
		jc.cancel()
	}
	w.jobMutex.Unlock()

	w.wg.Wait()
	w.lockManager.Close()
	log.Println("[WORKER] Shutdown complete.")
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
	log.Println("[SCHEDULER] Starting scheduler loop")
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
			log.Printf("[SCHEDULER] Error fetching schedules: %v", err)
			continue
		}

		for _, s := range schedules {
			log.Printf("[SCHEDULER] Executing schedule %d for source %d", s.ID, s.SourceID)
			
			// Enqueue sync job
			job := database.Job{
				Type:        "sync",
				State:       "queued",
				ScopeType:   "source",
				ScopeID:     fmt.Sprintf("%d", s.SourceID),
				RequestedAt: time.Now(),
				OwnerUserID: s.Source.OwnerUserID,
				CreatedBy:   "scheduler",
			}
			
			if err := w.db.Create(&job).Error; err != nil {
				log.Printf("[SCHEDULER] Error enqueuing job: %v", err)
				continue
			}

			// Compute next run at
			sched, err := cron.ParseStandard(s.CronExpr)
			if err != nil {
				log.Printf("[SCHEDULER] Invalid cron expression '%s' for schedule %d: %v", s.CronExpr, s.ID, err)
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

	var jobID uint64
	err := w.db.Raw("SELECT claim_next_job(?)", w.workerID).Scan(&jobID).Error
	if err != nil {
		log.Printf("[WORKER] Error claiming job: %v", err)
		return
	}

	if jobID == 0 {
		return
	}

	var job database.Job
	if err := w.db.First(&job, jobID).Error; err != nil {
		log.Printf("[WORKER] Error fetching job %d: %v", jobID, err)
		return
	}

	// Acquire advisory lock for scope
	lockKey, err := w.lockManager.GetScopeLockKey(context.Background(), job.ScopeType, job.ScopeID)
	if err != nil {
		log.Printf("[WORKER] Error computing lock key for job %d: %v", job.ID, err)
		return
	}

	acquired, err := w.lockManager.AcquireTryLock(context.Background(), lockKey)
	if err != nil {
		log.Printf("[WORKER] Error acquiring advisory lock for job %d: %v", job.ID, err)
		return
	}

	if !acquired {
		log.Printf("[WORKER] Scope locked for job %d, requeueing", job.ID)
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

	log.Printf("[WORKER] Claimed job %d (%s)", job.ID, job.Type)
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
	var itemID uint64
	err := w.db.Raw("SELECT claim_next_jobitem(?)", jc.job.ID).Scan(&itemID).Error
	if err != nil {
		log.Printf("[WORKER] Error claiming item for job %d: %v", jc.job.ID, err)
		return
	}

	if itemID == 0 {
		// No more items, finish job
		w.finishJob(jc.job.ID, nil)
		return
	}

	err = w.acqHandler.ExecuteItem(jc.ctx, jc.job.ID, itemID)
	if err != nil {
		log.Printf("[WORKER] Error processing item %d: %v", itemID, err)
	}
}

func (w *WorkerOrchestrator) runMonolithicJob(jc *jobContext) {
	log.Printf("[WORKER] Executing monolithic job %d (%s)", jc.job.ID, jc.job.Type)
	
	var err error
	switch jc.job.Type {
	case "artist_scan":
		artistID, _ := uuid.Parse(jc.job.ScopeID)
		err = w.atService.SyncDiscography(artistID)
	case "release_monitor":
		err = w.rmService.CheckAllArtists()
	case "index_refresh":
		log.Printf("[WORKER] Triggering Gonic index refresh")
		// Placeholder for actual refresh call via GonicClient
		w.mbService.HealthCheck() // Just a dummy call to use a service
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
	
	log.Printf("[WORKER] Finished job %d: %s", jobID, finalState)
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
