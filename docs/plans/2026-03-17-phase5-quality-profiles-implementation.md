# Phase 5 Implementation: Quality Profiles

## Tasks

### Task 1: Add QualityProfile API Handler
**File:** `backend/internal/api/profiles.go` (new)

Create ProfileHandler with methods:
- `List(c *fiber.Ctx)` - List all profiles
- `Get(c *fiber.Ctx)` - Get single profile
- `Create(c *fiber.Ctx)` - Create profile
- `Update(c *fiber.Ctx)` - Update profile  
- `Delete(c *fiber.Ctx)` - Delete profile

---

### Task 2: Wire Up Profile Routes
**Files:**
- `backend/cmd/server/main.go` - Add ProfileHandler
- `backend/cmd/server/main_test.go` - Update test

Add routes:
```go
profileRoutes := apiProtected.Group("/profiles")
profileRoutes.Get("/", profileHandler.List)
profileRoutes.Post("/", profileHandler.Create)
profileRoutes.Get("/:id", profileHandler.Get)
profileRoutes.Patch("/:id", profileHandler.Update)
profileRoutes.Delete("/:id", profileHandler.Delete)
```

---

### Task 3: Add Profile CLI Commands
**File:** `backend/cmd/cli/main.go`

Add:
- `profile list` - List profiles
- `profile add [name]` - Add profile
- `profile rm [id]` - Delete profile
- `profile set-default [id]` - Set default profile

---

### Task 4: Add Agent Functions
**File:** `backend/internal/agent/handlers.go`

Add:
- `ListProfiles(db)` - List all profiles
- `CreateProfile(db, name, ...)` - Create profile
- `DeleteProfile(db, id)` - Delete profile

---

### Task 5: Profile Validation Service
**File:** `backend/internal/services/profile_service.go` (new)

Create ProfileService with methods:
- `ValidateFormat(profile, format) bool`
- `ValidateBitrate(profile, bitrate) bool`
- `ScoreResult(profile, searchResult) float64`

---

### Task 6: Seed Default Profiles
**File:** `backend/internal/database/migrate.go` or init

Add default profiles on startup:
- "High Quality"
- "Portable" 
- "Archival"

---

## Implementation Order
1. Task 1: Profile API Handler
2. Task 2: Wire routes
3. Task 3: CLI commands
4. Task 4: Agent functions
5. Task 5: Validation service
6. Task 6: Seed defaults
