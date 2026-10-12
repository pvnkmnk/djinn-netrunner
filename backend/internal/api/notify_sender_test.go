package api

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/lib/pq"
	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// The fast path is only real if a write the application performs publishes a
// notification, so these tests drive the real write paths against a real
// PostgreSQL and listen on the channels the two consumers subscribe to. A test
// that sent NOTIFY itself would exercise the listener and skip the sender, which
// is the half that was missing; one that asserted the SQL text alone would pass
// on a trigger that never fires.
//
// They are in this package because the API's enqueue is the path a user's click
// takes (POST /api/acquire -> a queued job -> the worker's channel).
// backend/cmd/worker/interop_test.go covers the other half: that a notification
// on `opswakeup` reaches the worker's listener.
//
// DATABASE_URL must be a Postgres URL: the sender is PostgreSQL-only, and the
// LISTEN/NOTIFY broker is the subject, so a skip here is a missing precondition
// rather than a pass. scripts/postgres_gate.py runs these with a real database and
// fails a run where they skip.

// postgresNotifyTestDB connects to the database under test and runs the
// production migrator, which is what installs the notification triggers. Calling
// the migrator rather than copying its DDL means these tests prove the boot path
// installs the sender -- the thing a fresh deployment depends on.
func postgresNotifyTestDB(t *testing.T) (*gorm.DB, string) {
	t.Helper()

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set: the NOTIFY sender is PostgreSQL-only, so there is nothing to publish to")
	}
	if !database.IsPostgres(dbURL) {
		t.Skipf("DATABASE_URL is not a Postgres URL (%s...): LISTEN/NOTIFY is PostgreSQL-only",
			strings.SplitN(dbURL, ":", 2)[0])
	}

	db, err := database.Connect(&config.Config{DatabaseURL: dbURL})
	require.NoError(t, err, "connect to the database under test")
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	require.NoError(t, database.Migrate(db), "the migrator installs the notification triggers")
	return db, dbURL
}

// newNotifyListener opens a listener on the database under test and proves it is
// actually listening before returning. pq.NewListener connects asynchronously, and
// a NOTIFY sent before the LISTEN reaches the server is lost with no error -- a
// test that then failed would be blaming the sender for its own race, and the
// worker's 5-second poll hides the same race in production.
func newNotifyListener(t *testing.T, db *gorm.DB, dbURL string, channels ...string) *pq.Listener {
	t.Helper()

	listener := pq.NewListener(dbURL, 10*time.Second, time.Minute, nil)
	t.Cleanup(func() { _ = listener.Close() })
	for _, channel := range channels {
		require.NoError(t, listener.Listen(channel), "subscribe to %s", channel)
	}

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		// The probe is sent on a channel under test and carries no payload, so it
		// can only be mistaken for a real publish by a consumer that ignores both
		// the channel and the payload.
		require.NoError(t, db.Exec("NOTIFY "+channels[0]).Error)
		select {
		case n := <-listener.Notify:
			if n != nil && n.Channel == channels[0] {
				return listener
			}
		case <-time.After(250 * time.Millisecond):
		}
	}
	t.Fatalf("no notification on %q within 10s of a probe NOTIFY: the listener never connected to %s",
		channels[0], dbURL)
	return nil
}

// awaitNotification waits for the first notification on `channel` that `match`
// accepts. Other traffic on the channel (the readiness probe, a job another test
// left behind) is skipped rather than failed on.
func awaitNotification(t *testing.T, listener *pq.Listener, channel string, match func(*pq.Notification) bool) *pq.Notification {
	t.Helper()

	deadline := time.After(10 * time.Second)
	for {
		select {
		case n := <-listener.Notify:
			if n != nil && n.Channel == channel && match(n) {
				return n
			}
		case <-deadline:
			t.Fatalf("nothing matching arrived on %q within 10s: the write path performed the write but did "+
				"not publish, so the fast path is not installed (or publishes on a channel nobody listens to)", channel)
			return nil
		}
	}
}

// TestAcquireAPIEnqueuePublishesAWakeup is the sender's acceptance criterion: a
// job queued through the HTTP API wakes an idle worker without waiting for its
// poll. The assertion is on the notification the API's own write produced, not on
// the row it wrote -- the row was never the broken half.
func TestAcquireAPIEnqueuePublishesAWakeup(t *testing.T) {
	db, dbURL := postgresNotifyTestDB(t)

	user := database.User{Email: "notify-wakeup@test.local", PasswordHash: "hash", Role: "user"}
	require.NoError(t, db.Create(&user).Error)
	t.Cleanup(func() { db.Exec("DELETE FROM users WHERE id = ?", user.ID) })

	listener := newNotifyListener(t, db, dbURL, "opswakeup")

	handler := NewAcquireHandler(db)
	app := fiber.New()
	app.Use(func(c fiber.Ctx) error {
		c.Locals("user", user)
		return c.Next()
	})
	app.Post("/api/acquire", handler.Create)

	req := httptest.NewRequest("POST", "/api/acquire",
		strings.NewReader(`{"artist":"Notify Proof","album":"Wakeup"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, 201, resp.StatusCode, "the enqueue itself must succeed, or there is nothing to publish")

	var created struct {
		Status string `json:"status"`
		JobID  uint64 `json:"job_id"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&created))
	require.NotZero(t, created.JobID)
	t.Cleanup(func() {
		db.Exec("DELETE FROM jobitems WHERE job_id = ?", created.JobID)
		db.Exec("DELETE FROM jobs WHERE id = ?", created.JobID)
	})

	notification := awaitNotification(t, listener, "opswakeup", func(n *pq.Notification) bool {
		var event struct {
			Event string `json:"event"`
			JobID uint64 `json:"job_id"`
		}
		if err := json.Unmarshal([]byte(n.Extra), &event); err != nil {
			return false
		}
		return event.Event == "job_queued" && event.JobID == created.JobID
	})

	var event struct {
		Event   string `json:"event"`
		JobID   uint64 `json:"job_id"`
		JobType string `json:"jobtype"`
	}
	require.NoError(t, json.Unmarshal([]byte(notification.Extra), &event))
	assert.Equal(t, "job_queued", event.Event)
	assert.Equal(t, created.JobID, event.JobID)
	assert.Equal(t, "acquisition", event.JobType, "the payload carries the type the worker decides on")
}

// TestJobLogInsertPublishesToTheConsoleStream proves the second consumer's channel
// the same way. A job log is written as a row (the worker's logJob and the API's
// log tails both go through GORM), so the row is the write path here; the console
// then loads it by id and broadcasts the formatted line.
func TestJobLogInsertPublishesToTheConsoleStream(t *testing.T) {
	db, dbURL := postgresNotifyTestDB(t)

	job := database.Job{Type: "sync", State: "queued", RequestedAt: time.Now()}
	require.NoError(t, db.Create(&job).Error)
	t.Cleanup(func() { db.Exec("DELETE FROM jobs WHERE id = ?", job.ID) })

	listener := newNotifyListener(t, db, dbURL, "opsevents")

	logRow := database.JobLog{JobID: job.ID, Level: "info", Message: "notify sender proof"}
	require.NoError(t, db.Create(&logRow).Error)
	t.Cleanup(func() { db.Exec("DELETE FROM job_logs WHERE id = ?", logRow.ID) })

	notification := awaitNotification(t, listener, "opsevents", func(n *pq.Notification) bool {
		var event struct {
			Event string `json:"event"`
			LogID uint64 `json:"log_id"`
		}
		if err := json.Unmarshal([]byte(n.Extra), &event); err != nil {
			return false
		}
		return event.Event == "job_log" && event.LogID == logRow.ID
	})

	var event struct {
		Event string `json:"event"`
		JobID uint64 `json:"job_id"`
		LogID uint64 `json:"log_id"`
	}
	require.NoError(t, json.Unmarshal([]byte(notification.Extra), &event))
	assert.Equal(t, "job_log", event.Event)
	assert.Equal(t, job.ID, event.JobID, "the console subscribes per job, so the payload must carry the job")
	assert.Equal(t, logRow.ID, event.LogID, "the listener loads the line by this id")
}
