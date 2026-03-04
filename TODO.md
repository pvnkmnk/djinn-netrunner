# NetRunner Migration & Implementation TODO

## 🎯 Current Status: Go Backend Foundation Established
The core logic from the experimental `netrunnnerrr` project has been ported to a structured Go backend within `netrunner_repo/backend`. The system now combines the advanced Artist Tracking features with the professional orchestration patterns (Advisory Locks, Round-robin fairness).

## ✅ Completed Tasks
- [x] **Backend Initialization**: Go module initialized with Fiber, GORM, and Asynq.
- [x] **Unified Data Models**: Merged Python and Go models into `backend/internal/database/models.go` (GORM).
- [x] **Database Infrastructure**: Ported connection and migration logic to Go.
- [x] **Core Services Ported**:
    - `MusicBrainzService` (with 1req/s rate limiting).
    - `ArtistTrackingService` (Add/Sync/Monitor).
    - `ReleaseMonitorService` (Background daily checks).
    - `ScannerService` (Audio tag extraction via `dhowden/tag`).
    - `SlskdService` (API client skeleton).
- [x] **Go Worker Orchestrator**: Implemented PostgreSQL-based job claiming (`claim_next_job`) with goroutine execution.
- [x] **DB Migration**: Created `ops/db/init/migrations/2026_03_04_006_artist_tracking.sql`.

## 🔜 Next Steps (Conductor Workflow)

### 1. Infrastructure Integration
- [ ] **Docker Update**: Modify `docker-compose.yml` to replace Python worker/server with `backend-server` and `backend-worker`.
- [ ] **Dockerfile Creation**: Finalize multi-stage Dockerfiles for the Go binaries.

### 2. Feature Completion
- [ ] **Acquisition Pipeline**: Port the specific Soulseek search ranking and download scoring logic from Python to Go.
- [ ] **Job Item Handlers**: Implement `claim_next_jobitem` loop in the Go worker for `acquisition` type jobs.
- [ ] **Gonic Integration**: Implement the actual trigger for library refreshes via Gonic API.

### 3. UI/UX Mapping
- [ ] **Router Alignment**: Update HTMX templates in `ops/web/templates` to point to the new Fiber API endpoints (`:8080/api/...`).
- [ ] **Artist Tracking UI**: Finalize the `artist_tracking.html` integration with the Go backend.

---
*Generated on 2026-03-04 for migration session.*
