package database

import (
	"fmt"
	"strings"

	"gorm.io/gorm"
)

// The LISTEN/NOTIFY fast path, installed by the migrator.
//
// Both editions polled a 5-second loop for the project's whole history while
// README.md, docs/WHITEPAPER.md, docs/NETRUNNERLITE.md and ADR 0004 described an
// event-driven wakeup. The only sender in the tree was the SQL bootstrap in
// ops/db/init/02-functions.sql, which no compose file mounts and no code path
// executes. Mounting that file is not the fix: Docker's init scripts run against
// a fresh database only, so an existing deployment would keep the empty fast path
// forever, and the image does not ship ops/ at all -- backend/Dockerfile copies
// ./backend and ops/web. Installing from Go reaches a fresh volume and an existing
// database alike, runs on every boot rather than once, and travels with the binary
// into every deployment and the e2e stack.
//
// What is installed is exactly what a listener consumes:
//
//   - a job entering 'queued' publishes on `opswakeup`, the channel the worker's
//     listener subscribes to (backend/cmd/worker/main.go, listenForWakeup), so an
//     idle worker wakes on the insert instead of on its next poll;
//   - a row appended to `job_logs` publishes on `opsevents`, the channel the
//     console's log fan-out subscribes to (backend/internal/api/websocket.go,
//     ListenForJobLogs), so a line of output reaches an open console as it is
//     written.
//
// The second channel's other legacy event -- a job state change -- is deliberately
// NOT installed: no listener reads it, and a published event nobody consumes is a
// claim about the UI rather than behaviour. It belongs in the same change as its
// consumer.
//
// This is PostgreSQL-only, like the advisory locks and for the same reason: SQLite
// has no NOTIFY. NetrunnerLite therefore keeps polling, which is a real edition
// difference now and is recorded as one in docs/NETRUNNERLITE.md -- rather than the
// "same today because nothing works" the matrix carried while the sender was
// missing.
//
// Statement order matters: each function before the trigger that calls it.
//
// The log trigger targets `job_logs`, which is the table GORM's AutoMigrate
// creates for database.JobLog and the one the console reads. The legacy bootstrap
// says `joblogs` -- along with a `sources` table and a `jobtype` enum the runtime
// schema has not had for a long time -- so mounting that file could not have
// installed this sender even on a fresh database: it attaches its triggers to
// tables that do not exist. That is the second reason the sender lives here.
var notifyTriggerStatements = []string{
	// Wakeup: the worker's channel.
	`CREATE OR REPLACE FUNCTION notify_worker_wakeup()
	RETURNS trigger AS $$
	BEGIN
		IF NEW.state = 'queued' THEN
			PERFORM pg_notify('opswakeup', json_build_object(
				'event', 'job_queued',
				'job_id', NEW.id,
				'jobtype', NEW.job_type
			)::text);
		END IF;
		RETURN NEW;
	END;
	$$ LANGUAGE plpgsql`,
	`DROP TRIGGER IF EXISTS job_queued_wakeup ON jobs`,
	`CREATE TRIGGER job_queued_wakeup
		AFTER INSERT ON jobs
		FOR EACH ROW
		EXECUTE FUNCTION notify_worker_wakeup()`,

	// Console log fan-out: the web server's channel.
	`CREATE OR REPLACE FUNCTION notify_job_log()
	RETURNS trigger AS $$
	BEGIN
		PERFORM pg_notify('opsevents', json_build_object(
			'event', 'job_log',
			'job_id', NEW.job_id,
			'log_id', NEW.id
		)::text);
		RETURN NEW;
	END;
	$$ LANGUAGE plpgsql`,
	`DROP TRIGGER IF EXISTS job_log_notify ON job_logs`,
	`CREATE TRIGGER job_log_notify
		AFTER INSERT ON job_logs
		FOR EACH ROW
		EXECUTE FUNCTION notify_job_log()`,
}

// installNotifyTriggers installs the sender. Every statement is idempotent
// (CREATE OR REPLACE, DROP ... IF EXISTS), so the migrator installs on every boot
// without a marker to consult, and the whole set is one transaction so a database
// either has the sender or has none of it: a half-installed pair would wake
// workers while silently dropping console output, which is the failure mode that
// is hardest to notice from the outside.
func installNotifyTriggers(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		for _, statement := range notifyTriggerStatements {
			if err := tx.Exec(statement).Error; err != nil {
				return fmt.Errorf("installing the %s: %w", notifyStatementName(statement), err)
			}
		}
		return nil
	})
}

// notifyStatementName is the statement's first line, so a failure names the step
// that broke instead of echoing the whole function body into the log.
func notifyStatementName(statement string) string {
	if first, _, found := strings.Cut(statement, "\n"); found {
		return strings.TrimSpace(first)
	}
	return strings.TrimSpace(statement)
}
