package api

import (
	"encoding/json"
	"fmt"
	"html"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/contrib/websocket"
	"github.com/lib/pq"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"gorm.io/gorm"
)

// WebSocketManager manages per-job WebSocket subscriptions for real-time log streaming.
// Clients subscribe to specific job IDs and only receive logs for those jobs.
type WebSocketManager struct {
	// jobID (string) -> set of connections
	clients map[string]map[*websocket.Conn]struct{}
	mu      sync.RWMutex
}

// NewWebSocketManager creates a new WebSocket manager with initialized client maps.
func NewWebSocketManager() *WebSocketManager {
	return &WebSocketManager{
		clients: make(map[string]map[*websocket.Conn]struct{}),
	}
}

// Subscribe registers a connection for real-time updates on a specific job.
func (m *WebSocketManager) Subscribe(conn *websocket.Conn, jobID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.clients[jobID]; !ok {
		m.clients[jobID] = make(map[*websocket.Conn]struct{})
	}
	m.clients[jobID][conn] = struct{}{}
}

// Unsubscribe removes a connection from a job's subscription set.
// If the set becomes empty, the jobID key is removed from the map.
func (m *WebSocketManager) Unsubscribe(conn *websocket.Conn, jobID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if clients, ok := m.clients[jobID]; ok {
		delete(clients, conn)
		if len(clients) == 0 {
			delete(m.clients, jobID)
		}
	}
}

// Broadcast sends a message only to clients subscribed to the specific jobID.
// Clients subscribed to other job IDs do not receive this message.
func (m *WebSocketManager) Broadcast(jobID string, message string) {
	m.mu.RLock()
	clients, ok := m.clients[jobID]
	// Copy active connections under lock to avoid iterating the map outside the lock
	active := make([]*websocket.Conn, 0, len(clients))
	for conn := range clients {
		active = append(active, conn)
	}
	m.mu.RUnlock()

	if !ok || len(active) == 0 {
		return
	}

	data := []byte(message)
	var deadConns []*websocket.Conn
	for _, conn := range active {
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			deadConns = append(deadConns, conn)
		}
	}

	// Remove dead connections in a single write lock
	if len(deadConns) > 0 {
		m.mu.Lock()
		defer m.mu.Unlock()
		if clients, ok := m.clients[jobID]; ok {
			for _, conn := range deadConns {
				delete(clients, conn)
				conn.Close()
			}
			if len(clients) == 0 {
				delete(m.clients, jobID)
			}
		}
	}
}

// Cleanup removes dead connections from all per-job subscription sets.
// It pings each connection to check liveness and removes those that fail.
// Lock is only held during map mutation, not during network I/O.
func (m *WebSocketManager) Cleanup() {
	// Phase 1: copy all connections under read lock
	m.mu.RLock()
	type jobConn struct {
		jobID string
		conn  *websocket.Conn
	}
	var all []jobConn
	for jobID, clients := range m.clients {
		for conn := range clients {
			all = append(all, jobConn{jobID: jobID, conn: conn})
		}
	}
	m.mu.RUnlock()

	if len(all) == 0 {
		return
	}

	// Phase 2: ping connections outside the lock
	var deadConns []jobConn
	for _, jc := range all {
		if err := jc.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
			deadConns = append(deadConns, jc)
		}
	}

	// Phase 3: remove dead connections under write lock
	if len(deadConns) > 0 {
		m.mu.Lock()
		defer m.mu.Unlock()
		for _, jc := range deadConns {
			if clients, ok := m.clients[jc.jobID]; ok {
				delete(clients, jc.conn)
				if len(clients) == 0 {
					delete(m.clients, jc.jobID)
				}
			}
		}
	}
}

type JobLogEvent struct {
	Event string `json:"event"`
	JobID uint64 `json:"job_id"`
	LogID uint64 `json:"log_id"`
}

// ListenForJobLogs listens for PostgreSQL NOTIFY events and broadcasts
// log entries to the appropriate per-job WebSocket clients.
func (m *WebSocketManager) ListenForJobLogs(dbURL string, db *gorm.DB) {
	reportProblem := func(ev pq.ListenerEventType, err error) {
		if err != nil {
			log.Printf("[NOTIFY] Log listener error: %v", err)
		}
	}

	listener := pq.NewListener(dbURL, 10*time.Second, time.Minute, reportProblem)
	err := listener.Listen("opsevents")
	if err != nil {
		log.Fatalf("[NOTIFY] Failed to listen for logs: %v", err)
	}

	log.Println("[NOTIFY] Listening for 'opsevents' log notifications")

	for {
		select {
		case n := <-listener.Notify:
			var event JobLogEvent
			if err := json.Unmarshal([]byte(n.Extra), &event); err != nil {
				continue
			}

			if event.Event == "job_log" {
				var jobLog database.JobLog
				if err := db.First(&jobLog, event.LogID).Error; err == nil {
					// Format as HTML for HTMX compatibility
					// ✅ SECURITY: Escape log message to prevent XSS
					// ✅ ACCESSIBILITY: Use semantic time and ARIA labels
					logHTML := fmt.Sprintf(
						`<div class="log-line log-%s" id="log-%d" data-log-id="%d">`+
							`<time class="log-ts" datetime="%s" title="%s">%s</time> `+
							`<span class="log-level" aria-label="Log Level %s">[%s]</span> `+
							`<span class="log-msg">%s</span>`+
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
					jobIDStr := strconv.FormatUint(event.JobID, 10)
					m.Broadcast(jobIDStr, logHTML)
				}
			}
		case <-time.After(1 * time.Minute):
			go listener.Ping()
		}
	}
}

func stringsToLower(s string) string {
	return strings.ToLower(s)
}

// HandleEvents manages a WebSocket connection for system-wide event notifications.
// Subscribes the connection to the "system" job scope.
func (m *WebSocketManager) HandleEvents(c *websocket.Conn) {
	m.Subscribe(c, "system")
	defer m.Unsubscribe(c, "system")

	// Read loop to keep connection alive
	for {
		if _, _, err := c.ReadMessage(); err != nil {
			break
		}
	}
}

// HandleConsole manages a WebSocket connection for job-specific log streaming.
// Subscribes the connection to the specific job ID from the URL path.
func (m *WebSocketManager) HandleConsole(c *websocket.Conn, db *gorm.DB) {
	jobIDStr := c.Params("job_id")
	jobID, err := strconv.ParseUint(jobIDStr, 10, 64)
	if err != nil {
		log.Printf("[WS] Invalid job ID provided to console: %s", jobIDStr)
		return
	}

	m.Subscribe(c, jobIDStr)
	defer m.Unsubscribe(c, jobIDStr)

	// Send initial backlog
	var logs []database.JobLog
	query := db.Where("job_id = ?", jobID)

	tailStr := c.Query("tail")
	sinceIDStr := c.Query("since_id")

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
			// ✅ SECURITY: Escape log message to prevent XSS
			// ✅ ACCESSIBILITY: Use semantic time and ARIA labels
			logHTML := fmt.Sprintf(
				`<div class="log-line log-%s" id="log-%d" data-log-id="%d">`+
					`<time class="log-ts" datetime="%s" title="%s">%s</time> `+
					`<span class="log-level" aria-label="Log Level %s">[%s]</span> `+
					`<span class="log-msg">%s</span>`+
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
			c.WriteMessage(websocket.TextMessage, []byte(logHTML))
		}
	}

	// Read loop to keep connection alive
	for {
		if _, _, err := c.ReadMessage(); err != nil {
			break
		}
	}
}
