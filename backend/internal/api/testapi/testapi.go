// Package testapi owns the e2e test-helper endpoints mounted under /api/test.
// These exist so browser specs can drive worker-exercising scenarios
// deterministically; they are NOT part of the product API and every endpoint
// double-gates on E2E_ENABLE_TEST_API plus an authenticated session.
package testapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"gorm.io/gorm"
)

// Mount registers the test endpoints on the given protected router group.
// Callers must only invoke it when cfg.E2EEnableTestAPI is true; the
// endpoints themselves re-check the gate per request so a config flip at
// runtime cannot expose them.
func Mount(router fiber.Router, cfg *config.Config, db *gorm.DB) {
	router.Post("/test/create-dir", createDir(cfg))
	router.Post("/test/seed-fallback-refusal", seedFallbackRefusal(cfg, db))
	router.Post("/test/seed-fallback-refusal/cleanup", cleanupProbeResidue(cfg, db))
}

// scopeSeq disambiguates two seeds sharing a clock tick (Windows ~15ms
// granularity makes UnixNano collisions real, not theoretical).
var scopeSeq atomic.Uint64

// cleanupMu and cleanupArtists implement DJI-502's single roster owner.
// probeFixtureArtists (below) is the BASELINE — the three names the checked-in
// specs seed. Every seed call ADDS the names its own payload uses, so a new
// acceptance clause's residue is cleaned by the same endpoint with no
// fixture-list edit; the drift that hurt was exactly a spec gaining a name
// cleanup never learned, after which hash/recording dedup (paths that BYPASS
// the identity gate) silently short-circuits the next probe. Seeds and
// cleanup run on different requests, so the map is guarded; entries are
// only ever added (bounded by seeds per process, and identical names are
// idempotent), never trimmed mid-run — a trimmed name between a spec's
// seed and its next run's cleanup would reintroduce the residue hazard.
var (
	cleanupMu      sync.Mutex
	cleanupArtists = map[string]struct{}{}
)

// declareSeedArtists registers a seed's fixture names with cleanup.
func declareSeedArtists(artists ...string) {
	cleanupMu.Lock()
	defer cleanupMu.Unlock()
	for _, a := range artists {
		if a != "" {
			cleanupArtists[a] = struct{}{}
		}
	}
}

// peerTagArtist extracts the name a successfully imported on-demand decoy
// would carry in the library — the peer's TAG artist, not the request's.
func peerTagArtist(p *PeerSpec) string {
	if p == nil {
		return ""
	}
	return p.TagArtist
}

// cleanupRoster returns the full artist roster cleanup must remove: the
// baseline fixture names plus everything any seed declared this process.
func cleanupRoster() []string {
	cleanupMu.Lock()
	defer cleanupMu.Unlock()
	roster := make([]string, 0, len(probeFixtureArtists)+len(cleanupArtists))
	roster = append(roster, probeFixtureArtists...)
	for a := range cleanupArtists {
		roster = append(roster, a)
	}
	return roster
}

func gateEnabled(cfg *config.Config) bool {
	return cfg != nil && cfg.E2EEnableTestAPI
}

// PeerSpec is the roster entry a spec may register with the fake slskd at
// seed time. Marker must be a lowercase substring of the seeded item's query.
type PeerSpec struct {
	Username  string `json:"username"`
	Filename  string `json:"filename"`
	TagArtist string `json:"tag_artist"`
	TagAlbum  string `json:"tag_album"`
	TagTitle  string `json:"tag_title"`
	Marker    string `json:"marker"`
	// Local is the staging-relative file the fake (re)generates on enqueue.
	// Derived from the filename when empty.
	Local string `json:"local"`
}

// registerPeer forwards a spec to the fake slskd's management surface. The
// fake has no host-published port (the worker talks to it over the compose
// network), so the server process is the one caller that can reach it.
// Best-effort on connection errors (the fake is absent on stacks that run the
// real slskd) but a loud 502 when the fake answers and rejects the spec.
func registerPeer(cfg *config.Config, spec *PeerSpec) error {
	if spec == nil {
		return nil
	}
	if spec.Local == "" {
		spec.Local = spec.Filename[strings.LastIndex(spec.Filename, "/")+1:]
	}
	client := http.Client{Timeout: 5 * time.Second}
	body, err := json.Marshal(map[string]string{
		"username":   spec.Username,
		"filename":   spec.Filename,
		"tag_artist": spec.TagArtist,
		"tag_album":  spec.TagAlbum,
		"tag_title":  spec.TagTitle,
		"marker":     spec.Marker,
		"local":      spec.Local,
	})
	if err != nil {
		return err
	}
	url := strings.TrimSuffix(cfg.SlskdURL, "/") + "/roster"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("X-API-Key", cfg.SlskdAPIKey)
	resp, err := client.Do(req)
	if err != nil {
		return nil // no fake slskd on this stack — the baseline roster stands
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("fake-slskd rejected peer %q: %s", spec.Username, raw)
	}
	return nil
}

func currentUser(c *fiber.Ctx) (database.User, bool) {
	user, ok := c.Locals("user").(database.User)
	return user, ok
}

// createDir: create directory (used by E2E tests to create library paths).
func createDir(cfg *config.Config) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if !gateEnabled(cfg) {
			return c.Status(403).JSON(fiber.Map{"error": "test API not enabled"})
		}
		if _, ok := currentUser(c); !ok {
			return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
		}

		var payload struct {
			Path string `json:"path"`
		}
		if err := c.BodyParser(&payload); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid payload"})
		}
		if payload.Path == "" {
			return c.Status(400).JSON(fiber.Map{"error": "path required"})
		}

		// Path traversal protection: clean path and verify it starts with allowed prefix
		cleanPath := filepath.Clean(payload.Path)
		allowedPrefixes := []string{"/tmp/", cfg.MusicLibraryPath}
		valid := false
		for _, prefix := range allowedPrefixes {
			if strings.HasPrefix(cleanPath, prefix) {
				valid = true
				break
			}
		}
		if !valid {
			return c.Status(400).JSON(fiber.Map{"error": "invalid path"})
		}

		if err := os.MkdirAll(cleanPath, 0o755); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "failed to create directory"})
		}
		return c.JSON(fiber.Map{"status": "ok", "path": cleanPath})
	}
}

// seedFallbackRefusal: seed a fallback-refusal acquisition (used by the DJI-501
// e2e refusal spec and the ga-probes suite). The item's source_url is public
// and resolvable — it passes the app's pre-handover walk (DJI-500) — but it is
// not on the egress boundary's allowlist, so yt-dlp's fetch through the proxy
// (YTDLP_PROXY) is denied at connect time: the refusal comes from the boundary,
// not from a dead address. httpbin.org is a public, long-lived documentation
// service: reachable, harmless, never a media source, and not on the allowlist.
//
// Every seed also DECLARES its fixture names to cleanup (sessionCleanup
// below): the request artist and, when a peer rides the payload, the peer's
// tag artist — the two names an import could land under. That registration is
// what makes cleanup self-extending (DJI-502): a new acceptance clause's
// residue is removed by the same endpoint, with no fixture-list edit.
func seedFallbackRefusal(cfg *config.Config, db *gorm.DB) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if !gateEnabled(cfg) {
			return c.Status(403).JSON(fiber.Map{"error": "test API not enabled"})
		}

		user, ok := currentUser(c)
		if !ok {
			return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
		}

		var payload struct {
			Artist string `json:"artist"`
			Album  string `json:"album"`
			// Peer optionally registers an on-demand peer with the e2e stack's
			// slskd stand-in (ops/fake-slskd's /roster) BEFORE the job is
			// created, keyed by marker word in the item's query. This keeps a
			// new acceptance clause from needing a fixture-code change; the
			// baseline roster still answers when omitted.
			Peer *PeerSpec `json:"peer"`
			// URL optionally overrides the probe's source_url. Empty keeps the
			// DJI-501 default: a public, non-allowlisted host the boundary
			// denies. The wrong-work probe passes a URL the boundary ALLOWS —
			// a documentation-IPv6 host serving a real but mismatched FLAC —
			// so the identity gate is the layer that refuses (see
			// egress-refusal.spec.ts and ga-probes.spec.ts for the probes).
			URL string `json:"url"`
			// NoFallback skips the source_url entirely: the item runs the
			// SOULSEEK entrance only. Used by the Soulseek wrong-work probe,
			// whose "network" is the e2e stack's slskd stand-in (ops/fake-slskd)
			// serving a deliberately mismatched file — the entrance that could
			// never be driven before it existed.
			NoFallback bool `json:"no_fallback"`
			// MaxAttempts overrides the job's retry budget (default 3). The
			// refusal probes are deterministic — the same denial every
			// attempt — so watching the full retry schedule burn ~6 minutes
			// proves nothing the first attempt doesn't; a job 54-style full
			// retry run already demonstrated the retry path ends terminal.
			MaxAttempts int `json:"max_attempts"`
		}
		if err := c.BodyParser(&payload); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid payload"})
		}
		if payload.Artist == "" {
			return c.Status(400).JSON(fiber.Map{"error": "artist required"})
		}
		var sourceURL string
		switch {
		case payload.NoFallback:
			sourceURL = ""
		case payload.URL != "":
			sourceURL = payload.URL
		default:
			sourceURL = "https://httpbin.org/bytes/1024"
		}

		// Register the spec's peer with the fake slskd BEFORE the job exists,
		// so the worker's search can never race the roster write.
		if err := registerPeer(cfg, payload.Peer); err != nil {
			return c.Status(502).JSON(fiber.Map{"error": err.Error()})
		}

		// Declare this seed's fixture names to cleanup: the requested artist
		// (the folder a refused item is stranded under) and, when a peer is		// specified, the peer's TAG artist (the library name a successfully		// imported decoy would carry). See sessionCleanup.
		declareSeedArtists(payload.Artist, peerTagArtist(payload.Peer))

		job := database.Job{
			Type:        "acquisition",
			State:       "queued",
			RequestedAt: time.Now(),
			OwnerUserID: &user.ID,
			CreatedBy:   "e2e_probe",
			Params:      json.RawMessage("{}"),
			// The probes run one attempt by default: their refusals are
			// deterministic, so watching the full retry schedule (~6 min of
			// identical denials) proves nothing the first attempt doesn't.
			// A full 3-attempt retry run to terminal was already observed
			// live (job 54 in the e2e DB, 2026-09-19).
			MaxAttempts: 3,
			// A unique scope per seed: the advisory lock key is a hash of
			// scope_type:scope_id, so empty-scope jobs all contend on one key —
			// a leaked lock from any earlier scope-less run would requeue this
			// job forever. Production acquire jobs are scoped the same way.
			// The seq suffix matters: Windows' clock ticks ~15ms, so two seeds
			// in one tick share a UnixNano and would contend again.
			ScopeType: "probe",
			ScopeID:   fmt.Sprintf("fallback-refusal-%d-%d", time.Now().UnixNano(), scopeSeq.Add(1)),
		}
		if payload.MaxAttempts >= 1 && payload.MaxAttempts <= 10 {
			job.MaxAttempts = payload.MaxAttempts
		}
		item := database.JobItem{
			Artist:          payload.Artist,
			Album:           payload.Album,
			NormalizedQuery: strings.TrimSpace(payload.Artist + " " + payload.Album),
			Status:          "queued",
			SourceURL:       sourceURL,
			OwnerUserID:     &user.ID,
		}
		if err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Create(&job).Error; err != nil {
				return err
			}
			// job.ID is only populated by the insert above — set it here, not in
			// the struct literal, or the item is created orphaned with job_id = 0
			// and the worker claims an empty job.
			item.JobID = job.ID
			return tx.Create(&item).Error
		}); err != nil {
			slog.Error("Failed to seed fallback-refusal job", "error", err)
			return c.Status(500).JSON(fiber.Map{"error": "failed to seed job"})
		}
		return c.JSON(fiber.Map{"job_id": job.ID})
	}
}

// probeFixtureArtists is the BASELINE roster (DJI-502): the three names the
// checked-in specs seed. Seeds extend the effective roster per-process via
// declareSeedArtists (see cleanupRoster), so a new clause never edits this
// list — the old "keep in sync" contract is what let residue short-circuit
// the identity gate via the dedup paths.
var probeFixtureArtists = []string{"Totally Different Band", "Wrong Work Probe", "Clean Success Artist"}

// cleanupProbeResidue: remove the rows and library files the probe specs seed,
// so a rerun (or the mutation check's own import) cannot short-circuit the
// behavior under test via the hash-duplicate or recording-dedup paths —
// both bypass the identity gate by design. The artist list is the fixture
// roster: the clean peer's tags and the FOLDER NAME the refused decoy imported
// under (the item's request — the decoy is stranded there by the
// terminal-discard boundary, which only removes STAGED files, never library
// files). Every failure is reported: a cleanup that silently no-ops leaves the
// duplicate paths active and the next probe exercises nothing.
func cleanupProbeResidue(cfg *config.Config, db *gorm.DB) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if !gateEnabled(cfg) {
			return c.Status(403).JSON(fiber.Map{"error": "test API not enabled"})
		}
		if _, ok := currentUser(c); !ok {
			return c.Status(401).JSON(fiber.Map{"error": "not authenticated"})
		}

		var problems []string
		for _, artist := range cleanupRoster() {
			if err := db.Where("artist = ?", artist).Delete(&database.Track{}).Error; err != nil {
				problems = append(problems, fmt.Sprintf("tracks %q: %v", artist, err))
			}
			if err := db.Where("artist = ?", artist).Delete(&database.Acquisition{}).Error; err != nil {
				problems = append(problems, fmt.Sprintf("acquisitions %q: %v", artist, err))
			}
			artistDir := filepath.Join(cfg.MusicLibraryPath, artist)
			if err := os.RemoveAll(artistDir); err != nil {
				problems = append(problems, fmt.Sprintf("dir %s: %v", artistDir, err))
			}
		}
		if len(problems) > 0 {
			return c.Status(500).JSON(fiber.Map{"error": "cleanup incomplete", "details": problems})
		}
		return c.JSON(fiber.Map{"status": "ok"})
	}
}
