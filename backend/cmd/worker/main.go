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
	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/pvnkmnk/netrunner/backend/internal/services"
	"gorm.io/gorm"
)

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
	
	activeJobs map[uint64]context.CancelFunc
	jobMutex   sync.Mutex
	running    bool
}

func NewWorkerOrchestrator(cfg *config.Config, db *gorm.DB) *WorkerOrchestrator {
	mb := services.NewMusicBrainzService(cfg)
	at := services.NewArtistTrackingService(db, mb)
	rm := services.NewReleaseMonitorService(db, at)
	return &WorkerOrchestrator{
		workerID:    fmt.Sprintf("worker-%s", uuid.New().String()[:8]),
		db:          db,
		cfg:         cfg,
		mbService:   mb,
		atService:   at,
		rmService:   rm,
		scanService: services.NewScannerService(db),
		lockManager: database.NewLockManager(db),
		activeJobs:  make(map[uint64]context.CancelFunc),
	}
}

func (w *WorkerOrchestrator) Start() {
	w.running = true
	log.Printf("[WORKER] Starting worker %s", w.workerID)

	// Heartbeat loop
	go w.heartbeatLoop()

	// Main job loop
	for w.running {
		w.claimAndProcess()
		time.Sleep(1 * time.Second)
	}
}

func (w *WorkerOrchestrator) Stop() {
	w.running = false
	w.lockManager.Close()
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

func (w *WorkerOrchestrator) claimAndProcess() {
	w.jobMutex.Lock()
	if len(w.activeJobs) >= 5 {
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
	
	w.jobMutex.Lock()
	w.activeJobs[job.ID] = cancel
	w.jobMutex.Unlock()

	go w.runJob(ctx, job, lockKey)
}

func (w *WorkerOrchestrator) runJob(ctx context.Context, job database.Job, lockKey int64) {
	log.Printf("[WORKER] Executing job %d (%s)", job.ID, job.Type)
	
	defer func() {
		w.lockManager.ReleaseLock(context.Background(), lockKey)
		
		w.jobMutex.Lock()
		delete(w.activeJobs, job.ID)
		w.jobMutex.Unlock()
	}()
	
	var err error
	summary := "Completed"

	switch job.Type {
	case "artist_scan":
		err = w.handleArtistScan(job)
	case "release_monitor":
		err = w.handleReleaseMonitor(job)
	case "index_refresh":
		err = w.handleIndexRefresh(job)
	default:
		err = fmt.Errorf("unsupported job type: %s", job.Type)
	}

	finalState := "succeeded"
	if err != nil {
		finalState = "failed"
		summary = err.Error()
		log.Printf("[WORKER] Job %d failed: %v", job.ID, err)
	} else {
		log.Printf("[WORKER] Job %d succeeded", job.ID)
	}

	now := time.Now()
	w.db.Model(&job).Updates(map[string]interface{}{
		"state":       finalState,
		"finished_at": &now,
		"summary":     summary,
	})
}

func (w *WorkerOrchestrator) handleArtistScan(job database.Job) error {
	// Parse artist ID from params or scope
	artistID, err := uuid.Parse(job.ScopeID)
	if err != nil {
		return fmt.Errorf("invalid artist ID: %s", job.ScopeID)
	}
	return w.atService.SyncDiscography(artistID)
}

func (w *WorkerOrchestrator) handleReleaseMonitor(job database.Job) error {
	return w.rmService.CheckAllArtists()
}

func (w *WorkerOrchestrator) handleIndexRefresh(job database.Job) error {
	// Simple placeholder for Gonic refresh
	log.Printf("[WORKER] Triggering Gonic index refresh")
	return nil
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
	log.Println("[WORKER] Shutting down...")
	worker.Stop()
}
