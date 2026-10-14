package api

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/contrib/v3/websocket"
	"github.com/lib/pq"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"gorm.io/gorm"
)

type WebSocketManager struct {
	// jobID -> set of connections
	clients map[string]map[*websocket.Conn]struct{}
	mu      sync.RWMutex
}

func NewWebSocketManager() *WebSocketManager {
	return &WebSocketManager{
		clients: make(map[string]map[*websocket.Conn]struct{}),
	}
}

func (m *WebSocketManager) Subscribe(client *websocket.Conn, jobID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.clients[jobID]; !ok {
		m.clients[jobID] = make(map[*websocket.Conn]struct{})
	}
	m.clients[jobID][client] = struct{}{}
}

func (m *WebSocketManager) Unsubscribe(client *websocket.Conn, jobID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if conns, ok := m.clients[jobID]; ok {
		delete(conns, client)
		if len(conns) == 0 {
			delete(m.clients, jobID)
		}
	}
}

func (m *WebSocketManager) Broadcast(jobID string, message string) {
	m.mu.RLock()
	clients, ok := m.clients[jobID]
	m.mu.RUnlock()

	if !ok {
		return
	}

	for client := range clients {
		if err := client.WriteMessage(websocket.TextMessage, []byte(message)); err != nil {
			slog.Error("WS broadcast error", "job_id", jobID, "error", err)
		}
	}
}

type JobLogEvent struct {
	Event string `json:"event"`
	JobID uint64 `json:"job_id"`
	LogID uint64 `json:"log_id"`
}

func (m *WebSocketManager) ListenForJobLogs(ctx context.Context, dbURL string, db *gorm.DB) {
	if !database.IsPostgres(dbURL) {
		slog.Info("Skipping Postgres log listener for non-Postgres database")
		<-ctx.Done()
		return
	}

	reportProblem := func(ev pq.ListenerEventType, err error) {
		if err != nil {
			slog.Error("Log listener error", "error", err)
		}
	}

	listener := pq.NewListener(dbURL, 10*time.Second, time.Minute, reportProblem)
	defer listener.Close()
	err := listener.Listen("opsevents")
	if err != nil {
		slog.Error("Failed to listen for logs", "error", err)
		return
	}

	slog.Info("Listening for opsevents log notifications")

	for {
		select {
		case <-ctx.Done():
			slog.Info("Log listener shutting down")
			return
		case n := <-listener.Notify:
			var event JobLogEvent
			if err := json.Unmarshal([]byte(n.Extra), &event); err != nil {
				continue
			}

			if event.Event == "job_log" {
				var jobLog database.JobLog
				if err := db.First(&jobLog, event.LogID).Error; err == nil {
					m.Broadcast(strconv.FormatUint(event.JobID, 10), logLineHTML(jobLog))
				}
			}
		case <-time.After(1 * time.Minute):
			go listener.Ping()
		}
	}
}

// logLineHTML renders one job log line as the console's socket sends it.
//
// The hx-swap-oob attribute is the whole destination. This app ships htmx's
// in-core websocket support (hx-ws) rather than the ws extension, and in-core
// ws swaps each top-level element of a message out of band - so a frame that
// named nothing would be dropped with an oobErrorNoTarget and the console
// would stay empty. "beforeend:#console-logs" appends instead of replacing,
// which is what a log stream is, and it is why the log pane in
// ops/web/templates/index.html has to keep that id.
//
// The outer element is the swap instruction and is consumed by it. htmx's
// out-of-band swap inserts the *children* of the element carrying hx-swap-oob
// into the target, not the element itself, so the line has to be a child here:
// measured in the browser, a frame whose log-line div carried the attribute had
// its wrapper dropped and only the time/level/message spans landed in the pane,
// which left .log-line empty and the console's own filters and Copy Last 200
// silently matching nothing.
//
// ✅ SECURITY: the message is a worker's text, not ours, so it is escaped.
// ✅ ACCESSIBILITY: semantic <time> and an aria-labelled level.
func logLineHTML(jobLog database.JobLog) string {
	return fmt.Sprintf(
		`<div hx-swap-oob="beforeend:#console-logs">`+
			`<div class="log-line log-%s" id="log-%d" data-log-id="%d">`+
			`<time class="log-ts" datetime="%s" title="%s">%s</time> `+
			`<span class="log-level" aria-label="Log Level %s">[%s]</span> `+
			`<span class="log-msg">%s</span>`+
			`</div>`+
			`</div>`,
		stringsToLower(jobLog.Level),
		jobLog.ID,
		jobLog.ID,
		jobLog.CreatedAt.Format(time.RFC3339),
		jobLog.CreatedAt.Format(time.RFC3339),
		jobLog.CreatedAt.Format("15:04:05"),
		jobLog.Level,
		jobLog.Level,
		html.EscapeString(jobLog.Message),
	)
}

// consoleNoticeHTML is a refusal this socket has to report itself: the job was
// deleted, or changed hands, between the attach that named it and this
// connection. It lands in the same pane as a log line, because a refusal the
// operator cannot see is indistinguishable from a console with nothing to say
// yet - and the server closes the socket straight after, so the pane is where
// the last word belongs.
func consoleNoticeHTML(message string) string {
	return fmt.Sprintf(
		`<div hx-swap-oob="beforeend:#console-logs">`+
			`<div class="log-line log-err">`+
			`<span class="log-level" aria-label="Log Level ERR">[ERR]</span> `+
			`<span class="log-msg">%s</span>`+
			`</div>`+
			`</div>`,
		html.EscapeString(message),
	)
}

func stringsToLower(s string) string {
	return strings.ToLower(s)
}

func (m *WebSocketManager) HandleEvents(c *websocket.Conn) {
	// ✅ SECURITY: Only admins can subscribe to the system-wide event stream
	user, ok := c.Locals("user").(database.User)
	if !ok || user.Role != "admin" {
		c.WriteMessage(websocket.TextMessage, []byte("Access denied: Admin role required for system events"))
		c.Close()
		return
	}

	// System-wide events use jobID "0"
	m.Subscribe(c, "0")
	defer m.Unsubscribe(c, "0")

	// Read loop to keep connection alive
	for {
		if _, _, err := c.ReadMessage(); err != nil {
			break
		}
	}
}

func (m *WebSocketManager) HandleConsole(c *websocket.Conn, db *gorm.DB) {
	jobIDStr := c.Params("job_id")
	jobIDUint, _ := strconv.ParseUint(jobIDStr, 10, 64)

	// ✅ SECURITY: Verify job ownership or admin role
	user, ok := c.Locals("user").(database.User)
	if !ok {
		c.Close()
		return
	}

	var job database.Job
	if err := db.First(&job, jobIDUint).Error; err != nil {
		c.WriteMessage(websocket.TextMessage, []byte(consoleNoticeHTML("Job not found - it may have been cleared since you attached.")))
		c.Close()
		return
	}

	if user.Role != "admin" && (job.OwnerUserID == nil || *job.OwnerUserID != user.ID) {
		c.WriteMessage(websocket.TextMessage, []byte(consoleNoticeHTML("Access denied: you do not own this job.")))
		c.Close()
		return
	}

	tailStr := c.Query("tail")
	sinceIDStr := c.Query("since_id")

	m.Subscribe(c, jobIDStr)
	defer m.Unsubscribe(c, jobIDStr)

	// Send initial backlog
	var logs []database.JobLog
	query := db.Where("job_id = ?", jobIDUint)

	if tailStr != "" {
		tail, _ := strconv.Atoi(tailStr)
		query = query.Order("id DESC").Limit(tail)
	} else if sinceIDStr != "" {
		sinceID, _ := strconv.ParseUint(sinceIDStr, 10, 64)
		query = query.Where("id > ?", sinceID).Order("id ASC")
	} else {
		query = query.Order("id DESC").Limit(50)
	}

	if err := query.Find(&logs).Error; err == nil {
		// If we queried DESC, we need to reverse for display
		if tailStr != "" || (tailStr == "" && sinceIDStr == "") {
			for i, j := 0, len(logs)-1; i < j; i, j = i+1, j-1 {
				logs[i], logs[j] = logs[j], logs[i]
			}
		}

		for _, jobLog := range logs {
			c.WriteMessage(websocket.TextMessage, []byte(logLineHTML(jobLog)))
		}
	}

	// Read loop to keep connection alive
	for {
		if _, _, err := c.ReadMessage(); err != nil {
			break
		}
	}
}
